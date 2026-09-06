package agent

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAutosshArgs(t *testing.T) {
	t.Parallel()
	args := AutosshArgs("/tmp/ssh_config")
	want := []string{"-M", "0", "-N", "-F", "/tmp/ssh_config", "postern-tunnel"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", args, want)
	}
	joined := strings.ToLower(strings.Join(args, " "))
	if strings.Contains(joined, "clearallforwardings") {
		t.Fatalf("ClearAllForwardings in autossh argv: %v", args)
	}
}

func TestHeartbeatArgsNoRemoteForward(t *testing.T) {
	t.Parallel()
	args := HeartbeatArgs("/tmp/ssh_config.rpc")
	want := []string{"-T", "-F", "/tmp/ssh_config.rpc", "postern-tunnel", "postern-agent-api"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", args, want)
	}
	if containsArg(args, "-R") || containsArg(args, "ClearAllForwardings=yes") {
		t.Fatalf("forwarding flags in heartbeat argv: %v", args)
	}
}

func TestParseRemoteForwardListenPort(t *testing.T) {
	t.Parallel()
	body := []byte("Host postern-tunnel\n    RemoteForward 127.0.0.1:2223 127.0.0.1:22\n")
	got, err := parseRemoteForwardListenPort(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2223 {
		t.Fatalf("port = %d", got)
	}
	if _, err := parseRemoteForwardListenPort([]byte("Host postern-tunnel\n")); err == nil {
		t.Fatal("expected missing RemoteForward")
	}
}

func TestSSHDashGRemoteForwardPresentOnTunnelAbsentOnRPC(t *testing.T) {
	t.Parallel()
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh not found")
	}
	p, cfg, st := setupEnrolled(t)
	if err := WriteSSHConfigs(p, st, cfg); err != nil {
		t.Fatal(err)
	}

	tunnelOut := sshG(t, ssh, p.SSHConfig())
	if !sshGHasRemoteForward(tunnelOut) {
		t.Fatalf("tunnel ssh -G missing remoteforward:\n%s", tunnelOut)
	}
	if !strings.Contains(tunnelOut, "2223") {
		t.Fatalf("tunnel ssh -G missing port 2223:\n%s", tunnelOut)
	}
	if sshGHasClearAllForwardingsYes(tunnelOut) {
		t.Fatalf("tunnel ssh -G has clearallforwardings yes:\n%s", tunnelOut)
	}

	rpcOut := sshG(t, ssh, p.SSHConfigRPC())
	if sshGHasRemoteForward(rpcOut) {
		t.Fatalf("rpc ssh -G has remoteforward:\n%s", rpcOut)
	}
}

func sshG(t *testing.T, ssh, configPath string) string {
	t.Helper()
	cmd := exec.Command(ssh, "-G", "-F", configPath, "postern-tunnel")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("ssh -G: %v\nstderr: %s\nstdout: %s", err, ee.Stderr, out)
		}
		t.Fatal(err)
	}
	return string(out)
}

func sshGHasRemoteForward(stdout string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) > 0 && strings.EqualFold(fields[0], "remoteforward") {
			return true
		}
	}
	return false
}

func sshGHasClearAllForwardingsYes(stdout string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.EqualFold(fields[0], "clearallforwardings") && strings.EqualFold(fields[1], "yes") {
			return true
		}
	}
	return false
}

func TestAutosshEnvOverrides(t *testing.T) {
	t.Parallel()
	env := autosshEnv([]string{"PATH=/bin", "AUTOSSH_GATETIME=30", "AUTOSSH_PORT=1", "AUTOSSH_PIDFILE=/old.pid", "HOME=/tmp"}, "/tmp/autossh.pid")
	got := map[string]string{}
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			t.Fatalf("bad env %q", e)
		}
		if _, dup := got[k]; dup && (k == "AUTOSSH_GATETIME" || k == "AUTOSSH_PORT" || k == "AUTOSSH_PIDFILE") {
			t.Fatalf("duplicate %s", k)
		}
		got[k] = v
	}
	if got["AUTOSSH_GATETIME"] != "0" || got["AUTOSSH_PORT"] != "0" {
		t.Fatalf("env = %v", env)
	}
	if got["AUTOSSH_PIDFILE"] != "/tmp/autossh.pid" {
		t.Fatalf("pidfile = %q", got["AUTOSSH_PIDFILE"])
	}
}
