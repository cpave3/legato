package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnExecutionRecordsItsEngram(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	if err := s.CreateExecution(ctx, executionAt("ex-a", "task-a", start)); err != nil {
		t.Fatal(err)
	}
	name, session, err := s.ExecutionEngram(ctx, "ex-a")
	if err != nil || name != "" || session != "" {
		t.Fatalf("a new execution's engram = %q %q %v, want none", name, session, err)
	}
	if err := s.SetExecutionEngram(ctx, "ex-a", "mneme", "ex-a"); err != nil {
		t.Fatal(err)
	}
	name, session, err = s.ExecutionEngram(ctx, "ex-a")
	if err != nil || name != "mneme" || session != "ex-a" {
		t.Fatalf("engram = %q %q %v", name, session, err)
	}
	if err := s.SetExecutionEngram(ctx, "ex-missing", "mneme", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing execution: %v", err)
	}
}

func TestATasksLastEngramIsItsLatestExecutionThatRanAsOne(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	for i, id := range []string{"ex-1", "ex-2", "ex-3"} {
		if err := s.CreateExecution(ctx, executionAt(id, "task-a", start.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	if last, err := s.LastTaskEngram(ctx, "task-a"); err != nil || last.Engram != "" {
		t.Fatalf("no engram yet: %+v %v", last, err)
	}
	_ = s.SetExecutionEngram(ctx, "ex-1", "argus", "ex-1")
	_ = s.SetExecutionEngram(ctx, "ex-2", "mneme", "ex-2")
	// ex-3 ran with no engram: the last engram is still mneme.
	last, err := s.LastTaskEngram(ctx, "task-a")
	if err != nil || last.Engram != "mneme" || last.Session != "ex-2" || last.ExecutionID != "ex-2" {
		t.Fatalf("last = %+v %v", last, err)
	}
	session, err := s.LastEngramSession(ctx, "task-a", "argus")
	if err != nil || session != "ex-1" {
		t.Fatalf("argus's last session = %q %v", session, err)
	}
	if session, _ := s.LastEngramSession(ctx, "task-a", "hestia"); session != "" {
		t.Fatalf("hestia's last session = %q, want none", session)
	}
}

func TestEngramBindings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	place := "repo:github.com/cpave3/legato"
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

	if err := s.BindEngram(ctx, place, "mneme", true, at); err != nil {
		t.Fatal(err)
	}
	// Binding another as the default keeps the old one bound.
	if err := s.BindEngram(ctx, place, "argus", true, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := s.EngramBindings(ctx, []string{place})
	if err != nil || len(got) != 2 {
		t.Fatalf("bindings = %+v %v", got, err)
	}
	defaults := 0
	for _, b := range got {
		if b.IsDefault {
			defaults++
			if b.Engram != "argus" {
				t.Fatalf("default = %s, want argus", b.Engram)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("%d defaults", defaults)
	}
	// Binding one already bound without the default changes nothing.
	if err := s.BindEngram(ctx, place, "mneme", false, at); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEngramDefault(ctx, place, "mneme"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEngramDefault(ctx, place, "hestia"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("default of an unbound engram: %v", err)
	}
	if err := s.TouchEngramBinding(ctx, place, "argus", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.EngramBindings(ctx, []string{place, "dir:/elsewhere"})
	for _, b := range got {
		switch b.Engram {
		case "mneme":
			if !b.IsDefault {
				t.Fatal("mneme is not the default")
			}
		case "argus":
			if b.IsDefault || b.LastUsedAt == nil || !b.LastUsedAt.Equal(at.Add(time.Hour)) {
				t.Fatalf("argus = %+v", b)
			}
		}
	}
	if err := s.ClearEngramDefault(ctx, place); err != nil {
		t.Fatal(err)
	}
	got, _ = s.AllEngramBindings(ctx)
	for _, b := range got {
		if b.IsDefault {
			t.Fatalf("%s is still the default", b.Engram)
		}
	}
	if len(got) != 2 {
		t.Fatalf("clearing the default unbound engrams: %+v", got)
	}
	if err := s.UnbindEngram(ctx, place, "argus"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnbindEngram(ctx, place, "argus"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unbinding twice: %v", err)
	}
	got, _ = s.AllEngramBindings(ctx)
	if len(got) != 1 || got[0].Engram != "mneme" || got[0].PlaceKey != place {
		t.Fatalf("after unbind = %+v", got)
	}
}

func TestAFailedEngramBootIsListedWhileItsAgentRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	for _, id := range []string{"ex-a", "ex-b"} {
		if err := s.CreateExecution(ctx, executionAt(id, "task-"+id, start)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetExecutionEngramFailure(ctx, "ex-a", "mneme", "disk is full"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListEngramBootFailures(ctx)
	if err != nil || len(got) != 1 || got[0].ExecutionID != "ex-a" || got[0].TaskID != "task-ex-a" || got[0].Engram != "mneme" || got[0].Reason != "disk is full" || got[0].TaskTitle != "Task task-ex-a" {
		t.Fatalf("failures = %+v %v", got, err)
	}
	if err := s.TerminalizeExecutionAt(ctx, "ex-a", ExecutionSucceeded, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListEngramBootFailures(ctx); len(got) != 0 {
		t.Fatalf("an ended agent's failure is still listed: %+v", got)
	}
}
