package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRenderLaunchAgentPlist(t *testing.T) {
	t.Parallel()
	exe := "/Users/neo/bin/postern"
	home := "/Users/neo"
	logPath := "/Users/neo/.local/share/postern/agent.log"
	body := RenderLaunchAgentPlist(exe, home, logPath)
	for _, want := range []string{
		"<key>Label</key>",
		"<string>com.postern.agent</string>",
		"<key>KeepAlive</key>",
		"<true/>",
		"<key>RunAtLoad</key>",
		"<key>ThrottleInterval</key>",
		"<integer>5</integer>",
		"<string>/Users/neo/bin/postern</string>",
		"<string>agent</string>",
		"<string>run</string>",
		"<key>HOME</key>",
		"<string>/Users/neo</string>",
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"<key>AUTOSSH_GATETIME</key>",
		"<string>0</string>",
		"<key>AUTOSSH_PORT</key>",
		"<string>0</string>",
		"<key>StandardOutPath</key>",
		"<string>/Users/neo/.local/share/postern/agent.log</string>",
		"<key>StandardErrorPath</key>",
		"<key>ProcessType</key>",
		"<string>Background</string>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plist missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "ClearAllForwardings") {
		t.Fatal("plist should not mention ClearAllForwardings")
	}
}

func TestRenderSystemdUserUnit(t *testing.T) {
	t.Parallel()
	body := RenderSystemdUserUnit("/home/neo/.local/bin/postern")
	for _, want := range []string{
		"[Unit]",
		"Description=Postern reverse SSH tunnel agent",
		"[Service]",
		"Type=simple",
		"ExecStart=/home/neo/.local/bin/postern agent run",
		"Restart=always",
		"RestartSec=5",
		"Environment=AUTOSSH_GATETIME=0",
		"Environment=AUTOSSH_PORT=0",
		"[Install]",
		"WantedBy=default.target",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("unit missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "network-online") {
		t.Fatalf("must not depend on network-online.target:\n%s", body)
	}
	if strings.Contains(body, "ClearAllForwardings") {
		t.Fatal("unit should not mention ClearAllForwardings")
	}
}

func TestInstallRefusesWithoutState(t *testing.T) {
	t.Parallel()
	p, _ := setupMachine(t)
	home := t.TempDir()
	err := Install(p, "/usr/bin/postern", home)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "state.json") && !strings.Contains(err.Error(), "not enrolled") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(LaunchAgentPlistPath(home)); !os.IsNotExist(err) {
		t.Fatal("plist written without state.json")
	}
	if _, err := os.Stat(SystemdUserUnitPath(home)); !os.IsNotExist(err) {
		t.Fatal("systemd unit written without state.json")
	}
}

func TestInstallRefusesWithoutPort(t *testing.T) {
	t.Parallel()
	p, _ := setupMachine(t)
	if err := SaveState(p.StateFile(), State{Name: "macbook", TunnelUser: "postern", VPSHostname: "vps.example.net"}); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	err := Install(p, "/usr/bin/postern", home)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "port missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteAgentUnitWithoutEnroll(t *testing.T) {
	t.Parallel()
	p, _ := setupMachine(t)
	_ = os.Remove(p.StateFile())
	home := t.TempDir()
	exe := "/usr/bin/postern"
	if err := WriteAgentUnit(p, exe, home); err != nil {
		t.Fatal(err)
	}
	switch runtime.GOOS {
	case "darwin":
		if _, err := os.Stat(LaunchAgentPlistPath(home)); err != nil {
			t.Fatal(err)
		}
	case "linux":
		if _, err := os.Stat(SystemdUserUnitPath(home)); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported GOOS %s", runtime.GOOS)
	}
}

func TestInstallWritesUnit(t *testing.T) {
	t.Parallel()
	p, _, _ := setupEnrolled(t)
	home := t.TempDir()
	exe := "/Users/neo/bin/postern"
	if runtime.GOOS == "linux" {
		exe = "/home/neo/.local/bin/postern"
	}
	if err := Install(p, exe, home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.SSHConfig()); err != nil {
		t.Fatalf("install should write ssh_config: %v", err)
	}
	switch runtime.GOOS {
	case "darwin":
		path := LaunchAgentPlistPath(home)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := string(body)
		if !strings.Contains(s, "<string>com.postern.agent</string>") {
			t.Fatalf("missing Label:\n%s", s)
		}
		if !strings.Contains(s, "<key>KeepAlive</key>") || !strings.Contains(s, "<key>RunAtLoad</key>") {
			t.Fatalf("missing KeepAlive/RunAtLoad:\n%s", s)
		}
		if !strings.Contains(s, exe) {
			t.Fatalf("missing ExecStart path:\n%s", s)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Fatalf("plist mode = %o", perm)
		}
	case "linux":
		path := SystemdUserUnitPath(home)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := string(body)
		if strings.Contains(s, "network-online") {
			t.Fatalf("network-online.target present:\n%s", s)
		}
		if !strings.Contains(s, "Restart=always") || !strings.Contains(s, "RestartSec=5") {
			t.Fatalf("missing Restart:\n%s", s)
		}
		if !strings.Contains(s, exe+" agent run") {
			t.Fatalf("missing ExecStart:\n%s", s)
		}
	default:
		t.Fatalf("unsupported GOOS %s", runtime.GOOS)
	}
}

