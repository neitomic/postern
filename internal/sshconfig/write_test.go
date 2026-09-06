package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceManagedAppendsWhenAbsent(t *testing.T) {
	t.Parallel()
	block := Render(goldenJump(), goldenHosts(), "0.1.0", goldenAt)
	got, err := ReplaceManaged("Host github.com\n    User git\n", block)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "Host github.com\n    User git\n\n# BEGIN POSTERN MANAGED BLOCK\n") {
		t.Fatalf("got:\n%s", got)
	}
	if !strings.HasSuffix(got, "# END POSTERN MANAGED BLOCK\n") {
		t.Fatalf("missing end marker:\n%s", got)
	}
	if strings.Count(got, BeginMarker) != 1 || strings.Count(got, EndMarker) != 1 {
		t.Fatalf("marker count:\n%s", got)
	}
}

func TestReplaceManagedReplacesMarkersOnly(t *testing.T) {
	t.Parallel()
	existing := "Host keep-before\n\n# BEGIN POSTERN MANAGED BLOCK\nold junk\n# END POSTERN MANAGED BLOCK\n\nHost keep-after\n    HostName x\n"
	block := Render(goldenJump(), goldenHosts(), "0.1.0", goldenAt)
	got, err := ReplaceManaged(existing, block)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Host keep-before") || !strings.Contains(got, "Host keep-after") {
		t.Fatalf("lost surrounding config:\n%s", got)
	}
	if strings.Contains(got, "old junk") {
		t.Fatalf("old block remained:\n%s", got)
	}
	if strings.Count(got, BeginMarker) != 1 || strings.Count(got, EndMarker) != 1 {
		t.Fatalf("marker count:\n%s", got)
	}
	if !strings.Contains(got, "Host macbook") || !strings.Contains(got, "Host nuc") {
		t.Fatalf("new stanza missing:\n%s", got)
	}
}

func TestReplaceManagedUnbalanced(t *testing.T) {
	t.Parallel()
	if _, err := ReplaceManaged("# BEGIN POSTERN MANAGED BLOCK\nno end\n", "x"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ReplaceManaged("no begin\n# END POSTERN MANAGED BLOCK\n", "x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteFileCreates0600AndReplacesMarkers(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config")
	block := Render(goldenJump(), goldenHosts(), "0.1.0", goldenAt)
	if err := WriteFile(path, block); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("created mode = %o, want 0600", perm)
	}

	prefix := "Host github.com\n    User git\n\n"
	if err := os.WriteFile(path, []byte(prefix+"# BEGIN POSTERN MANAGED BLOCK\nold\n# END POSTERN MANAGED BLOCK\n\nHost leftover\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, block); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !strings.HasPrefix(body, "Host github.com\n    User git\n") {
		t.Fatalf("prefix lost:\n%s", body)
	}
	if !strings.Contains(body, "Host leftover") {
		t.Fatalf("suffix lost:\n%s", body)
	}
	if strings.Contains(body, "old\n") || strings.Count(body, BeginMarker) != 1 {
		t.Fatalf("markers not replaced:\n%s", body)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("existing mode = %o, want 0644", perm)
	}
}

func TestWriteFileRefusesNonRegular(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteFile(dir, "x"); err == nil {
		t.Fatal("expected error for directory")
	}
}
