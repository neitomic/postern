package alloc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
)

var (
	ErrNoListenPID = errors.New("could not resolve listen pid")
	ErrNotSSHD     = errors.New("listen pid is not an sshd reverse-forward child")
)

func sshdChildFrom(comm, cmdline string) bool {
	if strings.TrimSpace(comm) != "sshd" {
		return false
	}
	cmd := strings.ReplaceAll(cmdline, "\x00", " ")
	// OpenSSH session processes retitle to "sshd: user [priv]" / "sshd: user@notty".
	if strings.Contains(cmd, "sshd:") {
		return true
	}
	return false
}

func parseSocketInode(target string) (uint64, bool) {
	s, ok := strings.CutPrefix(target, "socket:[")
	if !ok || !strings.HasSuffix(s, "]") {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSuffix(s, "]"), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func listenPIDsFrom(procNet io.Reader, procDir string, min, max int) (map[int]int, error) {
	list, err := parseProcNetTCPList(procNet, min, max)
	if err != nil {
		return nil, err
	}
	inodeToPort := make(map[uint64]int, len(list))
	for _, s := range list {
		if s.Inode != 0 {
			inodeToPort[s.Inode] = s.Port
		}
	}
	return scanSocketPids(procDir, inodeToPort), nil
}

func scanSocketPids(procDir string, inodeToPort map[uint64]int) map[int]int {
	out := make(map[int]int)
	if len(inodeToPort) == 0 {
		return out
	}
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		fdDir := fmt.Sprintf("%s/%d/fd", procDir, pid)
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil {
				continue
			}
			inode, ok := parseSocketInode(target)
			if !ok {
				continue
			}
			port, ok := inodeToPort[inode]
			if !ok {
				continue
			}
			if _, exists := out[port]; !exists {
				out[port] = pid
			}
		}
	}
	return out
}

func killListenPIDs(pids []int, inspect func(pid int) (comm, cmdline string, err error), kill func(pid int) error) error {
	if len(pids) == 0 {
		return ErrNoListenPID
	}
	var last error
	killed := 0
	for _, pid := range pids {
		if pid <= 1 {
			last = ErrNotSSHD
			continue
		}
		comm, cmdline, err := inspect(pid)
		if err != nil {
			last = err
			continue
		}
		if !sshdChildFrom(comm, cmdline) {
			last = ErrNotSSHD
			continue
		}
		if err := kill(pid); err != nil {
			last = err
			continue
		}
		killed++
	}
	if killed == 0 {
		if last != nil {
			return last
		}
		return ErrNotSSHD
	}
	return nil
}

func procInspect(procDir string, pid int) (comm, cmdline string, err error) {
	b, err := os.ReadFile(fmt.Sprintf("%s/%d/comm", procDir, pid))
	if err != nil {
		return "", "", err
	}
	c, err := os.ReadFile(fmt.Sprintf("%s/%d/cmdline", procDir, pid))
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(string(b)), string(c), nil
}

func sysKill(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
