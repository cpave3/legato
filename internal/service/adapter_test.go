package service_test

import (
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeAdapter struct {
	name string
}

func (f *fakeAdapter) Name() string                                    { return f.name }
func (f *fakeAdapter) InstallHooks(projectDir string) error            { return nil }
func (f *fakeAdapter) UninstallHooks(projectDir string) error          { return nil }
func (f *fakeAdapter) EnvVars(taskID, socketPath string) map[string]string {
	return map[string]string{"TASK_ID": taskID}
}

func TestAdapterRegistry_RegisterAndGet(t *testing.T) {
	reg := service.NewAdapterRegistry()
	adapter := &fakeAdapter{name: "test-tool"}

	reg.Register(adapter)

	got, err := reg.Get("test-tool")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name() != "test-tool" {
		t.Errorf("got name %q, want %q", got.Name(), "test-tool")
	}
}

func TestAdapterRegistry_GetUnregistered(t *testing.T) {
	reg := service.NewAdapterRegistry()

	_, err := reg.Get("nonexistent")

	if err == nil {
		t.Fatal("expected error for unregistered adapter")
	}
}

func TestAdapterRegistry_List(t *testing.T) {
	reg := service.NewAdapterRegistry()
	reg.Register(&fakeAdapter{name: "alpha"})
	reg.Register(&fakeAdapter{name: "beta"})

	names := reg.List()

	if len(names) != 2 {
		t.Fatalf("List() returned %d names, want 2", len(names))
	}
	// Just check both are present (order doesn't matter).
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["alpha"] || !found["beta"] {
		t.Errorf("List() = %v, want [alpha beta]", names)
	}
}
