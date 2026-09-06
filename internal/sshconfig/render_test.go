package sshconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var goldenAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func goldenHosts() []Host {
	return []Host{
		{
			Name: "nuc", LoginUser: "neo", Port: 2201,
			Tags: []string{"lab"}, Status: "degraded",
			TunnelOnline: true,
		},
		{
			Name: "macbook", LoginUser: "neo", Port: 2223,
			Tags: []string{"home", "laptop"}, Status: "online",
			TunnelOnline: true, AgentOnline: true,
		},
	}
}

func goldenJump() Jump {
	return Jump{User: "debian", Host: "vps.example.net", Port: 22}
}

func testdata(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "sshconfig", name)
}

func assertGolden(t *testing.T, got, name string) {
	t.Helper()
	want, err := os.ReadFile(testdata(t, name))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden %s mismatch\n got:\n%s\nwant:\n%s", name, got, want)
	}
}

func mustRender(t *testing.T, jump Jump, hosts []Host) string {
	t.Helper()
	got, err := Render(jump, hosts, "0.1.0", goldenAt)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRenderGoldens(t *testing.T) {
	t.Parallel()
	assertGolden(t, mustRender(t, goldenJump(), goldenHosts()), "no-identity.golden")

	j := goldenJump()
	j.IdentityFile = "/Users/neo/.ssh/id_ed25519"
	assertGolden(t, mustRender(t, j, goldenHosts()), "with-identity.golden")
}

func TestRenderOmitsDisabledIncludesOffline(t *testing.T) {
	t.Parallel()
	hosts := append(goldenHosts(), Host{
		Name: "pi", LoginUser: "pi", Port: 2202,
		Tags: []string{"lab"}, Status: "offline",
	}, Host{
		Name: "old", LoginUser: "neo", Port: 2203,
		Disabled: true, Status: "disabled",
	})
	got := mustRender(t, goldenJump(), hosts)
	if !strings.Contains(got, "Host pi\n") {
		t.Fatal("offline host omitted")
	}
	if !strings.Contains(got, "# postern: status=offline tags=lab") {
		t.Fatal("offline comment missing")
	}
	if strings.Contains(got, "Host old") {
		t.Fatal("disabled host was rendered")
	}
	if !strings.Contains(got, "HostKeyAlias postern-pi") || !strings.Contains(got, "CheckHostIP no") {
		t.Fatal("missing HostKeyAlias/CheckHostIP")
	}
}

func TestRenderSanitizesTags(t *testing.T) {
	t.Parallel()
	got := mustRender(t, goldenJump(), []Host{{
		Name: "macbook", LoginUser: "neo", Port: 2223,
		Tags: []string{"home#bad", "ok\nno"}, Status: "online",
	}})
	if strings.Contains(got, "home#bad") || strings.Contains(got, "ok\nno") {
		t.Fatalf("unsanitized tag in config:\n%s", got)
	}
	if !strings.Contains(got, "tags=homebad,okno") {
		t.Fatalf("tags = %s", got)
	}
}

func TestRenderNoInlineProxyJump(t *testing.T) {
	t.Parallel()
	got := mustRender(t, goldenJump(), goldenHosts())
	if strings.Contains(got, "ProxyJump debian@") || strings.Contains(got, "ProxyJump=user@") {
		t.Fatalf("inline ProxyJump:\n%s", got)
	}
	if !strings.Contains(got, "    ProxyJump postern-jump\n") {
		t.Fatal("missing ProxyJump postern-jump")
	}
}

func TestSSHGJumpIdentityFile(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not found")
	}
	dir := t.TempDir()
	id := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(id, []byte("dummy"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := goldenJump()
	j.IdentityFile = id
	body := mustRender(t, j, goldenHosts())
	cfg := filepath.Join(dir, "config")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	jumpG := sshG(t, cfg, "postern-jump")
	if !sshGHasIdentityFile(jumpG, id) {
		t.Fatalf("jump -G missing IdentityFile %s:\n%s", id, jumpG)
	}
	if !strings.Contains(jumpG, "identitiesonly yes") {
		t.Fatalf("jump -G missing identitiesonly:\n%s", jumpG)
	}

	destG := sshG(t, cfg, "macbook")
	if sshGHasIdentityFile(destG, id) {
		t.Fatalf("destination -G leaked jump IdentityFile:\n%s", destG)
	}
	if !strings.Contains(destG, "proxyjump postern-jump") {
		t.Fatalf("missing proxyjump:\n%s", destG)
	}
	if !strings.Contains(destG, "hostkeyalias postern-macbook") {
		t.Fatalf("missing hostkeyalias:\n%s", destG)
	}
	if !strings.Contains(destG, "checkhostip no") {
		t.Fatalf("missing checkhostip:\n%s", destG)
	}
	if !strings.Contains(destG, "identitiesonly yes") {
		t.Fatalf("destination missing identitiesonly:\n%s", destG)
	}
}

func TestRenderRejectsPoisonJump(t *testing.T) {
	t.Parallel()
	j := goldenJump()
	j.Host = "vps.example.net\nHost *\n    StrictHostKeyChecking no"
	if _, err := Render(j, goldenHosts(), "0.1.0", goldenAt); err == nil {
		t.Fatal("expected error for jump host newline")
	}
	j = goldenJump()
	j.User = "debian#root"
	if _, err := Render(j, goldenHosts(), "0.1.0", goldenAt); err == nil {
		t.Fatal("expected error for jump user")
	}
	j = goldenJump()
	j.IdentityFile = "/tmp/id with space"
	if _, err := Render(j, goldenHosts(), "0.1.0", goldenAt); err == nil {
		t.Fatal("expected error for identity_file space")
	}
}

func TestRenderSkipsInvalidHost(t *testing.T) {
	t.Parallel()
	got := mustRender(t, goldenJump(), []Host{
		{Name: "macbook", LoginUser: "neo", Port: 2223, Status: "online"},
		{Name: "bad\nHost *", LoginUser: "neo", Port: 2201, Status: "online"},
		{Name: "nuc", LoginUser: "neo\n", Port: 2202, Status: "online"},
		{Name: "pi", LoginUser: "pi", Port: 2203, Status: "offline#x"},
	})
	if !strings.Contains(got, "Host macbook\n") {
		t.Fatal("valid host omitted")
	}
	if strings.Contains(got, "Host *") || strings.Contains(got, "bad") {
		t.Fatalf("poison name interpolated:\n%s", got)
	}
	if strings.Contains(got, "Host nuc") || strings.Contains(got, "Host pi") {
		t.Fatalf("invalid user/status interpolated:\n%s", got)
	}
}

func TestSSHGNoIdentityOmitsJumpIdentitiesOnly(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not found")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	if err := os.WriteFile(cfg, []byte(mustRender(t, goldenJump(), goldenHosts())), 0o600); err != nil {
		t.Fatal(err)
	}
	jumpG := sshG(t, cfg, "postern-jump")
	if strings.Contains(jumpG, "identitiesonly yes") {
		t.Fatalf("jump IdentitiesOnly without IdentityFile:\n%s", jumpG)
	}
}

func sshG(t *testing.T, cfg, name string) string {
	t.Helper()
	cmd := exec.Command("ssh", "-G", "-F", cfg, name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ssh -G -F %s %s: %v\n%s", cfg, name, err, out)
	}
	return strings.ToLower(string(out))
}

func sshGHasIdentityFile(out, path string) bool {
	path = strings.ToLower(path)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "identityfile" && fields[1] == path {
			return true
		}
	}
	return false
}
