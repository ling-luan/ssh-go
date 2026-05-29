package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"ssh-forwarder/internal/config"
)

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateError    State = "error"
)

type Status struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	ProfileID         string    `json:"profileId"`
	ProfileName       string    `json:"profileName"`
	State             State     `json:"state"`
	Running           bool      `json:"running"`
	AutoStart         bool      `json:"autoStart"`
	LocalAddress      string    `json:"localAddress"`
	TargetAddress     string    `json:"targetAddress"`
	ActiveConnections int64     `json:"activeConnections"`
	BytesIn           uint64    `json:"bytesIn"`
	BytesOut          uint64    `json:"bytesOut"`
	StartedAt         time.Time `json:"startedAt,omitempty"`
	LastError         string    `json:"lastError,omitempty"`
}

type ProfileStatus struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	Connected     bool   `json:"connected"`
	ActiveTunnels int    `json:"activeTunnels"`
}

type Event struct {
	Time     time.Time `json:"time"`
	Level    string    `json:"level"`
	TunnelID string    `json:"tunnelId,omitempty"`
	Message  string    `json:"message"`
}

type Manager struct {
	store *config.Store

	mu       sync.Mutex
	runtimes map[string]*runtimeState
	clients  map[string]*clientRef
	events   []Event
}

type clientRef struct {
	client *ssh.Client
	refs   int
}

type runtimeState struct {
	cfg     config.Tunnel
	profile config.Profile

	mu        sync.RWMutex
	state     State
	listener  net.Listener
	startedAt time.Time
	lastError string
	stopOnce  sync.Once

	active   atomic.Int64
	bytesIn  atomic.Uint64
	bytesOut atomic.Uint64

	connMu sync.Mutex
	conns  map[net.Conn]struct{}
}

var bufferPool = sync.Pool{
	New: func() any {
		return make([]byte, 64*1024)
	},
}

func NewManager(store *config.Store) *Manager {
	return &Manager{
		store:    store,
		runtimes: make(map[string]*runtimeState),
		clients:  make(map[string]*clientRef),
	}
}

func (m *Manager) StartAutoStart() {
	cfg := m.store.Config()
	for _, t := range cfg.Tunnels {
		if t.AutoStart {
			if err := m.StartTunnel(t.ID); err != nil {
				m.addEvent("error", t.ID, fmt.Sprintf("自动启动失败：%v", err))
			}
		}
	}
}

func (m *Manager) StartTunnel(id string) error {
	cfg := m.store.Config()
	tun, ok := findTunnel(cfg, id)
	if !ok {
		return fmt.Errorf("tunnel %q not found", id)
	}
	profile, ok := findProfile(cfg, tun.ProfileID)
	if !ok {
		return fmt.Errorf("profile %q not found", tun.ProfileID)
	}

	rt := &runtimeState{
		cfg:     tun,
		profile: profile,
		state:   StateStarting,
		conns:   make(map[net.Conn]struct{}),
	}

	m.mu.Lock()
	if current, ok := m.runtimes[id]; ok {
		if current.isActive() {
			m.mu.Unlock()
			return fmt.Errorf("tunnel %q is already %s", id, current.currentState())
		}
		rt.bytesIn.Store(current.bytesIn.Load())
		rt.bytesOut.Store(current.bytesOut.Load())
	}
	m.runtimes[id] = rt
	m.mu.Unlock()

	if _, err := m.acquireClient(profile); err != nil {
		if rt.currentState() == StateStopped {
			return errors.New("tunnel start cancelled")
		}
		rt.failStart(err)
		m.addEvent("error", id, fmt.Sprintf("SSH 连接失败：%v", err))
		return err
	}
	if rt.currentState() != StateStarting {
		m.releaseClient(profile.ID)
		m.addEvent("info", id, "启动已取消")
		return errors.New("tunnel start cancelled")
	}

	localAddress := net.JoinHostPort(tun.LocalHost, fmt.Sprintf("%d", tun.LocalPort))
	listener, err := net.Listen("tcp", localAddress)
	if err != nil {
		m.releaseClient(profile.ID)
		rt.failStart(err)
		m.addEvent("error", id, fmt.Sprintf("监听本地地址 %s 失败：%v", localAddress, err))
		return err
	}
	if rt.currentState() != StateStarting {
		_ = listener.Close()
		m.releaseClient(profile.ID)
		m.addEvent("info", id, "启动已取消")
		return errors.New("tunnel start cancelled")
	}

	rt.markRunning(listener)
	m.addEvent("info", id, fmt.Sprintf("已启动：%s -> %s", rt.localAddress(), rt.targetAddress()))
	go m.acceptLoop(rt)
	return nil
}

