package main

import (
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

// Whether any Legato on a data folder runs the review engine: the broker
// holder holds the engine's lock while it ticks, and the operating system
// lets it go when that process ends.
func TestTheReviewEngineLockSaysWhetherAnyProcessRunsTheEngine(t *testing.T) {
	dir := t.TempDir()
	if reviewEngineRunning(dir) {
		t.Fatal("running before any process holds the engine")
	}
	holder := &reviewEngineLock{dir: dir}
	if !holder.hold() {
		t.Fatal("the holder could not take the lock")
	}
	if !reviewEngineRunning(dir) || !reviewEngineRunning(dir) {
		t.Fatal("not running while the holder holds it")
	}
	if !holder.hold() {
		t.Fatal("holding again failed")
	}
	other := &reviewEngineLock{dir: dir}
	if other.hold() {
		t.Fatal("a second process took the engine's lock")
	}
	holder.release()
	if reviewEngineRunning(dir) {
		t.Fatal("running after the holder let go")
	}
	if !other.hold() {
		t.Fatal("the next holder could not take the lock")
	}
	other.release()
}

// What the card menus are told of the engine: offered while any process on
// the data folder holds it, else why not.
func TestTheReviewEngineStatusSaysWhyNoReviewIsOffered(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	status := reviewEngineStatus(cfg)
	if got := status(); got.Running || got.Reason != service.ReviewPassesOffReason {
		t.Fatalf("off = %+v", got)
	}
	cfg.ReviewPasses.Enabled = true
	if got := status(); got.Running || got.Reason != service.ReviewEngineStoppedReason {
		t.Fatalf("no holder = %+v", got)
	}
	holder := &reviewEngineLock{dir: config.ResolveDataDir(cfg)}
	if !holder.hold() {
		t.Fatal("the holder could not take the lock")
	}
	defer holder.release()
	if got := status(); !got.Running || got.Reason != "" {
		t.Fatalf("held = %+v", got)
	}
}
