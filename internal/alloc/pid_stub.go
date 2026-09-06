//go:build !linux

package alloc

func ListenPIDs(min, max int) (map[int]int, error) {
	return map[int]int{}, nil
}

func KillListenPort(port int) error {
	return errListenLinuxOnly
}
