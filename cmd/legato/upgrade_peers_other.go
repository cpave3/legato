//go:build !linux

package main

// usesFile cannot tell, without /proc, which database a process has open:
// every process listening on a Legato socket counts.
func usesFile(int, string) bool { return true }
