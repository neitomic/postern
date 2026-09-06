//go:build !linux

package agent

import "syscall"

func autosshSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
