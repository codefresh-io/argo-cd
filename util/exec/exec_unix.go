//go:build !windows
// +build !windows

package exec

import "syscall"

func newSysProcAttr(setpgid bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: setpgid}
}
func sysCallSignal(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}
