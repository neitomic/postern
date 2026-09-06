package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type Client struct {
	Server       string `toml:"server"`
	Name         string `toml:"name"`
	LoginUser    string `toml:"login_user"`
	LocalSSHPort int    `toml:"local_ssh_port"`
	IdentityFile string `toml:"identity_file"`
	PosterndPath string `toml:"posternd_path"`
}

func DefaultClient() Client {
	return Client{
		LocalSSHPort: 22,
		PosterndPath: "/usr/bin/posternd",
	}
}

func ClientPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// ConfigDir is $XDG_CONFIG_HOME/postern on Linux and ~/.config/postern on
// macOS (not UserConfigDir, which is ~/Library/Application Support).
func ConfigDir() (string, error) {
	return xdgDir("XDG_CONFIG_HOME", ".config")
}

// DataDir is $XDG_DATA_HOME/postern on Linux and ~/.local/share/postern on macOS.
func DataDir() (string, error) {
	return xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share"))
}

func xdgDir(env, rel string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	base := filepath.Join(home, rel)
	if runtime.GOOS != "darwin" {
		if v := os.Getenv(env); v != "" {
			base = v
		}
	}
	return filepath.Join(base, "postern"), nil
}

// ParseServer splits USER@HOST or USER@HOST:PORT. Port defaults to 22.
func ParseServer(server string) (user, host string, port int, err error) {
	s := strings.TrimSpace(server)
	if s == "" {
		return "", "", 0, fmt.Errorf("empty server")
	}
	port = 22
	if i := strings.LastIndex(s, "@"); i >= 0 {
		user = s[:i]
		s = s[i+1:]
		if user == "" {
			return "", "", 0, fmt.Errorf("invalid server %q: missing user", server)
		}
	}
	host = s
	if h, p, ok := strings.Cut(s, ":"); ok {
		if h == "" || strings.Contains(h, ":") {
			return "", "", 0, fmt.Errorf("invalid server %q: missing host", server)
		}
		n, convErr := strconv.Atoi(p)
		if convErr != nil || n < 1 || n > 65535 {
			return "", "", 0, fmt.Errorf("invalid server %q: invalid port", server)
		}
		host = h
		port = n
	}
	if host == "" {
		return "", "", 0, fmt.Errorf("invalid server %q: missing host", server)
	}
	return user, host, port, nil
}

func LoadClient(path string) (Client, error) {
	cfg := DefaultClient()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Client{}, err
	}
	return cfg, nil
}

func (c Client) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(c); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
