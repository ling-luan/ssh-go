package tunnel

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"ssh-forwarder/internal/config"
)

var knownHostsMu sync.Mutex

func dialProfile(profile config.Profile, baseDir string) (*ssh.Client, error) {
	auth, err := authMethods(profile, baseDir)
	if err != nil {
		return nil, err
	}

	callback, err := hostKeyCallback(profile, baseDir)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(profile.ConnectTimeoutSeconds) * time.Second
	clientConfig := &ssh.ClientConfig{
		User:            profile.Username,
		Auth:            auth,
		HostKeyCallback: callback,
		Timeout:         timeout,
	}

	address := net.JoinHostPort(profile.Host, fmt.Sprintf("%d", profile.Port))
	rawConn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, err
	}
	if tcp, ok := rawConn.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(time.Duration(profile.KeepAliveSeconds) * time.Second)
	}

	conn, chans, reqs, err := ssh.NewClientConn(rawConn, address, clientConfig)
	if err != nil {
		_ = rawConn.Close()
		return nil, err
	}

	client := ssh.NewClient(conn, chans, reqs)
	go keepAlive(client, time.Duration(profile.KeepAliveSeconds)*time.Second)
	return client, nil
}

func authMethods(profile config.Profile, baseDir string) ([]ssh.AuthMethod, error) {
	switch profile.Auth.Type {
	case "password":
		return []ssh.AuthMethod{ssh.Password(profile.Auth.Password)}, nil
	case "key":
		keyPath := resolvePath(profile.Auth.KeyPath, baseDir)
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read private key %s: %w", keyPath, err)
		}
		var signer ssh.Signer
		if profile.Auth.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(profile.Auth.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key %s: %w", keyPath, err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, fmt.Errorf("unsupported auth type %q", profile.Auth.Type)
	}
}

func hostKeyCallback(profile config.Profile, baseDir string) (ssh.HostKeyCallback, error) {
	switch profile.HostKeyPolicy {
	case "insecure":
		return ssh.InsecureIgnoreHostKey(), nil
	case "known-hosts", "accept-new", "":
		knownHostsPath := profile.KnownHostsPath
		if strings.TrimSpace(knownHostsPath) == "" {
			knownHostsPath = filepath.Join(baseDir, "known_hosts")
		}
		knownHostsPath = resolvePath(knownHostsPath, baseDir)
		if err := ensureKnownHostsFile(knownHostsPath); err != nil {
			return nil, err
		}
		callback, err := knownhosts.New(knownHostsPath)
		if err != nil {
			return nil, err
		}
		if profile.HostKeyPolicy == "known-hosts" {
			return callback, nil
		}
		return acceptNewHostKey(callback, knownHostsPath), nil
	default:
		return nil, fmt.Errorf("unsupported host key policy %q", profile.HostKeyPolicy)
	}
}

func acceptNewHostKey(callback ssh.HostKeyCallback, knownHostsPath string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := callback(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			knownHostsMu.Lock()
			defer knownHostsMu.Unlock()
			line := knownhosts.Line([]string{hostname}, key)
			file, openErr := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if openErr != nil {
				return openErr
			}
			defer file.Close()
			if _, writeErr := file.WriteString(line + "\n"); writeErr != nil {
				return writeErr
			}
			return nil
		}
		return err
	}
}

func ensureKnownHostsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	return file.Close()
}

func resolvePath(path string, baseDir string) string {
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				path = home
			} else if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
				path = filepath.Join(home, path[2:])
			}
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(baseDir, path)
}

func keepAlive(client *ssh.Client, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		if err != nil {
			_ = client.Close()
			return
		}
	}
}
