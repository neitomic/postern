package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neitomic/postern/internal/agent"
	"github.com/neitomic/postern/internal/config"
)

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	return home
}

func TestJoinHelpDocumentsSubmit(t *testing.T) {
	cmd := newRoot()
	cmd.SetArgs([]string{"join", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"--submit", "admin authority", "not the headless path", "--apply-response", "--token"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestJoinRequiresTokenOrApply(t *testing.T) {
	cmd := newRoot()
	cmd.SetArgs([]string{"join"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestJoinApplyResponseRejectsForce(t *testing.T) {
	cmd := newRoot()
	cmd.SetArgs([]string{"join", "--apply-response", "resp.json", "--force"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v", err)
	}
}

func TestAgentHelpListsSubcommands(t *testing.T) {
	cmd := newRoot()
	cmd.SetArgs([]string{"agent", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"install", "uninstall", "enable", "disable", "status", "run", "set-name"} {
		if !strings.Contains(out, want) {
			t.Fatalf("agent help missing %q:\n%s", want, out)
		}
	}
}

func TestAgentSetNameRequiresArg(t *testing.T) {
	cmd := newRoot()
	cmd.SetArgs([]string{"agent", "set-name"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRootHelpListsInstallAndConfig(t *testing.T) {
	cmd := newRoot()
	cmd.SetArgs([]string{"--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"onboard", "install", "uninstall", "config", "init"} {
		if !strings.Contains(out, want) {
			t.Fatalf("root help missing %q:\n%s", want, out)
		}
	}
}

func TestOnboardHelp(t *testing.T) {
	cmd := newRoot()
	cmd.SetArgs([]string{"onboard", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"--server", "--name", "--token", "--submit", "--apply-response", "ONBOARDING.md", "not for a nuc/pi"} {
		if !strings.Contains(out, want) {
			t.Fatalf("onboard help missing %q:\n%s", want, out)
		}
	}
}

func TestOnboardWritesConfigWithoutToken(t *testing.T) {
	_ = isolateHome(t)
	cmd := newRoot()
	cmd.SetArgs([]string{
		"onboard",
		"--server", "debian@vps.example.net",
		"--name", "macbook",
		"--login-user", "neo",
		"--accept-host-key=false",
		"--no-install",
	})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "token issue") {
		t.Fatalf("expected next-step token issue:\n%s", buf.String())
	}
	p, err := agent.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadClient(p.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "debian@vps.example.net" || cfg.Name != "macbook" || cfg.LoginUser != "neo" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if _, err := os.Stat(p.IdentityFile()); err != nil {
		t.Fatalf("tunnel key: %v", err)
	}
}

func TestOnboardRequiresServer(t *testing.T) {
	_ = isolateHome(t)
	cmd := newRoot()
	cmd.SetArgs([]string{"onboard", "--no-install"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestConfigSetGetShow(t *testing.T) {
	_ = isolateHome(t)
	run := func(args ...string) string {
		t.Helper()
		cmd := newRoot()
		cmd.SetArgs(args)
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v\n%s", err, buf.String())
		}
		return buf.String()
	}
	out := run("config", "set", "server", "debian@vps.example.net")
	if !strings.Contains(out, "debian@vps.example.net") {
		t.Fatalf("set output = %q", out)
	}
	if got := strings.TrimSpace(run("config", "get", "server")); got != "debian@vps.example.net" {
		t.Fatalf("get server = %q", got)
	}
	run("config", "set", "name", "macbook")
	run("config", "set", "login-user", "neo")
	run("config", "set", "local-ssh-port", "2222")
	if got := strings.TrimSpace(run("config", "get", "name")); got != "macbook" {
		t.Fatalf("get name = %q", got)
	}
	if got := strings.TrimSpace(run("config", "get", "local-ssh-port")); got != "2222" {
		t.Fatalf("get port = %q", got)
	}
	show := run("config", "show")
	if !strings.Contains(show, "config.toml") || !strings.Contains(show, "macbook") {
		t.Fatalf("show = %s", show)
	}
	p, err := agent.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.IdentityFile()); err != nil {
		t.Fatalf("config set should create tunnel key: %v", err)
	}
}

func TestConfigSetRejectsUnknownKey(t *testing.T) {
	_ = isolateHome(t)
	cmd := newRoot()
	cmd.SetArgs([]string{"config", "set", "token", "nope"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestInstallCopiesBinaryNoService(t *testing.T) {
	home := isolateHome(t)
	binDir := home + "/opt"
	cmd := newRoot()
	cmd.SetArgs([]string{"install", "--bin-dir", binDir, "--no-service"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "installed") || !strings.Contains(out, binDir) {
		t.Fatalf("output = %s", out)
	}
	if !strings.Contains(out, "config set server") {
		t.Fatalf("missing next steps:\n%s", out)
	}
	st, err := os.Stat(binDir + "/postern")
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o755 {
		t.Fatalf("mode = %o", perm)
	}
}

func TestInstallWritesUnitNoEnable(t *testing.T) {
	home := isolateHome(t)
	cmd := newRoot()
	cmd.SetArgs([]string{"install", "--no-enable"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "unit written") {
		t.Fatalf("output = %s", buf.String())
	}
	switch {
	case fileExists(home + "/Library/LaunchAgents/com.postern.agent.plist"):
	case fileExists(home + "/.config/systemd/user/postern-agent.service"):
	default:
		t.Fatalf("no unit file under %s", home)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestInitMergesExistingConfig(t *testing.T) {
	_ = isolateHome(t)
	run := func(args ...string) {
		t.Helper()
		cmd := newRoot()
		cmd.SetArgs(args)
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "--server", "debian@vps.example.net", "--name", "macbook", "--login-user", "neo", "--local-ssh-port", "2222")
	run("init", "--server", "debian@other.example.net")
	p, err := agent.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadClient(p.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "macbook" || cfg.LoginUser != "neo" || cfg.LocalSSHPort != 2222 {
		t.Fatalf("existing fields wiped: %+v", cfg)
	}
	if cfg.Server != "debian@other.example.net" {
		t.Fatalf("server = %q", cfg.Server)
	}
}
