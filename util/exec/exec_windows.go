//go:build windows
// +build windows

package exec

import "syscall"

func newSysProcAttr(_ bool) *syscall.SysProcAttr { return &syscall.SysProcAttr{} }

func sysCallSignal(_ int, _ syscall.Signal) error { return nil }
