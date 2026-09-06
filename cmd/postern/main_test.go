package main

import (
	"bytes"
	"strings"
	"testing"
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
