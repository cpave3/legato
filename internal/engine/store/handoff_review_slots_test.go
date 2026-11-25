package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// The engine's pass start takes its reviewer slot from the in-flight
// passes of open loops in the transaction that writes the pass and its
// loop: concurrent starts never exceed max_concurrent, a refused start
// writes nothing, and a pass that ends frees its slot.
func TestConcurrentEnginePassStartsNeverExceedMaxConcurrent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const loops, max = 8, 3
	tasks := make([]string, loops)
	for i := range tasks {
		tasks[i] = fmt.Sprintf("card-%d", i)
		if err := s.CreateTask(ctx, Task{ID: tasks[i], Title: tasks[i], Status: "Doing", CreatedAt: "2024-01-01", UpdatedAt: "2024-01-01"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveHandoffReview(ctx, HandoffReview{TaskID: tasks[i], Settings: "{}", Phase: "ready", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	start := func(task string, number int) error {
		loop := HandoffReview{TaskID: task, Settings: "{}", Phase: "reviewing", Pass: 1, UpdatedAt: "2024-01-02T00:00:00Z"}
		pass := HandoffReviewPass{TaskID: task, Pass: 1, State: "reviewing", StartedAt: "2024-01-02T00:00:00Z"}
		pinned := []HandoffReviewPR{{TaskID: task, Pass: 1, Repo: "acme/widget", Number: number, Branch: "b", HeadSHA: "aaa"}}
		return s.StartHandoffReviewPass(ctx, pass, pinned, loop, max)
	}

	var wg sync.WaitGroup
	errs := make([]error, loops)
	for i, task := range tasks {
		wg.Add(1)
		go func(i int, task string) {
			defer wg.Done()
			errs[i] = start(task, 100+i)
		}(i, task)
	}
	wg.Wait()
	var took, refused []string
	for i, err := range errs {
		switch {
		case err == nil:
			took = append(took, tasks[i])
		case errors.Is(err, ErrReviewSlotsFull):
			refused = append(refused, tasks[i])
		default:
			t.Fatalf("starting %s: %v", tasks[i], err)
		}
	}
	if len(took) != max {
		t.Fatalf("%d passes started, want %d", len(took), max)
	}
	// A refused start leaves its loop as it was, with no pass and no claim.
	for _, task := range refused {
		got, err := s.GetHandoffReview(ctx, task)
		if err != nil || got.Phase != "ready" || got.Pass != 0 {
			t.Fatalf("refused loop %s = %+v, %v", task, got, err)
		}
		if _, err := s.GetHandoffReviewPass(ctx, task, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("refused loop %s has a pass: %v", task, err)
		}
	}
	if err := start(refused[0], 200); !errors.Is(err, ErrReviewSlotsFull) {
		t.Fatalf("a start with every slot taken: %v", err)
	}

	// A submitted pass still holds its slot; one ended (as Recall ends it)
	// frees it for the next start.
	if err := s.SubmitHandoffReviewPass(ctx, HandoffReviewPass{TaskID: took[0], Pass: 1, State: "submitted", Verdict: "clean"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := start(refused[0], 200); !errors.Is(err, ErrReviewSlotsFull) {
		t.Fatalf("a start while a submitted pass holds the slot: %v", err)
	}
	if _, err := s.EndHandoffReview(ctx, took[1], "recalled", "the task was recalled", "2024-01-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := start(refused[0], 200); err != nil {
		t.Fatalf("a start after a pass ended: %v", err)
	}
	if n, err := s.ReviewPassesInFlight(ctx); err != nil || n != max {
		t.Fatalf("passes in flight = %d, %v; want %d", n, err, max)
	}
}
