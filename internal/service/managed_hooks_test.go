package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func writeHookManifest(t *testing.T, dir, name, manifest string) {
	t.Helper()
	folder := filepath.Join(dir, name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "HOOK.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAManagedHookFoundOnDiskIsOnUntilTurnedOff(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "hooks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dir := filepath.Join(t.TempDir(), "managed-hooks")
	svc := NewManagedHookService(dir, db)
	ctx := context.Background()

	if list, err := svc.List(ctx); err != nil || len(list) != 0 || svc.Dir() != dir {
		t.Fatalf("no folder: %v, %v", list, err)
	}
	writeHookManifest(t, dir, "codeeval", "name: codeeval\ndescription: Gate.\nagent_tools: [claude-code]\nevents:\n  stop: {command: ./stop.sh}\n  session_start: {command: ./start.sh}\n")
	writeHookManifest(t, dir, "broken", "name: other\ndescription: Typo.\nevents:\n  stop: {command: ./a}\n")

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "broken" || list[1].Name != "codeeval" {
		t.Fatalf("hooks = %+v", list)
	}
	gate := list[1]
	if !gate.Enabled || gate.Err != "" || gate.Description != "Gate." || gate.Path != filepath.Join(dir, "codeeval") ||
		!reflect.DeepEqual(gate.Events, []string{"session_start", "stop"}) || !reflect.DeepEqual(gate.AgentTools, []string{"claude-code"}) {
		t.Fatalf("codeeval = %+v", gate)
	}
	if list[0].Err == "" || list[0].Events == nil || list[0].AgentTools == nil {
		t.Fatalf("broken = %+v, want its error and empty lists", list[0])
	}

	if err := svc.SetEnabled(ctx, "codeeval", false); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.List(ctx); list[1].Enabled {
		t.Fatalf("codeeval still on: %+v", list[1])
	}
	for _, name := range []string{"broken", "absent"} {
		if err := svc.SetEnabled(ctx, name, false); !errors.Is(err, ErrManagedHookNotFound) {
			t.Fatalf("SetEnabled(%s) = %v, want ErrManagedHookNotFound", name, err)
		}
	}
}
