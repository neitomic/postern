package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

const (
	goldenPub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBktPaVv6ld6eJT8/J9fyUUFiPzEBlI+8HfbMeGl1j/6"
	goldenFP  = "SHA256:xntGiH1ffcYHTKyHuq1uZHIszeGYiFhWDZd0LkSwLjo"
)

func testdata(t *testing.T, elem ...string) string {
	t.Helper()
	parts := append([]string{"..", "..", "testdata"}, elem...)
	return filepath.Join(parts...)
}

func TestParseEnrollPubkeyOK(t *testing.T) {
	t.Parallel()
	key, err := ParseEnrollPubkey([]byte("  " + goldenPub + " postern:macbook  "))
	if err != nil {
		t.Fatal(err)
	}
	if key.Type() != ssh.KeyAlgoED25519 {
		t.Fatalf("type = %q", key.Type())
	}
	got := MarshalStoredKey(key)
	if got != goldenPub {
		t.Fatalf("stored = %q, want %q", got, goldenPub)
	}
	if strings.Contains(got, "postern:") {
		t.Fatal("comment leaked into stored key")
	}
	if strings.ContainsAny(got, "\n\r") {
		t.Fatal("stored key contains newline")
	}
}

func TestFingerprintOpenSSH(t *testing.T) {
	t.Parallel()
	key, err := ParseEnrollPubkey([]byte(goldenPub))
	if err != nil {
		t.Fatal(err)
	}
	got := Fingerprint(key)
	if got != goldenFP {
		t.Fatalf("fp = %q, want %q", got, goldenFP)
	}
	sum := sha256.Sum256(key.Marshal())
	want := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("fp = %q, independent = %q", got, want)
	}
	if strings.Contains(got, "=") {
		t.Fatal("fingerprint is padded")
	}
}

func TestParseEnrollPubkeyRejectsNULAndCRLF(t *testing.T) {
	t.Parallel()
	cases := [][]byte{
		[]byte(goldenPub + "\n"),
		[]byte(goldenPub + "\r"),
		[]byte(goldenPub + "\r\n"),
		append([]byte(goldenPub[:10]), append([]byte{0}, goldenPub[10:]...)...),
	}
	for _, raw := range cases {
		if _, err := ParseEnrollPubkey(raw); !errors.Is(err, ErrInvalidPubkey) {
			t.Errorf("raw %q: err = %v, want %v", raw, err, ErrInvalidPubkey)
		}
	}
}

func TestParseEnrollPubkeyRejectsOptionsAndTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want error
	}{
		{`command="id" ` + goldenPub, ErrPubkeyOptions},
		{`restrict,port-forwarding ` + goldenPub, ErrPubkeyOptions},
		{`sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29tAAAAIHNrLWVkMjU1MTktdGVzdC1rZXktMzItYnl0ZXMhISEhAAAABHNzaDo=`, ErrNotEd25519},
		{`ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAgQCWiNCSb1oNrwsT6QCX1ZrLRmWMgeUCmcX/2gMsqNN2U6ejoem4fVvVBGsNry2jWXbgo+RFd8tpqd/BV4bZCg9d4uazLAV3AvrsdW998Fcg+Twp6o8fh43rPGaeTPVgOTGdAhumLJdedgQa5UyJ3LDOq9MN0FKfWuuoCUy7W8BTdQ==`, ErrNotEd25519},
		{"not-a-key", ErrInvalidPubkey},
	}
	for _, tc := range cases {
		_, err := ParseEnrollPubkey([]byte(tc.raw))
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.raw[:min(40, len(tc.raw))], err, tc.want)
		}
	}
}

func TestParseEnrollPubkeyMaliciousDir(t *testing.T) {
	t.Parallel()
	dir := testdata(t, "pubkeys", "malicious")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) == 0 {
		t.Fatal("malicious pubkey testdata is empty")
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseEnrollPubkey(raw); err == nil {
			t.Errorf("%s: accepted, want reject", e.Name())
		}
	}
}

func TestParseEnrollPubkeyGeneratedEd25519(t *testing.T) {
	t.Parallel()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	line := MarshalStoredKey(key) + " comment"
	got, err := ParseEnrollPubkey([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Marshal(), key.Marshal()) {
		t.Fatal("round-trip mismatch")
	}
}
