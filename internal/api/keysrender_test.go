package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neitomic/postern/internal/store"
)

const (
	testPub1 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBktPaVv6ld6eJT8/J9fyUUFiPzEBlI+8HfbMeGl1j/6"
	testPub2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBEfSo4bZcwBm8mvHGtnVnnsHO6Dzy99Wxjxm/+ztkFu"
)

func testdata(t *testing.T, elem ...string) string {
	t.Helper()
	parts := append([]string{"..", "..", "testdata"}, elem...)
	return filepath.Join(parts...)
}

func assertGolden(t *testing.T, got, name string) {
	t.Helper()
	path := testdata(t, "authorized_keys", name)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden %s mismatch\n got:\n%s\nwant:\n%s", name, got, want)
	}
}

func TestFormatAuthorizedKeysGoldens(t *testing.T) {
	t.Parallel()
	macbook := &store.Host{Name: "macbook", Port: 2223, Pubkey: testPub1}
	nuc := &store.Host{Name: "nuc", Port: 2201, Pubkey: testPub2}

	assertGolden(t, FormatAuthorizedKeys([]*store.Host{macbook}), "one-host.golden")
	assertGolden(t, FormatAuthorizedKeys([]*store.Host{nuc, macbook}), "two-hosts.golden")

	disabled := &store.Host{Name: "nuc", Port: 2201, Pubkey: testPub2, Disabled: true}
	assertGolden(t, FormatAuthorizedKeys([]*store.Host{macbook, disabled}), "disabled-omitted.golden")
}

func TestFormatAuthorizedKeysNoPermitOpenOrLocalhostListen(t *testing.T) {
	t.Parallel()
	out := FormatAuthorizedKeys([]*store.Host{
		{Name: "macbook", Port: 2223, Pubkey: testPub1},
		{Name: "nuc", Port: 2201, Pubkey: testPub2, Disabled: true},
	})
	if strings.Contains(out, "permitopen=") {
		t.Fatal("rendered permitopen=")
	}
	if strings.Contains(out, `permitlisten="localhost`) {
		t.Fatal("permitlisten uses localhost")
	}
	if strings.Contains(out, `permitlisten="0.0.0.0`) || strings.Contains(out, `permitlisten="::1`) {
		t.Fatal("permitlisten is not 127.0.0.1")
	}
	if !strings.Contains(out, `permitlisten="127.0.0.1:2223"`) {
		t.Fatal("missing 127.0.0.1 permitlisten")
	}
	if strings.Contains(out, "postern:nuc") {
		t.Fatal("disabled host was rendered")
	}
}

func TestWriteAuthorizedKeysAtomicMode(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "authorized_keys")
	if err := WriteAuthorizedKeys(path, []*store.Host{{Name: "macbook", Port: 2223, Pubkey: testPub1}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != keysFileMode {
		t.Fatalf("mode = %o, want %o", perm, keysFileMode)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, string(got), "one-host.golden")
	if leftovers, _ := filepath.Glob(path + "*.tmp"); len(leftovers) != 0 {
		t.Fatalf("tmp leftover: %v", leftovers)
	}
}

func TestSSHDFixture(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "contrib", "sshd", "50-postern.conf"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "Match User postern") {
		t.Fatal("missing Match User postern")
	}
	if !strings.Contains(body, "PermitOpen none") {
		t.Fatal("missing PermitOpen none")
	}
	if !strings.Contains(body, "PermitUserEnvironment POSTERN_NAME,POSTERN_ROLE") {
		t.Fatal("missing PermitUserEnvironment")
	}
	if !strings.Contains(body, "ForceCommand /usr/bin/posternd-shell") {
		t.Fatal("missing ForceCommand")
	}
	if strings.Contains(strings.ToLower(body), "permitopen=") {
		t.Fatal("fixture has permitopen= (belongs on keys, not sshd_config)")
	}
	trimmed := strings.TrimSpace(body)
	if !strings.HasSuffix(trimmed, "Match all") {
		t.Fatalf("drop-in must end with Match all, got %q", trimmed[len(trimmed)-20:])
	}
}
