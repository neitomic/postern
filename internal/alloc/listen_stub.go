//go:build !linux

package alloc

import "errors"

var errListenLinuxOnly = errors.New("listen probe is Linux-only")

type stubProbe struct{}

func ProcProbe() ListenProbe {
	return stubProbe{}
}

func (stubProbe) Listening(min, max int) (map[int]struct{}, error) {
	return nil, errListenLinuxOnly
}
