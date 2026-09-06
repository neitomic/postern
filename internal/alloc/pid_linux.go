//go:build linux

package alloc

import (
	"os"
)

func ListenPIDs(min, max int) (map[int]int, error) {
	f, err := os.Open("/proc/net/tcp")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return listenPIDsFrom(f, "/proc", min, max)
}

func KillListenPort(port int) error {
	pids, err := ListenPIDs(port, port)
	if err != nil {
		return err
	}
	pid, ok := pids[port]
	if !ok {
		return ErrNoListenPID
	}
	return killListenPIDs([]int{pid}, func(pid int) (string, string, error) {
		return procInspect("/proc", pid)
	}, sysKill)
}
