package agent

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neitomic/postern/internal/auth"
	"golang.org/x/crypto/ssh"
)

func TestGenerateKeyEd25519(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	priv := filepath.Join(dir, "id_ed25519")
	if err := GenerateKey(priv, "postern:macbook"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(priv)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("private mode = %o, want 0600", perm)
	}
	pubInfo, err := os.Stat(priv + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if perm := pubInfo.Mode().Perm(); perm != 0o644 {
		t.Fatalf("public mode = %o, want 0644", perm)
	}

	pemBytes, err := os.ReadFile(priv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pemBytes), "BEGIN OPENSSH PRIVATE KEY") {
		t.Fatalf("private key is not OpenSSH PEM: %s", pemBytes[:min(80, len(pemBytes))])
	}
	raw, err := ssh.ParseRawPrivateKey(pemBytes)
	if err != nil {
		t.Fatal(err)
	}
	switch k := raw.(type) {
	case ed25519.PrivateKey:
		if len(k) != ed25519.PrivateKeySize {
			t.Fatalf("ed25519 size = %d", len(k))
		}
	case *ed25519.PrivateKey:
		if len(*k) != ed25519.PrivateKeySize {
			t.Fatalf("ed25519 size = %d", len(*k))
		}
	default:
		t.Fatalf("private key type %T, want ed25519", raw)
	}

	pubLine, err := os.ReadFile(priv + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	key, comment, _, _, err := ssh.ParseAuthorizedKey(pubLine)
	if err != nil {
		t.Fatal(err)
	}
	if key.Type() != ssh.KeyAlgoED25519 {
		t.Fatalf("pub type = %q", key.Type())
	}
	if comment != "postern:macbook" {
		t.Fatalf("comment = %q", comment)
	}
	fp := auth.Fingerprint(key)
	sum := sha256.Sum256(key.Marshal())
	want := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	if fp != want {
		t.Fatalf("fp = %q, want %q", fp, want)
	}

	if err := GenerateKey(priv, "postern:macbook"); err == nil {
		t.Fatal("expected error when key exists")
	}
}

func TestEnsureKeyIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	priv := filepath.Join(dir, "id_ed25519")
	if err := EnsureKey(priv, "postern:nuc"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureKey(priv, "postern:nuc"); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(priv)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("EnsureKey rewrote an existing key")
	}
}

func TestEnsureKeyRestoresMissingPub(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	priv := filepath.Join(dir, "id_ed25519")
	if err := GenerateKey(priv, "postern:pi"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(priv + ".pub"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureKey(priv, "postern:pi"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(priv + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	key, err := auth.ParseEnrollPubkey(bytesTrim(raw))
	if err != nil {
		t.Fatal(err)
	}
	if key.Type() != ssh.KeyAlgoED25519 {
		t.Fatalf("type = %q", key.Type())
	}
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
