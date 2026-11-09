//go:build !linux

package main

// setNoNewPrivs has no counterpart outside Linux: host commands run as
// Legato's user, with nothing more.
func setNoNewPrivs() error { return nil }
