package store

import (
	"context"
	"testing"
)

func TestGetTaskNotifyEnabled_DefaultsToFalse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// No task preference has been set — default must be false.
	enabled, err := s.GetTaskNotifyEnabled(ctx, "TASK-NEW")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Fatalf("expected notify_enabled to default to false, got true")
	}
}

func TestUpdateTaskNotifyEnabled_Toggle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpdateTaskNotifyEnabled(ctx, "TASK-1", true); err != nil {
		t.Fatal(err)
	}
	enabled, err := s.GetTaskNotifyEnabled(ctx, "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatalf("expected true, got false")
	}

	if err := s.UpdateTaskNotifyEnabled(ctx, "TASK-1", false); err != nil {
		t.Fatal(err)
	}
	enabled, err = s.GetTaskNotifyEnabled(ctx, "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatalf("expected false, got true")
	}
}
