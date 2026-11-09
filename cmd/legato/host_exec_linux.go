package main

import "golang.org/x/sys/unix"

// setNoNewPrivs stops this process and what it runs from gaining
// privileges: setuid binaries such as sudo do not elevate.
func setNoNewPrivs() error { return unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) }
