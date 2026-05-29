package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"ssh-forwarder/internal/config"
	"ssh-forwarder/internal/tunnel"
)

type App struct {
	ctx     context.Context
	store   *config.Store
	manager *tunnel.Manager
}

type SnapshotResponse struct {
	ConfigPath string                 `json:"configPath"`
	Config     config.AppConfig       `json:"config"`
	Tunnels    []TunnelStatusResponse `json:"tunnels"`
	Profiles   []tunnel.ProfileStatus `json:"profiles"`
	Events     []EventResponse        `json:"events"`
}

type TunnelStatusResponse struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	ProfileID         string `json:"profileId"`
	ProfileName       string `json:"profileName"`
	State             string `json:"state"`
	Running           bool   `json:"running"`
	AutoStart         bool   `json:"autoStart"`
	LocalAddress      string `json:"localAddress"`
	TargetAddress     string `json:"targetAddress"`
	ActiveConnections int64  `json:"activeConnections"`
	BytesIn           uint64 `json:"bytesIn"`
	BytesOut          uint64 `json:"bytesOut"`
	StartedAt         string `json:"startedAt,omitempty"`
	LastError         string `json:"lastError,omitempty"`
}

type EventResponse struct {
	Time       string `json:"time"`
	Level      string `json:"level"`
	TunnelID   string `json:"tunnelId,omitempty"`
	TunnelName string `json:"tunnelName,omitempty"`
	Message    string `json:"message"`
}

type SaveProfileRequest struct {
	OriginalID string         `json:"originalId"`
	Profile    config.Profile `json:"profile"`
}

type SaveTunnelRequest struct {
	OriginalID string        `json:"originalId"`
	Tunnel     config.Tunnel `json:"tunnel"`
}

