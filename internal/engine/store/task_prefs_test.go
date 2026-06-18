package store

import (
	"context"
	"testing"
)

func TestTaskNotifyEnabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	enabled, err := s.GetTaskNotifyEnabled(ctx, "TASK-1")
	if err != nil {
		t.Fatalf("get before set: %v", err)
	}
	if enabled {
		t.Error("expected disabled by default")
	}

	if err := s.UpdateTaskNotifyEnabled(ctx, "TASK-1", true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	enabled, err = s.GetTaskNotifyEnabled(ctx, "TASK-1")
	if err != nil {
		t.Fatalf("get after enable: %v", err)
	}
	if !enabled {
		t.Error("expected enabled")
	}

	if err := s.UpdateTaskNotifyEnabled(ctx, "TASK-1", false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	enabled, err = s.GetTaskNotifyEnabled(ctx, "TASK-1")
	if err != nil {
		t.Fatalf("get after disable: %v", err)
	}
	if enabled {
		t.Error("expected disabled")
	}
}