func (m *Manager) StopTunnel(id string) error {
	m.mu.Lock()
	rt, ok := m.runtimes[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if !rt.stop() {
		return nil
	}
	m.addEvent("info", id, "正在停止隧道")
	return nil
}

func (m *Manager) StartAll() map[string]string {
	cfg := m.store.Config()
	result := make(map[string]string, len(cfg.Tunnels))
	for _, tun := range cfg.Tunnels {
		if err := m.StartTunnel(tun.ID); err != nil {
			result[tun.ID] = err.Error()
		} else {
			result[tun.ID] = "ok"
		}
	}
	return result
}

func (m *Manager) StopAll() {
	cfg := m.store.Config()
	for _, tun := range cfg.Tunnels {
		_ = m.StopTunnel(tun.ID)
	}
}

func (m *Manager) Shutdown(ctx context.Context) {
	m.StopAll()
	done := make(chan struct{})
	go func() {
		for {
			if !m.anyActive() {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	select {
	case <-ctx.Done():
		m.forceCloseClients()
	case <-done:
	}
}

func (m *Manager) IsTunnelRunning(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.runtimes[id]
	return ok && rt.isActive()
}

func (m *Manager) ProfileHasRunning(profileID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rt := range m.runtimes {
		if rt.profile.ID == profileID && rt.isActive() {
			return true
		}
	}
	return false
}

func (m *Manager) Snapshot() ([]Status, []ProfileStatus, []Event) {
	cfg := m.store.Config()

	profileNames := make(map[string]string, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		profileNames[p.ID] = p.Name
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	statuses := make([]Status, 0, len(cfg.Tunnels))
	for _, tun := range cfg.Tunnels {
		if rt, ok := m.runtimes[tun.ID]; ok {
			if rt.isActive() {
				statuses = append(statuses, rt.status(profileNames[rt.cfg.ProfileID]))
			} else {
				statuses = append(statuses, rt.statusForConfig(tun, profileNames[tun.ProfileID]))
			}
			continue
		}
		statuses = append(statuses, Status{
			ID:            tun.ID,
			Name:          tun.Name,
			ProfileID:     tun.ProfileID,
			ProfileName:   profileNames[tun.ProfileID],
			State:         StateStopped,
			Running:       false,
			AutoStart:     tun.AutoStart,
			LocalAddress:  net.JoinHostPort(tun.LocalHost, fmt.Sprintf("%d", tun.LocalPort)),
			TargetAddress: net.JoinHostPort(tun.TargetHost, fmt.Sprintf("%d", tun.TargetPort)),
		})
	}

	activeByProfile := make(map[string]int)
	for _, rt := range m.runtimes {
		if rt.isActive() {
			activeByProfile[rt.profile.ID]++
		}
	}
	profiles := make([]ProfileStatus, 0, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		_, connected := m.clients[p.ID]
		profiles = append(profiles, ProfileStatus{
			ID:            p.ID,
			Name:          p.Name,
			Address:       net.JoinHostPort(p.Host, fmt.Sprintf("%d", p.Port)),
			Connected:     connected,
			ActiveTunnels: activeByProfile[p.ID],
		})
	}

	events := append([]Event(nil), m.events...)
	sort.Slice(events, func(i, j int) bool {
		return events[i].Time.After(events[j].Time)
	})
	return statuses, profiles, events
}

func (m *Manager) acceptLoop(rt *runtimeState) {
	defer m.releaseClient(rt.profile.ID)
	for {
		conn, err := rt.listener.Accept()
		if err != nil {
			if rt.currentState() == StateStopping {
				rt.markStopped()
				m.addEvent("info", rt.cfg.ID, "隧道已停止")
			} else {
				rt.setRuntimeError(err)
				m.addEvent("error", rt.cfg.ID, fmt.Sprintf("接收连接失败：%v", err))
			}
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetKeepAlive(true)
			_ = tcp.SetNoDelay(true)
		}
		go m.handleConn(rt, conn)
	}
}

func (m *Manager) handleConn(rt *runtimeState, local net.Conn) {
	rt.active.Add(1)
	rt.addConn(local)
	defer func() {
		rt.removeConn(local)
		_ = local.Close()
		rt.active.Add(-1)
	}()

	remote, err := m.dialTarget(rt)
	if err != nil {
		rt.noteError(err)
		m.addEvent("error", rt.cfg.ID, fmt.Sprintf("打开目标地址失败：%v", err))
		return
	}
	rt.addConn(remote)
	defer func() {
		rt.removeConn(remote)
		_ = remote.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		copyCounted(remote, local, &rt.bytesOut)
		closeWrite(remote)
	}()
	go func() {
		defer wg.Done()
		copyCounted(local, remote, &rt.bytesIn)
		closeWrite(local)
	}()
	wg.Wait()
}

func (m *Manager) dialTarget(rt *runtimeState) (net.Conn, error) {
	client := m.clientForProfile(rt.profile.ID)
	if client == nil {
		return nil, errors.New("SSH client is not connected")
	}

	target := rt.targetAddress()
	remote, err := client.Dial("tcp", target)
	if err == nil {
		return remote, nil
	}

	rt.noteError(fmt.Errorf("SSH channel failed, reconnecting: %w", err))
	client, reconnectErr := m.redialClient(rt.profile)
	if reconnectErr != nil {
		return nil, fmt.Errorf("%w; reconnect failed: %v", err, reconnectErr)
	}
	remote, err = client.Dial("tcp", target)
	if err != nil {
		return nil, err
	}
	return remote, nil
}

func (m *Manager) acquireClient(profile config.Profile) (*ssh.Client, error) {
	m.mu.Lock()
	if ref, ok := m.clients[profile.ID]; ok {
		ref.refs++
		client := ref.client
		m.mu.Unlock()
		return client, nil
	}
	m.mu.Unlock()

	client, err := dialProfile(profile, m.store.Dir())
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if ref, ok := m.clients[profile.ID]; ok {
		ref.refs++
		existing := ref.client
		_ = client.Close()
		return existing, nil
	}
	m.clients[profile.ID] = &clientRef{client: client, refs: 1}
	return client, nil
}

func (m *Manager) releaseClient(profileID string) {
	m.mu.Lock()
	ref, ok := m.clients[profileID]
	if !ok {
		m.mu.Unlock()
		return
	}
	ref.refs--
	if ref.refs > 0 {
		m.mu.Unlock()
		return
	}
	delete(m.clients, profileID)
	client := ref.client
	m.mu.Unlock()
	_ = client.Close()
}

func (m *Manager) clientForProfile(profileID string) *ssh.Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref, ok := m.clients[profileID]
	if !ok {
		return nil
	}
	return ref.client
}

func (m *Manager) redialClient(profile config.Profile) (*ssh.Client, error) {
	client, err := dialProfile(profile, m.store.Dir())
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	ref, ok := m.clients[profile.ID]
	if !ok {
		_ = client.Close()
		return nil, errors.New("tunnel stopped")
	}
	old := ref.client
	ref.client = client
	_ = old.Close()
	return client, nil
}

func (m *Manager) anyActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rt := range m.runtimes {
		if rt.isActive() {
			return true
		}
	}
	return false
}

func (m *Manager) forceCloseClients() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, ref := range m.clients {
		_ = ref.client.Close()
		delete(m.clients, id)
	}
}

func (m *Manager) addEvent(level string, tunnelID string, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, Event{
		Time:     time.Now(),
		Level:    level,
		TunnelID: tunnelID,
		Message:  message,
	})
	if len(m.events) > 200 {
		m.events = append([]Event(nil), m.events[len(m.events)-200:]...)
	}
}

func (rt *runtimeState) isActive() bool {
	state := rt.currentState()
	return state == StateStarting || state == StateRunning || state == StateStopping
}

func (rt *runtimeState) currentState() State {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.state
}

func (rt *runtimeState) markRunning(listener net.Listener) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.listener = listener
	rt.state = StateRunning
	rt.startedAt = time.Now()
	rt.lastError = ""
}