func NewApp(store *config.Store, manager *tunnel.Manager) *App {
	return &App{
		store:   store,
		manager: manager,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.manager.StartAutoStart()
}

func (a *App) shutdown(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a.manager.Shutdown(shutdownCtx)
}

func (a *App) Snapshot() SnapshotResponse {
	tunnels, profiles, events := a.manager.Snapshot()
	cfg := a.store.Config()
	return SnapshotResponse{
		ConfigPath: a.store.Path(),
		Config:     cfg,
		Tunnels:    mapTunnelStatuses(tunnels),
		Profiles:   profiles,
		Events:     mapEvents(events, tunnelNameMap(cfg)),
	}
}

func (a *App) ReloadSnapshot() (SnapshotResponse, error) {
	if err := a.store.Reload(); err != nil {
		return SnapshotResponse{}, err
	}
	return a.Snapshot(), nil
}

func mapTunnelStatuses(items []tunnel.Status) []TunnelStatusResponse {
	out := make([]TunnelStatusResponse, 0, len(items))
	for _, item := range items {
		startedAt := ""
		if !item.StartedAt.IsZero() {
			startedAt = item.StartedAt.Format(time.RFC3339)
		}
		out = append(out, TunnelStatusResponse{
			ID:                item.ID,
			Name:              item.Name,
			ProfileID:         item.ProfileID,
			ProfileName:       item.ProfileName,
			State:             string(item.State),
			Running:           item.Running,
			AutoStart:         item.AutoStart,
			LocalAddress:      item.LocalAddress,
			TargetAddress:     item.TargetAddress,
			ActiveConnections: item.ActiveConnections,
			BytesIn:           item.BytesIn,
			BytesOut:          item.BytesOut,
			StartedAt:         startedAt,
			LastError:         item.LastError,
		})
	}
	return out
}

func mapEvents(items []tunnel.Event, tunnelNames map[string]string) []EventResponse {
	out := make([]EventResponse, 0, len(items))
	for _, item := range items {
		eventTime := ""
		if !item.Time.IsZero() {
			eventTime = item.Time.Format(time.RFC3339)
		}
		out = append(out, EventResponse{
			Time:       eventTime,
			Level:      item.Level,
			TunnelID:   item.TunnelID,
			TunnelName: tunnelNames[item.TunnelID],
			Message:    item.Message,
		})
	}
	return out
}

func tunnelNameMap(cfg config.AppConfig) map[string]string {
	names := make(map[string]string, len(cfg.Tunnels))
	for _, tun := range cfg.Tunnels {
		names[tun.ID] = tun.Name
	}
	return names
}

func (a *App) StartTunnel(id string) error {
	return a.manager.StartTunnel(strings.TrimSpace(id))
}

func (a *App) StopTunnel(id string) error {
	return a.manager.StopTunnel(strings.TrimSpace(id))
}

func (a *App) StartAll() map[string]string {
	return a.manager.StartAll()
}

func (a *App) StopAll() {
	a.manager.StopAll()
}

func (a *App) SaveProfile(req SaveProfileRequest) error {
	next := req.Profile
	next.ID = strings.TrimSpace(next.ID)
	next.Name = strings.TrimSpace(next.Name)
	next.Host = strings.TrimSpace(next.Host)
	next.Username = strings.TrimSpace(next.Username)
	next.Auth.Type = strings.TrimSpace(strings.ToLower(next.Auth.Type))
	next.Auth.KeyPath = strings.TrimSpace(next.Auth.KeyPath)
	next.HostKeyPolicy = strings.TrimSpace(strings.ToLower(next.HostKeyPolicy))
	next.KnownHostsPath = strings.TrimSpace(next.KnownHostsPath)
	originalID := strings.TrimSpace(req.OriginalID)

	return a.store.Update(func(cfg *config.AppConfig) error {
		if originalID != "" {
			if a.manager.ProfileHasRunning(originalID) {
				return errors.New("该 SSH Profile 正在被运行中的隧道使用，请先停止相关隧道")
			}
			next.ID = originalID
			for i := range cfg.Profiles {
				if cfg.Profiles[i].ID == originalID {
					cfg.Profiles[i] = next
					return nil
				}
			}
			return fmt.Errorf("profile %q not found", originalID)
		}

		if next.ID == "" {
			next.ID = uniqueProfileID(*cfg, next.Name)
		}
		cfg.Profiles = append(cfg.Profiles, next)
		return nil
	})
}

func (a *App) DeleteProfile(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("请选择 SSH Profile")
	}
	if a.manager.ProfileHasRunning(id) {
		return errors.New("该 SSH Profile 正在被运行中的隧道使用，请先停止相关隧道")
	}

	return a.store.Update(func(cfg *config.AppConfig) error {
		if profileInUse(*cfg, id) {
			return errors.New("仍有隧道引用该 SSH Profile，请先修改或删除这些隧道")
		}
		for i := range cfg.Profiles {
			if cfg.Profiles[i].ID == id {
				cfg.Profiles = append(cfg.Profiles[:i], cfg.Profiles[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("profile %q not found", id)
	})
}

func (a *App) SaveTunnel(req SaveTunnelRequest) error {
	next := req.Tunnel
	next.ID = strings.TrimSpace(next.ID)
	next.Name = strings.TrimSpace(next.Name)
	next.ProfileID = strings.TrimSpace(next.ProfileID)
	next.LocalHost = strings.TrimSpace(next.LocalHost)
	next.TargetHost = strings.TrimSpace(next.TargetHost)
	originalID := strings.TrimSpace(req.OriginalID)

	return a.store.Update(func(cfg *config.AppConfig) error {
		if originalID != "" {
			if a.manager.IsTunnelRunning(originalID) {
				return errors.New("该隧道正在运行，请先停止隧道")
			}
			next.ID = originalID
			for i := range cfg.Tunnels {
				if cfg.Tunnels[i].ID == originalID {
					cfg.Tunnels[i] = next
					return nil
				}
			}
			return fmt.Errorf("tunnel %q not found", originalID)
		}

		if next.ID == "" {
			next.ID = uniqueTunnelID(*cfg, next.Name)
		}
		cfg.Tunnels = append(cfg.Tunnels, next)
		return nil
	})
}

func (a *App) DeleteTunnel(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("请选择隧道")
	}
	if a.manager.IsTunnelRunning(id) {
		return errors.New("该隧道正在运行，请先停止隧道")
	}

	return a.store.Update(func(cfg *config.AppConfig) error {
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID == id {
				cfg.Tunnels = append(cfg.Tunnels[:i], cfg.Tunnels[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("tunnel %q not found", id)
	})
}

func uniqueProfileID(cfg config.AppConfig, name string) string {
	used := make(map[string]struct{}, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		used[profile.ID] = struct{}{}
	}
	return uniqueID(used)
}

func uniqueTunnelID(cfg config.AppConfig, name string) string {
	used := make(map[string]struct{}, len(cfg.Tunnels))
	for _, tun := range cfg.Tunnels {
		used[tun.ID] = struct{}{}
	}
	return uniqueID(used)
}

func uniqueID(used map[string]struct{}) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b [6]byte
	for i := 0; i < 100; i++ {
		if _, err := rand.Read(b[:]); err != nil {
			break
		}
		out := make([]byte, len(b))
		for i, value := range b {
			out[i] = alphabet[int(value)%len(alphabet)]
		}
		candidate := string(out)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
	for i := 0; i < 1000000; i++ {
		candidate := fmt.Sprintf("%06d", i)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
	return fmt.Sprintf("%06d", len(used)%1000000)
}

func profileInUse(cfg config.AppConfig, profileID string) bool {
	for _, tun := range cfg.Tunnels {
		if tun.ProfileID == profileID {
			return true
		}
	}
	return false
}