func TestInstallMayWriteSSHConfigsButIsNotOnlyRenderer(t *testing.T) {
	t.Parallel()
	p, cfg, st := setupEnrolled(t)
	home := t.TempDir()
	if err := Install(p, "/usr/bin/postern", home); err != nil {
		t.Fatal(err)
	}
	st.Port = 2208
	if err := SaveState(p.StateFile(), st); err != nil {
		t.Fatal(err)
	}
	if err := WriteSSHConfigs(p, st, cfg); err != nil {
		t.Fatal(err)
	}
	tunnel, err := os.ReadFile(p.SSHConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tunnel), "RemoteForward 127.0.0.1:2208") {
		t.Fatalf("WriteSSHConfigs after install must pick up new port:\n%s", tunnel)
	}
}

func TestPlistXMLEscapesExecutable(t *testing.T) {
	t.Parallel()
	body := RenderLaunchAgentPlist(`/tmp/postern & "bin"`, `/Users/a&b`, `/tmp/a&b.log`)
	if strings.Contains(body, `/tmp/postern & "bin"`) {
		t.Fatal("unescaped ampersand in plist")
	}
	if !strings.Contains(body, "&amp;") {
		t.Fatalf("expected xml escape:\n%s", body)
	}
}

func TestLaunchdDisableThenBootout(t *testing.T) {
	t.Parallel()
	steps := launchdDisableArgs()
	if len(steps) != 2 {
		t.Fatalf("steps = %v", steps)
	}
	if steps[0][0] != "disable" {
		t.Fatalf("first step = %v, want disable (persistent)", steps[0])
	}
	if steps[1][0] != "bootout" {
		t.Fatalf("second step = %v, want bootout", steps[1])
	}
	if !strings.Contains(steps[0][1], launchdLabel) || !strings.Contains(steps[1][1], launchdLabel) {
		t.Fatalf("service target missing label: %v", steps)
	}
}

func TestLaunchdEnableBootoutThenBootstrap(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	plist := LaunchAgentPlistPath(home)
	steps := launchdEnableArgs(home)
	if len(steps) != 4 {
		t.Fatalf("steps = %v", steps)
	}
	if steps[0][0] != "bootout" {
		t.Fatalf("first = %v, want bootout so the on-disk plist is reloaded", steps[0])
	}
	if steps[1][0] != "enable" {
		t.Fatalf("second = %v, want enable before bootstrap (disabled labels reject bootstrap)", steps[1])
	}
	if steps[2][0] != "bootstrap" || !containsArg(steps[2], plist) {
		t.Fatalf("third = %v, want bootstrap of %s", steps[2], plist)
	}
	enableAt, bootstrapAt := -1, -1
	for i, step := range steps {
		if len(step) > 0 && step[0] == "enable" {
			enableAt = i
		}
		if len(step) > 0 && step[0] == "bootstrap" {
			bootstrapAt = i
		}
	}
	if enableAt < 0 || bootstrapAt < 0 || enableAt > bootstrapAt {
		t.Fatalf("enable must precede bootstrap: %v", steps)
	}
	if steps[3][0] != "kickstart" || !containsArg(steps[3], "-k") {
		t.Fatalf("fourth = %v, want kickstart -k", steps[3])
	}
}

func TestSystemdEnableRestartsWhenAlreadyActive(t *testing.T) {
	t.Parallel()
	inactive := systemdEnableArgs(false)
	for _, step := range inactive {
		if containsArg(step, "restart") {
			t.Fatalf("inactive plan should not restart: %v", inactive)
		}
	}
	if !containsArg(inactive[0], "daemon-reload") {
		t.Fatalf("missing daemon-reload: %v", inactive)
	}
	if !containsArg(inactive[1], "enable") || !containsArg(inactive[1], "--now") {
		t.Fatalf("missing enable --now: %v", inactive)
	}

	active := systemdEnableArgs(true)
	if len(active) != 3 {
		t.Fatalf("active plan = %v", active)
	}
	last := active[len(active)-1]
	if !containsArg(last, "restart") || containsArg(last, "network-online") {
		t.Fatalf("active plan should restart after enable --now: %v", active)
	}
}

func TestSystemdUserUnitPath(t *testing.T) {
	t.Parallel()
	got := SystemdUserUnitPath("/home/neo")
	wantSuffix := filepath.Join("systemd", "user", "postern-agent.service")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("path = %q", got)
	}
	if !strings.Contains(got, "postern-agent.service") {
		t.Fatalf("path = %q", got)
	}
}
