package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

const (
	DefaultConfigFile = "ssh-forwarder.json"
)

type AuthConfig struct {
	Type       string `json:"type"`
	Password   string `json:"password,omitempty"`
	KeyPath    string `json:"keyPath,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type Profile struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	Host                  string     `json:"host"`
	Port                  int        `json:"port"`
	Username              string     `json:"username"`
	Auth                  AuthConfig `json:"auth"`
	HostKeyPolicy         string     `json:"hostKeyPolicy"`
	KnownHostsPath        string     `json:"knownHostsPath,omitempty"`
	ConnectTimeoutSeconds int        `json:"connectTimeoutSeconds,omitempty"`
	KeepAliveSeconds      int        `json:"keepAliveSeconds,omitempty"`
}

type Tunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ProfileID  string `json:"profileId"`
	LocalHost  string `json:"localHost"`
	LocalPort  int    `json:"localPort"`
	TargetHost string `json:"targetHost"`
	TargetPort int    `json:"targetPort"`
	AutoStart  bool   `json:"autoStart"`
}

type AppConfig struct {
	Profiles []Profile `json:"profiles"`
	Tunnels  []Tunnel  `json:"tunnels"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  AppConfig
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func Default() AppConfig {
	cfg := AppConfig{
		Profiles: []Profile{},
		Tunnels:  []Tunnel{},
	}
	cfg.Normalize()
	return cfg
}

func Load(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigFile
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	store := &Store{path: abs}
	data, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		store.cfg = Default()
		if err := store.saveLocked(); err != nil {
			return nil, err
		}
		return store, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &store.cfg); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	store.cfg.Normalize()
	if err := store.cfg.Validate(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Path() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.path
}

func (s *Store) Dir() string {
	return filepath.Dir(s.Path())
}

func (s *Store) Config() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Clone()
}

func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.cfg = Default()
		return s.saveLocked()
	}
	if err != nil {
		return err
	}

	var next AppConfig
	if err := json.Unmarshal(data, &next); err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	next.Normalize()
	if err := next.Validate(); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

func (s *Store) Update(fn func(*AppConfig) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.cfg.Clone()
	if err := fn(&next); err != nil {
		return err
	}
	next.Normalize()
	if err := next.Validate(); err != nil {
		return err
	}
	previous := s.cfg
	s.cfg = next
	if err := s.saveLocked(); err != nil {
		s.cfg = previous
		return err
	}
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.path, data, 0o600)
}

func (c AppConfig) Clone() AppConfig {
	next := c
	next.Profiles = slices.Clone(c.Profiles)
	next.Tunnels = slices.Clone(c.Tunnels)
	return next
}

func (c *AppConfig) Normalize() {
	for i := range c.Profiles {
		p := &c.Profiles[i]
		p.ID = strings.TrimSpace(p.ID)
		p.Name = strings.TrimSpace(p.Name)
		p.Host = strings.TrimSpace(p.Host)
		p.Username = strings.TrimSpace(p.Username)
		if p.Port == 0 {
			p.Port = 22
		}
		if p.Name == "" {
			p.Name = p.ID
		}
		p.Auth.Type = strings.TrimSpace(strings.ToLower(p.Auth.Type))
		if p.Auth.Type == "" {
			if strings.TrimSpace(p.Auth.KeyPath) != "" {
				p.Auth.Type = "key"
			} else {
				p.Auth.Type = "password"
			}
		}
		p.HostKeyPolicy = strings.TrimSpace(strings.ToLower(p.HostKeyPolicy))
		if p.HostKeyPolicy == "" {
			p.HostKeyPolicy = "accept-new"
		}
		if p.ConnectTimeoutSeconds <= 0 {
			p.ConnectTimeoutSeconds = 8
		}
		if p.KeepAliveSeconds <= 0 {
			p.KeepAliveSeconds = 30
		}
	}
	for i := range c.Tunnels {
		t := &c.Tunnels[i]
		t.ID = strings.TrimSpace(t.ID)
		t.Name = strings.TrimSpace(t.Name)
		t.ProfileID = strings.TrimSpace(t.ProfileID)
		t.LocalHost = strings.TrimSpace(t.LocalHost)
		t.TargetHost = strings.TrimSpace(t.TargetHost)
		if t.Name == "" {
			t.Name = t.ID
		}
		if t.LocalHost == "" {
			t.LocalHost = "127.0.0.1"
		}
	}
}

func (c AppConfig) Validate() error {
	profileIDs := make(map[string]struct{}, len(c.Profiles))
	for _, p := range c.Profiles {
		if !idPattern.MatchString(p.ID) {
			return fmt.Errorf("invalid profile id %q", p.ID)
		}
		if p.Host == "" {
			return fmt.Errorf("profile %q host is required", p.ID)
		}
		if p.Username == "" {
			return fmt.Errorf("profile %q username is required", p.ID)
		}
		if p.Port < 1 || p.Port > 65535 {
			return fmt.Errorf("profile %q port must be between 1 and 65535", p.ID)
		}
		if _, ok := profileIDs[p.ID]; ok {
			return fmt.Errorf("duplicate profile id %q", p.ID)
		}
		profileIDs[p.ID] = struct{}{}

		switch p.Auth.Type {
		case "password":
			if p.Auth.Password == "" {
				return fmt.Errorf("profile %q password is required", p.ID)
			}
		case "key":
			if strings.TrimSpace(p.Auth.KeyPath) == "" {
				return fmt.Errorf("profile %q key path is required", p.ID)
			}
		default:
			return fmt.Errorf("profile %q auth type must be password or key", p.ID)
		}

		switch p.HostKeyPolicy {
		case "accept-new", "known-hosts", "insecure":
		default:
			return fmt.Errorf("profile %q host key policy must be accept-new, known-hosts, or insecure", p.ID)
		}
	}

	tunnelIDs := make(map[string]struct{}, len(c.Tunnels))
	for _, t := range c.Tunnels {
		if !idPattern.MatchString(t.ID) {
			return fmt.Errorf("invalid tunnel id %q", t.ID)
		}
		if _, ok := tunnelIDs[t.ID]; ok {
			return fmt.Errorf("duplicate tunnel id %q", t.ID)
		}
		tunnelIDs[t.ID] = struct{}{}
		if _, ok := profileIDs[t.ProfileID]; !ok {
			return fmt.Errorf("tunnel %q references missing profile %q", t.ID, t.ProfileID)
		}
		if t.LocalPort < 1 || t.LocalPort > 65535 {
			return fmt.Errorf("tunnel %q local port must be between 1 and 65535", t.ID)
		}
		if t.TargetPort < 1 || t.TargetPort > 65535 {
			return fmt.Errorf("tunnel %q target port must be between 1 and 65535", t.ID)
		}
		if t.TargetHost == "" {
			return fmt.Errorf("tunnel %q target host is required", t.ID)
		}
	}
	return nil
}
