package agent

import (
	"strings"
	"testing"

	"github.com/neitomic/postern/internal/config"
)

func TestControlSSHArgsNoIdentitiesOnlyWithoutIdentityFile(t *testing.T) {
	t.Parallel()
	cfg := config.Client{
		Server:       "debian@vps.example.net",
		PosterndPath: "/usr/bin/posternd",
	}
	args, err := EnrollMachineArgs(cfg, "/tmp/postern-known_hosts")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "IdentitiesOnly") {
		t.Fatalf("IdentitiesOnly without IdentityFile: %v", args)
	}
	if containsArg(args, "-A") {
		t.Fatalf("agent forwarding: %v", args)
	}
	if !containsArg(args, "-T") {
		t.Fatal("missing -T")
	}
	if !containsKV(args, "-o", "BatchMode=yes") {
		t.Fatal("missing BatchMode")
	}
	if !containsKV(args, "-o", "ForwardAgent=no") {
		t.Fatal("missing ForwardAgent=no")
	}
	if !containsKV(args, "-o", "UserKnownHostsFile=/tmp/postern-known_hosts") {
		t.Fatal("missing UserKnownHostsFile")
	}
	if !containsKV(args, "-o", "GlobalKnownHostsFile=/dev/null") {
		t.Fatal("missing GlobalKnownHostsFile")
	}
	if args[len(args)-3] != "/usr/bin/posternd" || args[len(args)-2] != "enroll" || args[len(args)-1] != "--json" {
		t.Fatalf("remote command: %v", args)
	}
	if args[len(args)-4] != "debian@vps.example.net" {
		t.Fatalf("target = %v", args)
	}
}

func TestControlSSHArgsIdentityFile(t *testing.T) {
	t.Parallel()
	cfg := config.Client{
		Server:       "debian@vps.example.net",
		IdentityFile: "/home/neo/.ssh/id_ed25519",
		PosterndPath: "/opt/posternd",
	}
	args, err := EnrollMachineArgs(cfg, "/kh")
	if err != nil {
		t.Fatal(err)
	}
	if !containsKV(args, "-o", "IdentityFile=/home/neo/.ssh/id_ed25519") {
		t.Fatalf("missing IdentityFile: %v", args)
	}
	if !containsKV(args, "-o", "IdentitiesOnly=yes") {
		t.Fatalf("missing IdentitiesOnly: %v", args)
	}
	if containsArg(args, "-A") {
		t.Fatal("-A present")
	}
	if args[len(args)-3] != "/opt/posternd" {
		t.Fatalf("posternd_path: %v", args)
	}
}

func TestControlSSHArgsCustomPort(t *testing.T) {
	t.Parallel()
	cfg := config.Client{Server: "debian@vps.example.net:2222"}
	args, err := SubmitProbeArgs(cfg, "/kh")
	if err != nil {
		t.Fatal(err)
	}
	if !containsArg(args, "-p") || !containsArg(args, "2222") {
		t.Fatalf("missing -p 2222: %v", args)
	}
	if args[len(args)-1] != "true" {
		t.Fatalf("probe command: %v", args)
	}
	if containsArg(args, "-A") {
		t.Fatal("-A present")
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func containsKV(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
