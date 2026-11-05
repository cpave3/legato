package handofftest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// fakeBurnlite is a Burnlite ticket source with tasks in memory, recording
// every call made to it. Burnlite is read-only to Legato: it has no moves.
type fakeBurnlite struct {
	mu      sync.Mutex
	tickets map[string]service.RemoteTicket
	calls   []string
}

func newFakeBurnlite(tickets ...service.RemoteTicket) *fakeBurnlite {
	f := &fakeBurnlite{tickets: map[string]service.RemoteTicket{}}
	for _, t := range tickets {
		f.tickets[t.ID] = t
	}
	return f
}

// set changes a task, as someone editing it in Burnlite does.
func (f *fakeBurnlite) set(t service.RemoteTicket) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tickets[t.ID] = t
}

func (f *fakeBurnlite) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeBurnlite) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeBurnlite) Name() string                 { return service.BurnliteIntegration }
func (f *fakeBurnlite) Label() string                { return "Burnlite" }
func (f *fakeBurnlite) Enabled(context.Context) bool { return true }
func (f *fakeBurnlite) Interval() time.Duration      { return time.Hour }
func (f *fakeBurnlite) MovesCards() bool             { return true }

func (f *fakeBurnlite) Column(_ context.Context, t service.RemoteTicket) (string, error) {
	if t.Status == "Claimed" {
		return "Doing", nil
	}
	return "Ready", nil
}

func (f *fakeBurnlite) Pull(_ context.Context, tracked []store.Task) (service.PullBatch, error) {
	var keys []string
	var batch service.PullBatch
	f.mu.Lock()
	for _, task := range tracked {
		key := task.ID
		if task.RemoteID != nil {
			key = *task.RemoteID
		}
		keys = append(keys, key)
		if t, ok := f.tickets[key]; ok {
			batch.Tickets = append(batch.Tickets, t)
		} else {
			batch.Missing = append(batch.Missing, key)
		}
	}
	f.mu.Unlock()
	f.record("pull " + strings.Join(keys, ","))
	return batch, nil
}

func (f *fakeBurnlite) Search(_ context.Context, text string) ([]service.RemoteTicket, error) {
	f.record("search " + text)
	return nil, nil
}

func (f *fakeBurnlite) Get(_ context.Context, key string) (*service.RemoteTicket, error) {
	f.record("get " + key)
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tickets[key]; ok {
		return &t, nil
	}
	return nil, nil
}

// estimateTicket is a Burnlite task as the real source reads it: its
// Description, then its Plan and Checklist, as the ticket's text.
var estimateTicket = service.RemoteTicket{
	ID: "LEG-33", Summary: "Show a task's estimate on its card", Status: "Ready", IssueType: "Work",
	DescriptionMD: "A card should say how long its task is estimated to take.\n\n## Plan\n\nRead the size from remote_meta.\n\n## Checklist\n\n- [ ] Show it on the card",
	URL:           "https://burnlite.example/LEG-33", UpdatedAt: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
}

