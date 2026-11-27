package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Each process-wide role has its own lock in the data directory: holding
// one never keeps another role from its process, and a released lock
// leaves its file in place (removing it could let two processes lock
// different files at one path).
func TestRuntimeRolesLockTheirOwnFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	claims := map[string]func(string) (func(), error){
		"usage.lock":    claimUsagePoller,
		"chatbots.lock": claimChatBots,
		"mqtt.lock":     claimMQTTPublisher,
	}
	var releases []func()
	for name, claim := range claims {
		release, err := claim(dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		releases = append(releases, release)
		if _, err := claim(dir); err == nil {
			t.Fatalf("%s claimed twice", name)
		}
	}
	for _, release := range releases {
		release()
		release()
	}
	for name, claim := range claims {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		release, err := claim(dir)
		if err != nil {
			t.Fatalf("%s after release: %v", name, err)
		}
		release()
	}
}
