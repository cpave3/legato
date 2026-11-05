package handofftest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Attention on the main for its away tasks (LEG-30): only what needs an
// action only the main can take, and a phone notification only for what
// the worker cannot report itself.

// phone is a notifier that keeps what it was asked to send.
type phone struct {
	mu   sync.Mutex
	sent []notice
}

type notice struct{ title, message, link string }

func (p *phone) Notify(title, message string) error { return p.NotifyLink(title, message, "") }
func (p *phone) NotifyLink(title, message, link string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, notice{title, message, link})
	return nil
}
func (p *phone) Configured() bool      { return true }
func (p *phone) CanNotify(string) bool { return true }

func (p *phone) notices() []notice {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]notice(nil), p.sent...)
}

// sent waits for n notifications on the phones together, sent in the
// background, and gives them; a moment more shows none beyond n.
func sent(t *testing.T, n int, phones ...*phone) []notice {
	t.Helper()
	all := func() []notice {
		var out []notice
		for _, p := range phones {
			out = append(out, p.notices()...)
		}
		return out
	}
	eventually(t, fmt.Sprintf("%d notifications", n), func() bool { return len(all()) >= n })
	time.Sleep(30 * time.Millisecond)
	got := all()
	if len(got) != n {
		t.Fatalf("notifications %+v, want %d", got, n)
	}
	return got
}

// mainAttention is a main's Attention with the handoff producer, as a
// running main has it: reconciled on the board's events, sending phone
// notifications to the returned phone.
type mainAttention struct {
	*service.AttentionService
	adapter *service.HandoffAttentionAdapter
	phone   *phone
}

const mainAddress = "https://main.example:3080"

func attentionOf(t *testing.T, main *Main) *mainAttention {
	t.Helper()
	p := &phone{}
	adapter := service.NewHandoffAttentionAdapter(main.Store, service.HandoffAttentionOptions{
		Handoffs: main.Handoffs, Now: main.Clock.Now, Location: time.UTC,
		Notifier: p, ServerURL: func() string { return mainAddress },
	})
	attention := service.NewAttentionService(main.Store, main.Bus)
	if err := adapter.Register(attention); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(attention.StartEventReconciliation(context.Background()))
	return &mainAttention{AttentionService: attention, adapter: adapter, phone: p}
}