// burnliteTask imports a Burnlite task on the main through its sync and
// gives it Legato details of its own and a note. It returns the main's sync.
func burnliteTask(t *testing.T, main *Main, burnlite *fakeBurnlite, details, note string) service.SyncService {
	t.Helper()
	ctx := context.Background()
	sync := service.NewTicketSync(main.Store, main.Bus, nil, burnlite)
	if _, err := sync.ImportRemoteTask(ctx, service.BurnliteIntegration, estimateTicket.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := main.Board.UpdateTaskDescription(ctx, estimateTicket.ID, details); err != nil {
		t.Fatal(err)
	}
	if note != "" {
		if _, err := service.AddTaskNote(ctx, main.Store, estimateTicket.ID, note, "agent"); err != nil {
			t.Fatal(err)
		}
	}
	return sync
}

func TestABurnliteBackedTaskGoesToTheWorkerAsAReadOnlyCopyWithEditableDetails(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	burnlite := newFakeBurnlite(estimateTicket)
	burnliteTask(t, main, burnlite, "Keep it to one line on the card.", "The size is in remote_meta already.")

	away, err := handOff(t, main, paired.InstanceID, estimateTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	// The worker's card: Burnlite's name and text, a copy no source on the
	// worker syncs, then the Legato details.
	card, err := worker.Board.GetCard(ctx, away.WorkerTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if card.Title != estimateTicket.Summary || card.RemoteDescriptionMD != estimateTicket.DescriptionMD ||
		card.LocalDescriptionMD != "Keep it to one line on the card." {
		t.Fatalf("worker card %q: remote %q, local %q", card.Title, card.RemoteDescriptionMD, card.LocalDescriptionMD)
	}
	if card.Provider != "burnlite-copy" || card.RemoteID != "LEG-33" || card.RemoteMeta["url"] != estimateTicket.URL {
		t.Fatalf("worker card from %q %q, meta %v", card.Provider, card.RemoteID, card.RemoteMeta)
	}
	if len(card.Notes) != 1 || card.Notes[0].Source != service.NoteSourceMain {
		t.Fatalf("worker notes %+v", card.Notes)
	}

	// The brief gives the agent Burnlite's text read only, then the details.
	spawned := worker.Tmux.Spawned()
	if len(spawned) != 1 {
		t.Fatalf("spawned %+v", spawned)
	}
	brief := briefOf(t, spawned[0])
	ticketAt := strings.Index(brief, "### From Burnlite LEG-33 (read only)\n\n"+estimateTicket.DescriptionMD)
	detailsAt := strings.Index(brief, "### Details\n\nKeep it to one line on the card.")
	if ticketAt < 0 || detailsAt < ticketAt {
		t.Fatalf("the brief:\n%s", brief)
	}

	// The title and Burnlite's text are read only; the details are not.
	if err := worker.Board.UpdateTaskTitle(ctx, away.WorkerTaskID, "Something else"); !errors.Is(err, service.ErrRemoteTaskReadOnly) {
		t.Fatalf("editing the title on the worker: %v", err)
	}
	if err := worker.Board.UpdateTaskDescription(ctx, away.WorkerTaskID, "Show it beside the priority."); err != nil {
		t.Fatal(err)
	}
	card, _ = worker.Board.GetCard(ctx, away.WorkerTaskID)
	if card.Title != estimateTicket.Summary || card.RemoteDescriptionMD != estimateTicket.DescriptionMD || card.LocalDescriptionMD != "Show it beside the priority." {
		t.Fatalf("after editing: %q, remote %q, local %q", card.Title, card.RemoteDescriptionMD, card.LocalDescriptionMD)
	}

	// The worker never talks to Burnlite, even connected to one of its own.
	workerBurnlite := newFakeBurnlite(estimateTicket)
	workerSync := service.NewTicketSync(worker.Store, worker.Bus, nil, workerBurnlite)
	if _, err := workerSync.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := workerSync.(service.CardMoveSync).PushMove(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	if calls := workerBurnlite.Calls(); len(calls) != 0 {
		t.Fatalf("the worker asked Burnlite %v", calls)
	}
}

func TestOnlyTheLegatoDetailsOfABurnliteBackedTaskComeBack(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	burnlite := newFakeBurnlite(estimateTicket)
	sync := burnliteTask(t, main, burnlite, "Keep it to one line on the card.", "")
	away, err := handOff(t, main, paired.InstanceID, estimateTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	card := away.WorkerTaskID
	note, err := service.AddTaskNote(ctx, worker.Store, card, "The card had room beside the priority.", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Board.UpdateTaskDescription(ctx, card, "Shown beside the priority; see the PR."); err != nil {
		t.Fatal(err)
	}
	// Whatever the worker's copy says, it never comes back.
	workerCard, err := worker.Store.GetTask(ctx, card)
	if err != nil {
		t.Fatal(err)
	}
	workerCard.Title, workerCard.RemoteDescriptionMD = "Renamed on the worker", "Rewritten on the worker"
	if err := worker.Store.UpdateTask(ctx, *workerCard); err != nil {
		t.Fatal(err)
	}
	if err := worker.Board.MoveCard(ctx, card, "Done"); err != nil {
		t.Fatal(err)
	}

	// While the task is away, the main's Burnlite sync goes on as before.
	changed := estimateTicket
	changed.Summary, changed.DescriptionMD, changed.UpdatedAt = "Show a task's estimate on its board card", "Now with the uncertainty too.", estimateTicket.UpdatedAt.Add(time.Hour)
	burnlite.set(changed)
	if _, err := sync.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := main.Store.GetTask(ctx, estimateTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != changed.Summary || task.RemoteDescriptionMD != changed.DescriptionMD {
		t.Fatalf("away, the sync left %q / %q", task.Title, task.RemoteDescriptionMD)
	}

	eventually(t, "done on the main", func() bool { return mainAway(t, main, estimateTicket.ID).View == service.AwayViewDone })
	if _, err := main.Handoffs.Return(ctx, estimateTicket.ID); err != nil {
		t.Fatal(err)
	}

	// The Legato details and the worker's note came back; the title,
	// Burnlite's text and the link to Burnlite are the main's own.
	task, err = main.Store.GetTask(ctx, estimateTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != changed.Summary || task.RemoteDescriptionMD != changed.DescriptionMD || task.DescriptionMD != "Shown beside the priority; see the PR." {
		t.Fatalf("returned %q: remote %q, local %q", task.Title, task.RemoteDescriptionMD, task.DescriptionMD)
	}
	if task.Provider == nil || *task.Provider != service.BurnliteIntegration || task.RemoteID == nil || *task.RemoteID != "LEG-33" || task.Status != "Done" {
		t.Fatalf("returned from %v %v in %s", task.Provider, task.RemoteID, task.Status)
	}
	var back []store.TaskNote
	for _, n := range notesOf(t, main, estimateTicket.ID) {
		if n.Source == service.NoteSourceWorker {
			back = append(back, n)
		}
	}
	if len(back) != 1 || back[0].RemoteID != note.ID {
		t.Fatalf("the worker's notes on the main: %+v", back)
	}

	// And once home, the Burnlite sync goes on.
	later := changed
	later.Summary, later.UpdatedAt = "Show estimates on cards", changed.UpdatedAt.Add(time.Hour)
	burnlite.set(later)
	if _, err := sync.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	task, _ = main.Store.GetTask(ctx, estimateTicket.ID)
	if task.Title != later.Summary || task.DescriptionMD != "Shown beside the priority; see the PR." {
		t.Fatalf("home, the sync left %q / %q", task.Title, task.DescriptionMD)
	}
}

// A provider-backed task with no key of its provider's has no ticket to
// send: it is refused, and nothing is made.
func TestAProviderBackedTaskWithNoKeyIsNotHandedOff(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	provider := service.BurnliteIntegration
	now := time.Now().UTC().Format(time.RFC3339)
	if err := main.Store.CreateTask(ctx, store.Task{ID: "K7Q2M9XA", Title: "A Burnlite task", Status: "Ready", Provider: &provider, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := handOff(t, main, paired.InstanceID, "K7Q2M9XA"); !errors.Is(err, service.ErrHandoffInput) {
		t.Fatalf("handing off a task with no key: %v", err)
	}
	if cards := workerCards(t, worker); len(cards) != 0 {
		t.Fatalf("the worker made %+v", cards)
	}
}
