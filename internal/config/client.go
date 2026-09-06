package config

import (
	"os"
	"path/filepath"

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
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "postern", "config.toml"), nil
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
