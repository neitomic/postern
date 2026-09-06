//go:build linux

package auth

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func FromConn(c net.Conn) (Peer, error) {
	sc, ok := c.(syscall.Conn)
	if !ok {
		return Peer{}, errors.New("not a syscall conn")
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return Peer{}, err
	}
	var p Peer
	var sysErr error
	if err := raw.Control(func(fd uintptr) {
		p, sysErr = fromFD(int(fd))
	}); err != nil {
		return Peer{}, err
	}
	return p, sysErr
}

func fromFD(fd int) (Peer, error) {
	ucred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return Peer{}, err
	}
	p := Peer{
		PID: int(ucred.Pid),
		UID: ucred.Uid,
		GID: ucred.Gid,
	}
	gids, err := peerGroups(fd)
	if err != nil {
		gids, err = groupsFromProc(p.PID)
		if err != nil {
			return Peer{}, err
		}
	}
	p.Groups = gids
	return p, nil
}

func peerGroups(fd int) ([]uint32, error) {
	n := 64
	for {
		buf := make([]uint32, n)
		vallen := uint32(n * 4)
		_, _, errno := unix.Syscall6(
			unix.SYS_GETSOCKOPT,
			uintptr(fd),
			uintptr(unix.SOL_SOCKET),
			uintptr(unix.SO_PEERGROUPS),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&vallen)),
			0,
		)
		if errno == unix.ERANGE {
			need := int(vallen) / 4
			if need <= n {
				need = n * 2
			}
			if need > 65536 {
				return nil, fmt.Errorf("peergroups too large")
			}
			n = need
			continue
		}
		if errno != 0 {
			return nil, errno
		}
		count := int(vallen) / 4
		if count < 0 {
			count = 0
		}
		if count > len(buf) {
			count = len(buf)
		}
		out := make([]uint32, count)
		copy(out, buf[:count])
		return out, nil
	}
}

func groupsFromProc(pid int) ([]uint32, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseStatusGroups(f)
}
