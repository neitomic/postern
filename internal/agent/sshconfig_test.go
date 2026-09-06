package agent

import (
	"os"
	"strings"
	"testing"
)

func TestWriteSSHConfigsRemoteForwardAndNoClearAll(t *testing.T) {
	t.Parallel()
	p, cfg := setupMachine(t)
	st := State{
		Name:        "macbook",
		Port:        2223,
		TunnelUser:  "postern",
		VPSHostname: "vps.example.net",
		EnrolledAt:  "2026-09-06T12:00:00Z",
	}
	if err := WriteSSHConfigs(p, st, cfg); err != nil {
		t.Fatal(err)
	}

	tunnel, err := os.ReadFile(p.SSHConfig())
	if err != nil {
		t.Fatal(err)
	}
	rpc, err := os.ReadFile(p.SSHConfigRPC())
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{tunnel, rpc} {
		if hasSSHKeyword(string(body), "ClearAllForwardings") {
			t.Fatalf("ClearAllForwardings present:\n%s", body)
		}
		if strings.Contains(string(body), "0.0.0.0") {
			t.Fatalf("0.0.0.0 present:\n%s", body)
		}
		if strings.Contains(string(body), "localhost") {
			t.Fatalf("localhost present:\n%s", body)
		}
	}
	info, err := os.Stat(p.SSHConfig())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("ssh_config mode = %o", perm)
	}
	rpcInfo, err := os.Stat(p.SSHConfigRPC())
	if err != nil {
		t.Fatal(err)
	}
	if perm := rpcInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("ssh_config.rpc mode = %o", perm)
	}

	wantFwd := "RemoteForward 127.0.0.1:2223 127.0.0.1:22"
	if !strings.Contains(string(tunnel), wantFwd) {
		t.Fatalf("tunnel missing %q:\n%s", wantFwd, tunnel)
	}
	if !hasSSHKeyword(string(tunnel), "RemoteForward") {
		t.Fatalf("tunnel missing RemoteForward keyword:\n%s", tunnel)
	}
	if hasSSHKeyword(string(rpc), "RemoteForward") {
		t.Fatalf("rpc has RemoteForward:\n%s", rpc)
	}
	for _, body := range []string{string(tunnel), string(rpc)} {
		if !strings.Contains(body, "Host postern-tunnel") {
			t.Fatal("missing Host postern-tunnel")
		}
		if !strings.Contains(body, "User postern") {
			t.Fatal("missing User postern")
		}
		if !strings.Contains(body, "HostName vps.example.net") {
			t.Fatal("missing HostName")
		}
		if !strings.Contains(body, "IdentityFile "+p.IdentityFile()) {
			t.Fatal("missing IdentityFile")
		}
		if !strings.Contains(body, "UserKnownHostsFile "+p.KnownHosts()) {
			t.Fatal("missing UserKnownHostsFile")
		}
		if !strings.Contains(body, "IdentitiesOnly yes") {
			t.Fatal("missing IdentitiesOnly")
		}
	}
}

func TestWriteSSHConfigsLocalSSHPortAndJumpPort(t *testing.T) {
	t.Parallel()
	p, cfg := setupMachine(t)
	cfg.LocalSSHPort = 2222
	cfg.Server = "debian@vps.example.net:2222"
	st := State{Name: "macbook", Port: 2201, TunnelUser: "postern", VPSHostname: "vps.example.net"}
	if err := WriteSSHConfigs(p, st, cfg); err != nil {
		t.Fatal(err)
	}
	tunnel, err := os.ReadFile(p.SSHConfig())
	if err != nil {
		t.Fatal(err)
	}
	body := string(tunnel)
	if !strings.Contains(body, "RemoteForward 127.0.0.1:2201 127.0.0.1:2222") {
		t.Fatalf("forward = %s", body)
	}
	if !strings.Contains(body, "    Port 2222\n") {
		t.Fatalf("jump port = %s", body)
	}
	rpc, err := os.ReadFile(p.SSHConfigRPC())
	if err != nil {
		t.Fatal(err)
	}
	if hasSSHKeyword(string(rpc), "RemoteForward") {
		t.Fatalf("rpc has RemoteForward: %s", rpc)
	}
}

func hasSSHKeyword(body, key string) bool {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], key) {
			return true
		}
	}
	return false
}