func (rt *runtimeState) failStart(err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.state = StateError
	rt.lastError = err.Error()
}

func (rt *runtimeState) setRuntimeError(err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.state = StateError
	rt.lastError = err.Error()
}

func (rt *runtimeState) noteError(err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.lastError = err.Error()
}

func (rt *runtimeState) markStopped() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.state = StateStopped
	rt.listener = nil
}

func (rt *runtimeState) stop() bool {
	stopped := false
	rt.stopOnce.Do(func() {
		rt.mu.Lock()
		if rt.state == StateStopped {
			rt.mu.Unlock()
			return
		}
		rt.state = StateStopping
		listener := rt.listener
		if listener == nil {
			rt.state = StateStopped
		}
		rt.mu.Unlock()

		if listener != nil {
			_ = listener.Close()
		}
		rt.closeConnections()
		stopped = true
	})
	return stopped
}

func (rt *runtimeState) status(profileName string) Status {
	rt.mu.RLock()
	state := rt.state
	startedAt := rt.startedAt
	lastError := rt.lastError
	rt.mu.RUnlock()
	return Status{
		ID:                rt.cfg.ID,
		Name:              rt.cfg.Name,
		ProfileID:         rt.cfg.ProfileID,
		ProfileName:       profileName,
		State:             state,
		Running:           state == StateRunning,
		AutoStart:         rt.cfg.AutoStart,
		LocalAddress:      rt.localAddress(),
		TargetAddress:     rt.targetAddress(),
		ActiveConnections: rt.active.Load(),
		BytesIn:           rt.bytesIn.Load(),
		BytesOut:          rt.bytesOut.Load(),
		StartedAt:         startedAt,
		LastError:         lastError,
	}
}