// handoffItems reconciles the handoff producer and lists its items.
func (a *mainAttention) handoffItems(t *testing.T) []service.AttentionItem {
	t.Helper()
	ctx := context.Background()
	if err := a.Reconcile(ctx, service.HandoffAttentionProducer); err != nil {
		t.Fatal(err)
	}
	items, err := a.List(ctx, service.AttentionFilter{Producer: service.HandoffAttentionProducer})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// eventuallyItem waits for the handoff producer's one item of a signal.
func (a *mainAttention) eventuallyItem(t *testing.T, signal string) service.AttentionItem {
	t.Helper()
	var found service.AttentionItem
	eventually(t, "a "+signal+" item", func() bool {
		for _, item := range a.handoffItems(t) {
			if item.SignalKind == signal {
				found = item
				return true
			}
		}
		return false
	})
	return found
}

func action(t *testing.T, item service.AttentionItem, id string) service.AttentionAction {
	t.Helper()
	for _, a := range item.Actions {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("item %q has no action %q: %+v", item.Title, id, item.Actions)
	return service.AttentionAction{}
}

func TestADoneAwayTaskAsksToBeReturnedAndNeedsYouRaisesNothing(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	attention := attentionOf(t, main)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")

	// The agent asks the person something: the worker notifies for it, and
	// the main shows only the lane's pill.
	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "waiting", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "needs you", func() bool { return mainAway(t, main, taskID).View == service.AwayViewNeedsYou })
	if items := attention.handoffItems(t); len(items) != 0 {
		t.Fatalf("items for a task that needs you: %+v", items)
	}

	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	item := attention.eventuallyItem(t, service.AttentionSignalHandoffDone)
	workerURL := mainAway(t, main, taskID).WorkerURL
	if item.Title != "Fix it is done on vm-1" || item.TaskID != taskID || item.Urgency != "normal" ||
		item.Navigation.Kind != "task" || item.Navigation.Target != taskID {
		t.Fatalf("done item %+v", item)
	}
	if a := action(t, item, "return"); a.Kind != "execute" {
		t.Fatalf("return %+v", a)
	}
	if a := action(t, item, "open_worker"); a.Kind != "navigate" || a.Href != workerURL || workerURL == "" {
		t.Fatalf("open on worker %+v, want %s", a, workerURL)
	}
	if a := action(t, item, "open"); a.Kind != "navigate" {
		t.Fatalf("open %+v", a)
	}
	// The worker's agent-finished notification covers it.
	time.Sleep(30 * time.Millisecond)
	if got := attention.phone.notices(); len(got) != 0 {
		t.Fatalf("notified of a done task: %+v", got)
	}

	// Return brings it home, and the item goes.
	if err := attention.Execute(ctx, item.ID, service.AttentionActionRequest{ActionID: "return", ExpectedRevision: item.SourceRevision}); err != nil {
		t.Fatal(err)
	}
	if got, err := main.Handoffs.Away(ctx, taskID); err != nil || got != nil {
		t.Fatalf("still away after Return: %+v, %v", got, err)
	}
	if items := attention.handoffItems(t); len(items) != 0 {
		t.Fatalf("items once returned: %+v", items)
	}
}

func TestDismissingADoneItemHidesOnlyThatState(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	attention := attentionOf(t, main)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	item := attention.eventuallyItem(t, service.AttentionSignalHandoffDone)
	if err := attention.Dismiss(ctx, item.ID, item.SourceRevision); err != nil {
		t.Fatal(err)
	}
	if items := attention.handoffItems(t); len(items) != 0 {
		t.Fatalf("a dismissed item is back: %+v", items)
	}

	// The person moves it back to work on the worker: the item resolves.
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Doing"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "not done", func() bool { return mainAway(t, main, taskID).State == store.HandoffInProgress })
	if items := attention.handoffItems(t); len(items) != 0 {
		t.Fatalf("items while it is worked on: %+v", items)
	}
	// Done again is a new state, which raises the item again.
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	attention.eventuallyItem(t, service.AttentionSignalHandoffDone)
}

func TestAFailedHandoffRaisesRecallAndNotifiesThePhoneOnce(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	attention := attentionOf(t, main)
	// The TUI and legato serve on one database each run the producer.
	other := attentionOf(t, main)
	main.StartConnections()
	worker.Repos.Fail("acme/widget", errors.New("cloning acme/widget: repository not found"))
	taskID, _ := awayOn(t, main, worker, paired.InstanceID, "Fix it")

	item := attention.eventuallyItem(t, service.AttentionSignalHandoffFailed)
	if item.Title != "Handing Fix it to vm-1 failed" || !strings.Contains(item.Reason, "repository not found") ||
		item.Urgency != "high" || item.TaskID != taskID {
		t.Fatalf("failed item %+v", item)
	}
	if a := action(t, item, "recall"); a.Kind != "execute" {
		t.Fatalf("recall %+v", a)
	}
	action(t, item, "open_worker")
	other.handoffItems(t)
	attention.handoffItems(t)

	got := sent(t, 1, attention.phone, other.phone)
	if !strings.Contains(got[0].title, "Fix it") || !strings.Contains(got[0].message, "repository not found") ||
		got[0].link != mainAddress+"/board?highlight_task="+taskID {
		t.Fatalf("notification %+v", got[0])
	}

	if err := attention.Execute(ctx, item.ID, service.AttentionActionRequest{ActionID: "recall", ExpectedRevision: item.SourceRevision}); err != nil {
		t.Fatal(err)
	}
	if got, err := main.Handoffs.Away(ctx, taskID); err != nil || got != nil {
		t.Fatalf("still away after Recall: %+v, %v", got, err)
	}
	if items := attention.handoffItems(t); len(items) != 0 {
		t.Fatalf("items once recalled: %+v", items)
	}
}

func TestALostTaskOffersTakeBackAfterAConfirmAndNotifies(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	attention := attentionOf(t, main)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Gone")
	eventually(t, "a row", func() bool { return mainAway(t, main, taskID).View == service.AwayViewIdle })
	if err := worker.Store.DeleteTask(ctx, away.WorkerTaskID); err != nil {
		t.Fatal(err)
	}
	worker.Bus.Publish(events.Event{Type: events.EventCardsRefreshed})

	item := attention.eventuallyItem(t, service.AttentionSignalHandoffLost)
	if item.Title != "Gone is lost on vm-1" || item.Urgency != "high" || item.TaskID != taskID {
		t.Fatalf("lost item %+v", item)
	}
	takeBack := action(t, item, "take_back")
	if takeBack.Kind != "execute" || !takeBack.RequiresConfirm || takeBack.Confirmation == "" {
		t.Fatalf("take back %+v", takeBack)
	}
	if got := sent(t, 1, attention.phone); !strings.Contains(got[0].title, "Gone") {
		t.Fatalf("notifications %+v", got)
	}

	// Never without the person's confirmation.
	if err := attention.Execute(ctx, item.ID, service.AttentionActionRequest{ActionID: "take_back", ExpectedRevision: item.SourceRevision}); err == nil {
		t.Fatal("took a task back unconfirmed")
	}
	if err := attention.Execute(ctx, item.ID, service.AttentionActionRequest{ActionID: "take_back", ExpectedRevision: item.SourceRevision, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	homeWithoutResults(t, main, taskID, "Ready")
	if items := attention.handoffItems(t); len(items) != 0 {
		t.Fatalf("items once taken back: %+v", items)
	}
}

func TestAWorkerUnreachableFor15MinutesRaisesOneItemAndOneNotificationPerOutage(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	attention := attentionOf(t, main)
	main.StartConnections()
	firstID, _ := awayOn(t, main, worker, paired.InstanceID, "First")
	awayOn(t, main, worker, paired.InstanceID, "Second")
	eventually(t, "a row", func() bool { return mainAway(t, main, firstID).View == service.AwayViewIdle })

	dropped := worker.Clock.Now()
	unreachable(t, main, worker, paired.InstanceID, firstID)
	if items := attention.handoffItems(t); len(items) != 0 {
		t.Fatalf("items after 31 seconds down: %+v", items)
	}
	worker.Clock.Advance(14 * time.Minute)
	if items := attention.handoffItems(t); len(items) != 0 {
		t.Fatalf("items before 15 minutes down: %+v", items)
	}
	worker.Clock.Advance(time.Minute)
	item := attention.eventuallyItem(t, service.AttentionSignalWorkerUnreachable)
	want := "vm-1 unreachable since " + dropped.UTC().Format("15:04") + ", 2 away tasks"
	if item.Title != want || item.TaskID != "" || item.Navigation.Kind != "settings" || item.Navigation.Target != "workers" {
		t.Fatalf("unreachable item %+v, want %q", item, want)
	}
	if a := action(t, item, "open"); a.Kind != "navigate" || a.Label != "Open Settings › Workers" {
		t.Fatalf("open %+v", a)
	}
	attention.handoffItems(t)
	worker.Clock.Advance(time.Hour)
	attention.handoffItems(t)
	if got := sent(t, 1, attention.phone); got[0].link != mainAddress+"/settings#workers" {
		t.Fatalf("notifications %+v", got)
	}

	// Back up: the item resolves.
	worker.Restart()
	eventually(t, "the reconnection", func() bool { return workerRecord(t, main, paired.InstanceID).UnreachableSince == nil })
	eventually(t, "the item resolved", func() bool { return len(attention.handoffItems(t)) == 0 })

	// A new outage notifies again, once.
	unreachable(t, main, worker, paired.InstanceID, firstID)
	worker.Clock.Advance(15 * time.Minute)
	attention.eventuallyItem(t, service.AttentionSignalWorkerUnreachable)
	attention.handoffItems(t)
	sent(t, 2, attention.phone)
}

func TestAWorkerThatNoLongerAcceptsTheMainAsksToPairAgain(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	attention := attentionOf(t, main)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	awayOn(t, main, worker, paired.InstanceID, "Another")
	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "working", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "working", func() bool { return mainAway(t, main, taskID).View == service.AwayViewWorking })

	if err := worker.Tokens.RevokeMain(ctx, main.InstanceID()); err != nil {
		t.Fatal(err)
	}
	item := attention.eventuallyItem(t, service.AttentionSignalWorkerNotAccepting)
	if item.Title != "vm-1 no longer accepts this main" || item.Urgency != "high" ||
		item.Navigation.Kind != "settings" || item.Navigation.Target != "workers" {
		t.Fatalf("not accepting item %+v", item)
	}
	if a := action(t, item, "pair"); a.Kind != "navigate" || a.Label != "Pair again" {
		t.Fatalf("pair again %+v", a)
	}
	// One item for the worker, none for each task.
	if items := attention.handoffItems(t); len(items) != 1 {
		t.Fatalf("items %+v", items)
	}
	if got := sent(t, 1, attention.phone); !strings.Contains(got[0].title, "vm-1") {
		t.Fatalf("notifications %+v", got)
	}
}
