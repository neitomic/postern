package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultBinDir(t *testing.T) {
	t.Parallel()
	got := DefaultBinDir("/Users/neo")
	if got != "/Users/neo/.local/bin" {
		t.Fatalf("DefaultBinDir = %q", got)
	}
	if InstalledBinaryPath("/Users/neo") != "/Users/neo/.local/bin/postern" {
		t.Fatalf("InstalledBinaryPath = %q", InstalledBinaryPath("/Users/neo"))
	}
}

func TestInstallBinaryCopiesAndMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "postern")
	dst := filepath.Join(dir, "bin", "postern")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("#!/bin/sh\necho postern-fixture\n")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallBinary(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("copied = %q", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("mode = %o, want 0755", perm)
	}
}

func TestInstallBinarySameFileNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "postern")
	if err := os.WriteFile(src, []byte("abc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallBinary(src, src); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abc" {
		t.Fatalf("rewrote same file: %q", got)
	}
}

func TestInstallBinaryReplacesExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "new")
	dst := filepath.Join(dir, "postern")
	if err := os.WriteFile(src, []byte("v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallBinary(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("got %q, want v2", got)
	}
}

func TestDirOnPATH(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin")
	if !DirOnPATH(dir) {
		t.Fatal("expected dir on PATH")
	}
	if DirOnPATH(filepath.Join(dir, "nope")) {
		t.Fatal("unexpected PATH hit")
	}
	if PATHHint(dir) != "" {
		t.Fatal("PATHHint should be empty when on PATH")
	}
	off := filepath.Join(dir, "missing")
	hint := PATHHint(off)
	if !strings.Contains(hint, off) || !strings.Contains(hint, "PATH") {
		t.Fatalf("hint = %q", hint)
	}
}

func TestNextSteps(t *testing.T) {
	t.Parallel()
	enrolled := NextSteps(true)
	if !strings.Contains(enrolled, "autostart") {
		t.Fatalf("enrolled = %q", enrolled)
	}
	if strings.Contains(enrolled, "join --token") {
		t.Fatal("enrolled next-steps should not tell join")
	}
	fresh := NextSteps(false)
	for _, want := range []string{"config set server", "config set name", "join --token", "waits until this machine is enrolled"} {
		if !strings.Contains(fresh, want) {
			t.Fatalf("fresh missing %q:\n%s", want, fresh)
		}
	}
}
