package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/neitomic/postern/internal/config"
)

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunAbortsOnPortMismatch(t *testing.T) {
	t.Parallel()
	p, _, st := setupEnrolled(t)
	started := filepath.Join(t.TempDir(), "started")
	fake := writeScript(t, t.TempDir(), "autossh", "#!/bin/sh\necho started >\""+started+"\"\nexit 1\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := Run(ctx, p, RunOptions{
		Autossh: fake,
		SSH:     fake,
		writeSSHConfigs: func(p Paths, st State, cfg config.Client) error {
			st.Port = 2200
			return WriteSSHConfigs(p, st, cfg)
		},
	})
	if err == nil {
		t.Fatal("expected port mismatch")
	}
	if !strings.Contains(err.Error(), "2200") || !strings.Contains(err.Error(), "2223") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "state.json.port") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(started); !os.IsNotExist(err) {
		t.Fatal("autossh started despite port mismatch")
	}
	_ = st
}

func TestRunAbortsIfPortMissing(t *testing.T) {
	t.Parallel()
	p, _ := setupMachine(t)
	if err := SaveState(p.StateFile(), State{Name: "macbook", TunnelUser: "postern", VPSHostname: "vps.example.net"}); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), p, RunOptions{
		LookPath: func(string) (string, error) { return "/bin/false", nil },
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "port missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunMissingAutossh(t *testing.T) {
	t.Parallel()
	p, _, _ := setupEnrolled(t)
	err := Run(context.Background(), p, RunOptions{
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "install autossh" {
		t.Fatalf("err = %v", err)
	}
}

func TestRunChildDeathExitsParent(t *testing.T) {
	t.Parallel()
	p, _, st := setupEnrolled(t)
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	fake := writeScript(t, dir, "autossh", "#!/bin/sh\necho \"$@\" >\""+argvFile+"\"\nexit 7\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := Run(ctx, p, RunOptions{
		Autossh:   fake,
		SSH:       fake,
		Heartbeat: time.Hour,
	})
	if err == nil {
		t.Fatal("expected autossh_exit")
	}
	if !strings.Contains(err.Error(), "autossh_exit") {
		t.Fatalf("err = %v", err)
	}
	argv, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(argv))
	want := strings.Join(AutosshArgs(p.SSHConfig()), " ")
	if got != want {
		t.Fatalf("autossh argv = %q, want %q", got, want)
	}
	if strings.Contains(strings.ToLower(got), "clearallforwardings") {
		t.Fatalf("ClearAllForwardings in child argv: %q", got)
	}
	tunnel, err := os.ReadFile(p.SSHConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tunnel), "RemoteForward 127.0.0.1:2223 127.0.0.1:22") {
		t.Fatalf("run did not write ssh_config from state:\n%s", tunnel)
	}
	_ = st
}

func TestRunRegeneratesSSHConfigFromStateWithoutInstall(t *testing.T) {
	t.Parallel()
	p, cfg, st := setupEnrolled(t)
	stale := st
	stale.Port = 2200
	if err := WriteSSHConfigs(p, stale, cfg); err != nil {
		t.Fatal(err)
	}
	fake := writeScript(t, t.TempDir(), "autossh", "#!/bin/sh\nexit 1\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := Run(ctx, p, RunOptions{Autossh: fake, SSH: fake, Heartbeat: time.Hour})
	if err == nil || !strings.Contains(err.Error(), "autossh_exit") {
		t.Fatalf("err = %v", err)
	}
	tunnel, err := os.ReadFile(p.SSHConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tunnel), "RemoteForward 127.0.0.1:2223 127.0.0.1:22") {
		t.Fatalf("did not regenerate from state.json port:\n%s", tunnel)
	}
	if strings.Contains(string(tunnel), "127.0.0.1:2200") {
		t.Fatalf("stale port remains:\n%s", tunnel)
	}
}

