//go:build linux

package alloc

import (
	"os"
)

type procProbe struct{}

func ProcProbe() ListenProbe {
	return procProbe{}
}

func (procProbe) Listening(min, max int) (map[int]struct{}, error) {
	f, err := os.Open("/proc/net/tcp")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseProcNetTCP(f, min, max)
}
