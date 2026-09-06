package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neitomic/postern/internal/config"
	"github.com/neitomic/postern/internal/sshconfig"
)

const sampleHostsJSON = `[
  {"name":"macbook","login_user":"neo","port":2223,"tags":["home","laptop"],"last_seen":1000000,"disabled":false,"agent_online":true,"tunnel_online":true,"status":"online"},
  {"name":"nuc","login_user":"neo","port":2201,"tags":["lab"],"last_seen":999280,"disabled":false,"agent_online":false,"tunnel_online":true,"status":"degraded"},
  {"name":"pi","login_user":"pi","port":2202,"tags":["lab"],"last_seen":827200,"disabled":false,"agent_online":false,"tunnel_online":false,"status":"offline"},
  {"name":"old","login_user":"neo","port":2203,"tags":[],"disabled":true,"agent_online":false,"tunnel_online":false,"status":"disabled"}
]`

func TestPrintLS(t *testing.T) {
	t.Parallel()
	hosts, err := parseHosts([]byte(sampleHostsJSON))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	now := time.Unix(1_000_004, 0)
	if err := printLS(&buf, hosts, now); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "NAME") || !strings.Contains(got, "TUNNEL") || !strings.Contains(got, "AGENT") {
		t.Fatalf("header: %q", got)
	}
	if !strings.Contains(got, "macbook") || !strings.Contains(got, "old") || !strings.Contains(got, "pi") {
		t.Fatalf("missing hosts:\n%s", got)
	}
	if !strings.Contains(got, "up") || !strings.Contains(got, "down") {
		t.Fatalf("missing up/down:\n%s", got)
	}
	if !strings.Contains(got, "4s ago") || !strings.Contains(got, "12m ago") || !strings.Contains(got, "2d ago") {
		t.Fatalf("last_seen:\n%s", got)
	}
}

func TestFmtLastSeen(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	if got := fmtLastSeen(nil, now); got != "-" {
		t.Fatalf("nil = %q", got)
	}
	ts := int64(996)
	if got := fmtLastSeen(&ts, now); got != "4s ago" {
		t.Fatalf("4s = %q", got)
	}
}

func TestControlSSHArgsIdentity(t *testing.T) {
	t.Parallel()
	cfg := config.Client{Server: "debian@vps.example.net", IdentityFile: "/id", PosterndPath: "/usr/bin/posternd"}
	args := controlSSHArgs(cfg, "/kh", "/usr/bin/posternd", "hosts", "list", "--json")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-T") || !strings.Contains(joined, "BatchMode=yes") {
		t.Fatalf("missing -T/BatchMode: %v", args)
	}
	if !strings.Contains(joined, "UserKnownHostsFile=/kh") {
		t.Fatalf("known_hosts: %v", args)
	}
	if !strings.Contains(joined, "IdentityFile=/id") || !strings.Contains(joined, "IdentitiesOnly=yes") {
		t.Fatalf("identity opts: %v", args)
	}
	if !strings.Contains(joined, "ForwardAgent=no") {
		t.Fatalf("ForwardAgent: %v", args)
	}
	if strings.Contains(joined, "ProxyJump") {
		t.Fatalf("unexpected ProxyJump: %v", args)
	}
	if args[len(args)-4] != "/usr/bin/posternd" || args[len(args)-3] != "hosts" || args[len(args)-2] != "list" || args[len(args)-1] != "--json" {
		t.Fatalf("remote: %v", args)
	}
	if args[len(args)-5] != "debian@vps.example.net" {
		t.Fatalf("dest: %v", args)
	}
}

func TestControlSSHArgsNoIdentityOmitsIdentitiesOnly(t *testing.T) {
	t.Parallel()
	cfg := config.Client{Server: "debian@vps.example.net"}
	args := controlSSHArgs(cfg, "/kh", "/usr/bin/posternd", "hosts", "list", "--json")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "IdentitiesOnly") || strings.Contains(joined, "IdentityFile") {
		t.Fatalf("IdentitiesOnly without IdentityFile: %v", args)
	}
}

func TestSSHWrapperArgv(t *testing.T) {
	t.Parallel()
	argv := sshWrapperArgv("/tmp/c", "macbook", []string{"-v"})
	if strings.Join(argv, " ") != "/usr/bin/ssh -F /tmp/c macbook -v" {
		t.Fatalf("argv = %v", argv)
	}
	for _, a := range argv {
		if strings.Contains(a, "ProxyJump=") || strings.HasPrefix(a, "-o") {
			t.Fatalf("inline jump opts: %v", argv)
		}
	}
}

func TestLSJSONAndTable(t *testing.T) {
	defer swapFetch(func() ([]byte, error) { return []byte(sampleHostsJSON), nil })()
	now = func() time.Time { return time.Unix(1_000_004, 0) }
	defer func() { now = time.Now }()

	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"ls", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name":"macbook"`) {
		t.Fatalf("json: %s", out.String())
	}

	root = newRoot()
	out.Reset()
	root.SetOut(&out)
	root.SetArgs([]string{"ls"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "macbook") || !strings.Contains(out.String(), "old") {
		t.Fatalf("table:\n%s", out.String())
	}
}

func TestSSHConfigStdoutAndWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeClientTOML(t, home, "")

	defer swapFetch(func() ([]byte, error) { return []byte(sampleHostsJSON), nil })()
	now = func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	defer func() { now = time.Now }()

	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"ssh-config"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, sshconfig.BeginMarker) || !strings.Contains(body, "Host postern-jump") {
		t.Fatalf("stdout:\n%s", body)
	}
	if !strings.Contains(body, "HostKeyAlias postern-macbook") || !strings.Contains(body, "CheckHostIP no") {
		t.Fatalf("missing HostKeyAlias:\n%s", body)
	}
	if strings.Contains(body, "Host old") {
		t.Fatalf("disabled rendered:\n%s", body)
	}
	if strings.Contains(body, "ProxyJump debian@") {
		t.Fatalf("inline ProxyJump:\n%s", body)
	}
	if strings.Contains(body, "IdentityFile") {
		t.Fatalf("identity without config:\n%s", body)
	}

	path := filepath.Join(home, "ssh_config")
	if err := os.WriteFile(path, []byte("Host keep\n    HostName x\n\n# BEGIN POSTERN MANAGED BLOCK\nold\n# END POSTERN MANAGED BLOCK\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root = newRoot()
	root.SetArgs([]string{"ssh-config", "--write", "--path", path})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "Host keep") {
		t.Fatalf("lost user stanza:\n%s", s)
	}
	if strings.Contains(s, "old\n") || strings.Count(s, sshconfig.BeginMarker) != 1 {
		t.Fatalf("markers not replaced:\n%s", s)
	}
	if !strings.Contains(s, "Host nuc") {
		t.Fatalf("missing host:\n%s", s)
	}
}

func TestSSHUnknownExit2(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeClientTOML(t, home, "")
	defer swapFetch(func() ([]byte, error) { return []byte(sampleHostsJSON), nil })()

	called := false
	execve = func([]string) error {
		called = true
		return nil
	}
	defer func() { execve = execveDefault }()

	err := runSSHWrapper(ioDiscard(), []string{"nope"})
	var ec exitCodeError
	if !errors.As(err, &ec) || ec.code != 2 {
		t.Fatalf("err = %v, want exit 2", err)
	}
	if called {
		t.Fatal("exec on unknown host")
	}

	err = runSSHWrapper(ioDiscard(), []string{"old"})
	if !errors.As(err, &ec) || ec.code != 2 {
		t.Fatalf("disabled err = %v, want exit 2", err)
	}
}

func TestSSHDegradedWarnsAndExecs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeClientTOML(t, home, "/tmp/id_ed25519")
	defer swapFetch(func() ([]byte, error) { return []byte(sampleHostsJSON), nil })()

	var argv []string
	execve = func(a []string) error {
		argv = append([]string{}, a...)
		return nil
	}
	defer func() { execve = execveDefault }()

	var stderr bytes.Buffer
	if err := runSSHWrapper(&stderr, []string{"nuc", "--", "-v"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "warning: host nuc is degraded") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if len(argv) < 5 || argv[0] != sshBin || argv[1] != "-F" || argv[3] != "nuc" || argv[4] != "-v" {
		t.Fatalf("argv = %v", argv)
	}
	for _, a := range argv {
		if strings.Contains(a, "ProxyJump=") || strings.HasPrefix(a, "-o") {
			t.Fatalf("inline dest opts: %v", argv)
		}
	}
	body, err := os.ReadFile(argv[2])
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "Host postern-jump") || !strings.Contains(s, "IdentityFile /tmp/id_ed25519") {
		t.Fatalf("generated:\n%s", s)
	}
	if !strings.Contains(s, "Host nuc") {
		t.Fatalf("missing dest stanza:\n%s", s)
	}
}

func TestSSHOfflineWarns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeClientTOML(t, home, "")
	defer swapFetch(func() ([]byte, error) { return []byte(sampleHostsJSON), nil })()
	execve = func([]string) error { return nil }
	defer func() { execve = execveDefault }()

	var stderr bytes.Buffer
	if err := runSSHWrapper(&stderr, []string{"pi"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "warning: host pi is offline") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func swapFetch(fn func() ([]byte, error)) func() {
	prev := fetchHostsJSON
	fetchHostsJSON = fn
	return func() { fetchHostsJSON = prev }
}

func writeClientTOML(t *testing.T, home, identity string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "postern")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "server = \"debian@vps.example.net\"\njump_user = \"debian\"\njump_host = \"vps.example.net\"\njump_port = 22\nposternd_path = \"/usr/bin/posternd\"\n"
	if identity != "" {
		body += "identity_file = \"" + identity + "\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ioDiscard() *bytes.Buffer { return &bytes.Buffer{} }