func (rt *runtimeState) statusForConfig(tun config.Tunnel, profileName string) Status {
	rt.mu.RLock()
	state := rt.state
	startedAt := rt.startedAt
	lastError := rt.lastError
	rt.mu.RUnlock()
	return Status{
		ID:                tun.ID,
		Name:              tun.Name,
		ProfileID:         tun.ProfileID,
		ProfileName:       profileName,
		State:             state,
		Running:           state == StateRunning,
		AutoStart:         tun.AutoStart,
		LocalAddress:      net.JoinHostPort(tun.LocalHost, fmt.Sprintf("%d", tun.LocalPort)),
		TargetAddress:     net.JoinHostPort(tun.TargetHost, fmt.Sprintf("%d", tun.TargetPort)),
		ActiveConnections: rt.active.Load(),
		BytesIn:           rt.bytesIn.Load(),
		BytesOut:          rt.bytesOut.Load(),
		StartedAt:         startedAt,
		LastError:         lastError,
	}
}

func (rt *runtimeState) localAddress() string {
	return net.JoinHostPort(rt.cfg.LocalHost, fmt.Sprintf("%d", rt.cfg.LocalPort))
}

func (rt *runtimeState) targetAddress() string {
	return net.JoinHostPort(rt.cfg.TargetHost, fmt.Sprintf("%d", rt.cfg.TargetPort))
}

func (rt *runtimeState) addConn(conn net.Conn) {
	rt.connMu.Lock()
	defer rt.connMu.Unlock()
	rt.conns[conn] = struct{}{}
}

func (rt *runtimeState) removeConn(conn net.Conn) {
	rt.connMu.Lock()
	defer rt.connMu.Unlock()
	delete(rt.conns, conn)
}

func (rt *runtimeState) closeConnections() {
	rt.connMu.Lock()
	defer rt.connMu.Unlock()
	for conn := range rt.conns {
		_ = conn.Close()
	}
}

func copyCounted(dst io.Writer, src io.Reader, counter *atomic.Uint64) {
	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)
	writer := countedWriter{dst: dst, counter: counter}
	_, _ = io.CopyBuffer(writer, src, buf)
}

type countedWriter struct {
	dst     io.Writer
	counter *atomic.Uint64
}

func (w countedWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	w.counter.Add(uint64(n))
	return n, err
}

func closeWrite(conn net.Conn) {
	if c, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = c.CloseWrite()
		return
	}
	_ = conn.Close()
}

func findTunnel(cfg config.AppConfig, id string) (config.Tunnel, bool) {
	for _, tun := range cfg.Tunnels {
		if tun.ID == id {
			return tun, true
		}
	}
	return config.Tunnel{}, false
}

func findProfile(cfg config.AppConfig, id string) (config.Profile, bool) {
	for _, profile := range cfg.Profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return config.Profile{}, false
}
