package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestConfigDirDataDirXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	xdgCfg := filepath.Join(t.TempDir(), "xdg-config")
	xdgData := filepath.Join(t.TempDir(), "xdg-data")
	t.Setenv("XDG_CONFIG_HOME", xdgCfg)
	t.Setenv("XDG_DATA_HOME", xdgData)

	cfg, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	client, err := ClientPath()
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "darwin" {
		wantCfg := filepath.Join(home, ".config", "postern")
		wantData := filepath.Join(home, ".local", "share", "postern")
		if cfg != wantCfg {
			t.Fatalf("ConfigDir = %q, want %q", cfg, wantCfg)
		}
		if data != wantData {
			t.Fatalf("DataDir = %q, want %q", data, wantData)
		}
		ucd, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		if cfg == filepath.Join(ucd, "postern") || strings.Contains(cfg, "Application Support") {
			t.Fatalf("used macOS UserConfigDir: %q (UserConfigDir=%q)", cfg, ucd)
		}
	} else {
		if cfg != filepath.Join(xdgCfg, "postern") {
			t.Fatalf("ConfigDir = %q, want XDG", cfg)
		}
		if data != filepath.Join(xdgData, "postern") {
			t.Fatalf("DataDir = %q, want XDG", data)
		}
	}
	if client != filepath.Join(cfg, "config.toml") {
		t.Fatalf("ClientPath = %q", client)
	}
}

func TestParseServer(t *testing.T) {
	t.Parallel()
	user, host, port, err := ParseServer("debian@vps.example.net")
	if err != nil || user != "debian" || host != "vps.example.net" || port != 22 {
		t.Fatalf("got %q %q %d %v", user, host, port, err)
	}
	user, host, port, err = ParseServer("debian@vps.example.net:2222")
	if err != nil || user != "debian" || host != "vps.example.net" || port != 2222 {
		t.Fatalf("got %q %q %d %v", user, host, port, err)
	}
	user, host, port, err = ParseServer("vps.example.net")
	if err != nil || user != "" || host != "vps.example.net" || port != 22 {
		t.Fatalf("got %q %q %d %v", user, host, port, err)
	}
	if _, _, _, err := ParseServer(""); err == nil {
		t.Fatal("empty server")
	}
	if _, _, _, err := ParseServer("debian@host:99999"); err == nil {
		t.Fatal("bad port")
	}
}
