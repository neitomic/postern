package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultPosternd(t *testing.T) {
	t.Parallel()
	c := DefaultPosternd()
	if c.DBPath != "/var/lib/postern/postern.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
	if c.SocketPath != "/run/postern/api.sock" {
		t.Errorf("SocketPath = %q", c.SocketPath)
	}
	if c.TunnelUser != "postern" {
		t.Errorf("TunnelUser = %q", c.TunnelUser)
	}
	if c.AuthorizedKeysPath != "/var/lib/postern/authorized_keys" {
		t.Errorf("AuthorizedKeysPath = %q", c.AuthorizedKeysPath)
	}
	if c.PortMin != 2200 || c.PortMax != 2299 {
		t.Errorf("ports = %d-%d", c.PortMin, c.PortMax)
	}
	if c.HeartbeatOfflineAfter.Duration() != 90*time.Second {
		t.Errorf("HeartbeatOfflineAfter = %s", c.HeartbeatOfflineAfter)
	}
	if c.JoinTokenTTL.Duration() != 15*time.Minute {
		t.Errorf("JoinTokenTTL = %s", c.JoinTokenTTL)
	}
	if c.VPSHostname != "vps.example.net" {
		t.Errorf("VPSHostname = %q", c.VPSHostname)
	}
}

func TestLoadPosternd(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "posternd.toml")
	body := `
db_path = "/tmp/x.db"
port_min = 3000
heartbeat_offline_after = "2m"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadPosternd(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.DBPath != "/tmp/x.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
	if c.PortMin != 3000 {
		t.Errorf("PortMin = %d", c.PortMin)
	}
	if c.PortMax != 2299 {
		t.Errorf("PortMax = %d, want default", c.PortMax)
	}
	if c.HeartbeatOfflineAfter.Duration() != 2*time.Minute {
		t.Errorf("HeartbeatOfflineAfter = %s", c.HeartbeatOfflineAfter)
	}
	if c.JoinTokenTTL.Duration() != 15*time.Minute {
		t.Errorf("JoinTokenTTL = %s, want default", c.JoinTokenTTL)
	}
	if c.SocketPath != "/run/postern/api.sock" {
		t.Errorf("SocketPath = %q, want default", c.SocketPath)
	}
}
