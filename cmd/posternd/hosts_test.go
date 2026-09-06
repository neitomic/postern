package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/neitomic/postern/internal/alloc"
)

func TestApplyRmListenKilledDoesNotRequireRoot(t *testing.T) {
	t.Parallel()
	got := rmResponse{OK: true, Name: "macbook", Port: 2223, Listen: true, Killed: true}
	var stderr bytes.Buffer
	err := applyRmListen(got, true, 1000, func(int) error {
		t.Fatal("must not retry kill when daemon already killed")
		return nil
	}, &stderr)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stderr.String(), "still LISTEN") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestApplyRmListenExit2WhenKillNotDelivered(t *testing.T) {
	t.Parallel()
	got := rmResponse{OK: true, Name: "macbook", Port: 2223, Listen: true, Killed: false}
	var stderr bytes.Buffer
	err := applyRmListen(got, true, 1000, func(int) error {
		t.Fatal("non-root must not call killPort")
		return nil
	}, &stderr)
	var ec exitCodeError
	if !errors.As(err, &ec) || ec.code != 2 {
		t.Fatalf("err = %v, want exit 2", err)
	}
	if !strings.Contains(err.Error(), "requires root") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyRmListenRootNoPIDIsSuccess(t *testing.T) {
	t.Parallel()
	got := rmResponse{OK: true, Name: "macbook", Port: 2223, Listen: true, Killed: false}
	err := applyRmListen(got, true, 0, func(int) error { return alloc.ErrNoListenPID }, io.Discard)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyRmListenNoKillWarnsOnly(t *testing.T) {
	t.Parallel()
	got := rmResponse{OK: true, Name: "macbook", Port: 2223, Listen: true}
	var stderr bytes.Buffer
	err := applyRmListen(got, false, 1000, nil, &stderr)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stderr.String(), "still LISTEN") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestApplyRmListenClearIsSilent(t *testing.T) {
	t.Parallel()
	got := rmResponse{OK: true, Name: "macbook", Port: 2223, Listen: false, Killed: true}
	var stderr bytes.Buffer
	err := applyRmListen(got, true, 1000, func(int) error {
		t.Fatal("must not kill")
		return nil
	}, &stderr)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
