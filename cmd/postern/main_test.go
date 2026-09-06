package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/neitomic/postern/internal/agent"
	"github.com/neitomic/postern/internal/config"
)

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

func TestInitMergesExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
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
