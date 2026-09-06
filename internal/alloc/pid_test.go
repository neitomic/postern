package alloc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHDChildFrom(t *testing.T) {
	t.Parallel()
	cases := []struct {
		comm, cmd string
		want      bool
	}{
		{"sshd", "sshd: postern [priv]", true},
		{"sshd", "sshd: postern@notty", true},
		{"sshd", "/usr/sbin/sshd\x00-D\x00", false},
		{"sshd", "sshd -D", false},
		{"systemd", "sshd: postern [priv]", false},
		{"sshd", "", false},
	}
	for _, tc := range cases {
		if got := sshdChildFrom(tc.comm, tc.cmd); got != tc.want {
			t.Errorf("sshdChildFrom(%q, %q) = %v, want %v", tc.comm, tc.cmd, got, tc.want)
		}
	}
}

func TestParseSocketInode(t *testing.T) {
	t.Parallel()
	n, ok := parseSocketInode("socket:[12345]")
	if !ok || n != 12345 {
		t.Fatalf("got %d %v", n, ok)
	}
	if _, ok := parseSocketInode("/dev/null"); ok {
		t.Fatal("non-socket")
	}
}

func TestParseProcNetTCPListInode(t *testing.T) {
	t.Parallel()
	list, err := parseProcNetTCPList(strings.NewReader(sampleProcNetTCP), 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	byPort := map[int]uint64{}
	for _, s := range list {
		byPort[s.Port] = s.Inode
	}
	if byPort[2200] != 1 {
		t.Fatalf("inode 2200 = %d", byPort[2200])
	}
}

func TestListenPIDsFromFakeProc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pidDir := filepath.Join(dir, "4242", "fd")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[1]", filepath.Join(pidDir, "4")); err != nil {
		t.Fatal(err)
	}
	got, err := listenPIDsFrom(strings.NewReader(sampleProcNetTCP), dir, 2200, 2299)
	if err != nil {
		t.Fatal(err)
	}
	if got[2200] != 4242 {
		t.Fatalf("got %v", got)
	}
}

func TestKillListenPIDsOnlySSHDChild(t *testing.T) {
	t.Parallel()
	var killed []int
	err := killListenPIDs([]int{1, 9, 11}, func(pid int) (string, string, error) {
		switch pid {
		case 9:
			return "sshd", "/usr/sbin/sshd -D", nil
		case 11:
			return "sshd", "sshd: postern [priv]", nil
		default:
			return "init", "", nil
		}
	}, func(pid int) error {
		killed = append(killed, pid)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(killed) != 1 || killed[0] != 11 {
		t.Fatalf("killed %v", killed)
	}

	err = killListenPIDs([]int{9}, func(pid int) (string, string, error) {
		return "sshd", "sshd -D", nil
	}, func(pid int) error {
		t.Fatal("must not kill listener")
		return nil
	})
	if err != ErrNotSSHD {
		t.Fatalf("err = %v", err)
	}
}