func TestRunHeartbeatWhileChildAliveThenExitOnDeath(t *testing.T) {
	t.Parallel()
	p, _, _ := setupEnrolled(t)
	dir := t.TempDir()
	hbFile := filepath.Join(dir, "hb")
	lock := filepath.Join(dir, "lock")
	autossh := writeScript(t, dir, "autossh", "#!/bin/sh\ntrap 'exit 0' TERM\ntouch \""+lock+"\"\nwhile [ -f \""+lock+"\" ]; do sleep 0.05; done\nexit 3\n")
	ssh := writeScript(t, dir, "ssh", "#!/bin/sh\ncat > \""+hbFile+"\"\nexit 0\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, p, RunOptions{
			Autossh:   autossh,
			SSH:       ssh,
			Heartbeat: 20 * time.Millisecond,
		})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(hbFile)
		if err == nil && strings.Contains(string(b), `"op":"heartbeat"`) && strings.Contains(string(b), `"v":1`) {
			_ = os.Remove(lock)
			err := <-done
			if err == nil || !strings.Contains(err.Error(), "autossh_exit") {
				t.Fatalf("after child death err = %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = os.Remove(lock)
	<-done
	t.Fatal("heartbeat was not sent while child was alive")
}

func TestRunShutdownSIGKILLsStubbornChild(t *testing.T) {
	t.Parallel()
	p, _, _ := setupEnrolled(t)
	dir := t.TempDir()
	alive := filepath.Join(dir, "alive")
	autossh := writeScript(t, dir, "autossh", "#!/bin/sh\ntrap '' TERM\ntouch \""+alive+"\"\nwhile true; do sleep 0.05; done\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, p, RunOptions{
			Autossh:      autossh,
			SSH:          autossh,
			Heartbeat:    time.Hour,
			ShutdownWait: 200 * time.Millisecond,
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(alive); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(alive); err != nil {
		cancel()
		<-done
		t.Fatal("child did not start")
	}
	start := time.Now()
	cancel()
	err := <-done
	if err != nil {
		t.Fatalf("shutdown err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("shutdown did not SIGKILL a TERM-ignoring child")
	}
}

func TestRunShutdownReturnsNilForCooperativeChild(t *testing.T) {
	t.Parallel()
	p, _, _ := setupEnrolled(t)
	dir := t.TempDir()
	alive := filepath.Join(dir, "alive")
	autossh := writeScript(t, dir, "autossh", "#!/bin/sh\ntrap 'exit 0' TERM\ntouch \""+alive+"\"\nwhile true; do sleep 0.05; done\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, p, RunOptions{
			Autossh:      autossh,
			SSH:          autossh,
			Heartbeat:    time.Hour,
			ShutdownWait: 2 * time.Second,
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(alive); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	cancel()
	err := <-done
	if err != nil {
		t.Fatalf("cooperative shutdown err = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("SIGTERM-cooperative child should not wait for SIGKILL timeout")
	}
}

func TestAutosshSysProcAttrSetpgid(t *testing.T) {
	t.Parallel()
	if !autosshSysProcAttr().Setpgid {
		t.Fatal("Setpgid required so SIGTERM/SIGKILL reach autossh+ssh")
	}
}

func TestReapStaleAutossh(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "autossh.pid")
	script := writeScript(t, dir, "autossh", "#!/bin/sh\ntrap '' TERM\nwhile true; do sleep 0.05; done\n")
	cmd := exec.Command(script)
	cmd.SysProcAttr = autosshSysProcAttr()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	defer func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
	if err := os.WriteFile(pidfile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reapStaleAutossh(pidfile)
	if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
		t.Fatal("pidfile should be removed")
	}
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
		t.Fatal("stale autossh still alive")
	}
}

func TestCheckRemoteForwardMismatch(t *testing.T) {
	t.Parallel()
	p, cfg, st := setupEnrolled(t)
	if err := WriteSSHConfigs(p, st, cfg); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(p.SSHConfig())
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.ReplaceAll(string(body), "127.0.0.1:2223", "127.0.0.1:2200")
	if err := os.WriteFile(p.SSHConfig(), []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	err = checkRemoteForward(p.SSHConfig(), st.Port)
	if err == nil {
		t.Fatal("expected mismatch")
	}
	if !strings.Contains(err.Error(), "2200") || !strings.Contains(err.Error(), "2223") {
		t.Fatalf("err = %v", err)
	}
}
