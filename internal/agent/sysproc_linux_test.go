//go:build linux

package agent

import (
	"syscall"
	"testing"
)

func TestAutosshPdeathsig(t *testing.T) {
	t.Parallel()
	attr := autosshSysProcAttr()
	if !attr.Setpgid {
		t.Fatal("Setpgid")
	}
	if attr.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("Pdeathsig = %v, want SIGTERM", attr.Pdeathsig)
	}
}
