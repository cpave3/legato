//go:build !linux

package cli

import "syscall"

// childOfBridge has nothing to add where the kernel cannot signal a child on
// its parent's death; the bridge stops the server itself when it exits.
func childOfBridge() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
