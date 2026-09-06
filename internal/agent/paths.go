package agent

import (
	"path/filepath"

	"github.com/neitomic/postern/internal/config"
)

type Paths struct {
	ConfigDir string
	DataDir   string
}

func DefaultPaths() (Paths, error) {
	cfg, err := config.ConfigDir()
	if err != nil {
		return Paths{}, err
	}
	data, err := config.DataDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{ConfigDir: cfg, DataDir: data}, nil
}

func (p Paths) ConfigFile() string    { return filepath.Join(p.ConfigDir, "config.toml") }
func (p Paths) StateFile() string     { return filepath.Join(p.DataDir, "state.json") }
func (p Paths) EnrollRequest() string { return filepath.Join(p.DataDir, "enroll-request.json") }
func (p Paths) IdentityFile() string  { return filepath.Join(p.DataDir, "id_ed25519") }
func (p Paths) IdentityPub() string   { return filepath.Join(p.DataDir, "id_ed25519.pub") }
func (p Paths) KnownHosts() string    { return filepath.Join(p.DataDir, "known_hosts") }
func (p Paths) SSHConfig() string     { return filepath.Join(p.DataDir, "ssh_config") }
func (p Paths) SSHConfigRPC() string  { return filepath.Join(p.DataDir, "ssh_config.rpc") }

func (p Paths) Mkdir() error {
	if err := mkdir(p.ConfigDir); err != nil {
		return err
	}
	return mkdir(p.DataDir)
}
