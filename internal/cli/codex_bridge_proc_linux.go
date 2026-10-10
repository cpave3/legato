package cli

import "syscall"

// childOfBridge ties the app server's life to the bridge that started it: a
// bridge that is killed leaves nobody to stop the server, so the kernel does.
func childOfBridge() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
