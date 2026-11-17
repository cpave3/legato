//go:build !linux

package mcpstdio

import "syscall"

func sysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// proc is a process the server started. Only Linux finds them (through
// /proc); elsewhere a server's process group is all that is stopped.
type proc struct {
	pid   int
	start string
}

func (proc) same() bool { return false }

func descendants(int) []proc { return nil }
