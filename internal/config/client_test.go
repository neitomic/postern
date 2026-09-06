package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultClient(t *testing.T) {
	t.Parallel()
	c := DefaultClient()
	if c.LocalSSHPort != 22 {
		t.Errorf("LocalSSHPort = %d", c.LocalSSHPort)
	}
	if c.PosterndPath != "/usr/bin/posternd" {
		t.Errorf("PosterndPath = %q", c.PosterndPath)
	}
	if c.IdentityFile != "" {
		t.Errorf("IdentityFile = %q, want empty", c.IdentityFile)
	}
}

func TestClientSaveLoad(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "postern", "config.toml")
	want := Client{
		Server:       "debian@vps.example.net",
		Name:         "macbook",
		LoginUser:    "neo",
		LocalSSHPort: 22,
		IdentityFile: "",
		PosterndPath: "/usr/bin/posternd",
	}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config mode = %o, want 0600", perm)
	}
	got, err := LoadClient(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadClientDefaults(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("server = \"debian@vps\"\nname = \"nuc\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadClient(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server != "debian@vps" || c.Name != "nuc" {
		t.Fatalf("got %+v", c)
	}
	if c.LocalSSHPort != 22 || c.PosterndPath != "/usr/bin/posternd" {
		t.Fatalf("defaults not applied: %+v", c)
	}
}
