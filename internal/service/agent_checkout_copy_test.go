package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// copyTree stands in for clonefile on a host without APFS.
func copyTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return exec.Command("cp", "-a", src, dst).Run()
}

// A backend without overlays (Seatbelt) gives a reviewer a copy-on-write
// copy of the checkout in its scratch folder: it builds and tests there,
// the author's checkout stays read-only, and the copy goes with the
// execution.
func TestWithoutOverlaysAThrowawayCheckoutIsACopyInTheScratchFolder(t *testing.T) {
	backend := &fakeBackend{}
	var copied [][2]string
	svc, s, _ := sandboxedService(t, backend, func(settings *SandboxSettings) {
		settings.CopyCheckout = func(src, dst string) error {
			copied = append(copied, [2]string{src, dst})
			return copyTree(src, dst)
		}
	})
	createTask(t, s, "task-1")
	work := realTempDir(t)
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !svc.(CheckoutOverlayProber).CheckoutOverlayAvailable() {
		t.Fatal("a throwaway checkout is said to be unavailable")
	}
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkingDir: work, ReadOnlyCheckout: true, ThrowawayCheckout: true}); err != nil {
		t.Fatal(err)
	}
	if len(copied) != 1 || copied[0][0] != work {
		t.Fatalf("copied %q", copied)
	}
	plan := backend.plans[0]
	copy := copied[0][1]
	if plan.Chdir != copy {
		t.Fatalf("the agent works in %s, want the copy %s", plan.Chdir, copy)
	}
	if m, ok := mountOf(plan, copy); !ok || m.Kind != sandbox.ReadWrite {
		t.Fatalf("the copy %+v %v", m, ok)
	}
	if m, ok := mountOf(plan, work); !ok || m.Kind != sandbox.ReadOnly {
		t.Fatalf("the author's checkout %+v %v", m, ok)
	}
	if _, err := os.Stat(filepath.Join(copy, "main.go")); err != nil {
		t.Fatalf("the copy holds nothing: %v", err)
	}
	session, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	if readOnly, _ := s.ExecutionReadOnly(context.Background(), session.ExecutionID); !readOnly {
		t.Error("the execution is no longer read-only to the credential broker")
	}
}

// Where the copy cannot be made (another volume, not APFS), the reviewer
// gets the read-only checkout, as without overlays before.
func TestACheckoutThatCannotBeCopiedIsReadOnly(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend, func(settings *SandboxSettings) {
		settings.CopyCheckout = func(string, string) error { return errors.New("not APFS") }
	})
	createTask(t, s, "task-1")
	work := realTempDir(t)
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkingDir: work, ReadOnlyCheckout: true, ThrowawayCheckout: true}); err != nil {
		t.Fatal(err)
	}
	plan := backend.plans[0]
	if m, ok := mountOf(plan, work); !ok || m.Kind != sandbox.ReadOnly || plan.Chdir != work {
		t.Fatalf("checkout %+v %v, chdir %s", m, ok, plan.Chdir)
	}
}
