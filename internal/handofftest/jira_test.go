package handofftest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// fakeJira is a Jira with tickets, recording every call made to it.
type fakeJira struct {
	mu      sync.Mutex
	tickets map[string]service.RemoteTicket
	calls   []string
}

func newFakeJira(tickets ...service.RemoteTicket) *fakeJira {
	f := &fakeJira{tickets: map[string]service.RemoteTicket{}}
	for _, t := range tickets {
		f.tickets[t.ID] = t
	}
	return f
}

// set changes a ticket, as someone editing it in Jira does.
func (f *fakeJira) set(t service.RemoteTicket) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tickets[t.ID] = t
}

func (f *fakeJira) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeJira) Search(_ context.Context, jql string) ([]service.RemoteTicket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "search "+jql)
	var out []service.RemoteTicket
	for key, t := range f.tickets {
		if strings.Contains(jql, `"`+key+`"`) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeJira) GetTicket(_ context.Context, id string) (*service.RemoteTicket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "get "+id)
	t, ok := f.tickets[id]
	if !ok {
		return nil, errors.New("no such ticket")
	}
	return &t, nil
}

func (f *fakeJira) ListTransitions(_ context.Context, id string) ([]service.RemoteTransition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "transitions "+id)
	return nil, nil
}

func (f *fakeJira) DoTransition(_ context.Context, id, transition string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "transition "+id+" "+transition)
	return nil
}

var checkoutTicket = service.RemoteTicket{
	ID: "ACME-12", Summary: "Checkout times out on large carts", Status: "In Progress",
	DescriptionMD: "Carts over 50 items time out at checkout.\n\nAcceptance: checkout under 2s.",
	URL:           "https://acme.atlassian.net/browse/ACME-12", UpdatedAt: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
}

// jiraTask imports a Jira ticket on the main through its Jira sync and gives
// it Legato details of its own and a note. It returns the main's sync.
func jiraTask(t *testing.T, main *Main, jira *fakeJira, details, note string) service.SyncService {
	t.Helper()
	ctx := context.Background()
	sync := service.NewSyncService(main.Store, main.Bus, jira, "", []string{"ACME"}, time.Hour, true)
	if _, err := sync.ImportRemoteTask(ctx, "jira", checkoutTicket.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := main.Board.UpdateTaskDescription(ctx, checkoutTicket.ID, details); err != nil {
		t.Fatal(err)
	}
	if note != "" {
		if _, err := service.AddTaskNote(ctx, main.Store, checkoutTicket.ID, note, "agent"); err != nil {
			t.Fatal(err)
		}
	}
	return sync
}

func TestAJiraBackedTaskGoesToTheWorkerAsAReadOnlyCopyWithEditableDetails(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	jira := newFakeJira(checkoutTicket)
	jiraTask(t, main, jira, "Start with the cart serializer.", "Profiled: the serializer is 80% of it.")

	away, err := handOff(t, main, paired.InstanceID, checkoutTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	// The worker's card: Jira's title and text, a copy that is not Jira's
	// to sync, then the Legato details.
	card, err := worker.Board.GetCard(ctx, away.WorkerTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if card.Title != checkoutTicket.Summary || card.RemoteDescriptionMD != checkoutTicket.DescriptionMD ||
		card.LocalDescriptionMD != "Start with the cart serializer." {
		t.Fatalf("worker card %q: remote %q, local %q", card.Title, card.RemoteDescriptionMD, card.LocalDescriptionMD)
	}
	if card.Provider != service.HandoffCopyProvider("jira") || card.RemoteID != "ACME-12" || card.RemoteMeta["url"] != checkoutTicket.URL {
		t.Fatalf("worker card from %q %q, meta %v", card.Provider, card.RemoteID, card.RemoteMeta)
	}
	if !strings.Contains(card.DescriptionMD, checkoutTicket.DescriptionMD) || !strings.Contains(card.DescriptionMD, "## Legato details\n\nStart with the cart serializer.") {
		t.Fatalf("the card's whole description:\n%s", card.DescriptionMD)
	}
	if len(card.Notes) != 1 || card.Notes[0].Source != service.NoteSourceMain {
		t.Fatalf("worker notes %+v", card.Notes)
	}

	// The brief gives the agent Jira's text read only, then the details.
	spawned := worker.Tmux.Spawned()
	if len(spawned) != 1 {
		t.Fatalf("spawned %+v", spawned)
	}
	brief := briefOf(t, spawned[0])
	jiraAt := strings.Index(brief, "### From Jira ACME-12 (read only)\n\n<ticket provider=\"Jira\" key=\"ACME-12\">\n"+checkoutTicket.DescriptionMD)
	detailsAt := strings.Index(brief, "### Details\n\n<task-details>\nStart with the cart serializer.")
	if jiraAt < 0 || detailsAt < jiraAt {
		t.Fatalf("the brief:\n%s", brief)
	}

	// On the worker, the title and Jira's text are read only; the Legato
	// details are the one part that can change.
	if err := worker.Board.UpdateTaskTitle(ctx, away.WorkerTaskID, "Something else"); !errors.Is(err, service.ErrRemoteTaskReadOnly) {
		t.Fatalf("editing the title on the worker: %v", err)
	}
	if err := worker.Board.UpdateTaskDescription(ctx, away.WorkerTaskID, "The serializer copies each item twice."); err != nil {
		t.Fatal(err)
	}
	card, _ = worker.Board.GetCard(ctx, away.WorkerTaskID)
	if card.Title != checkoutTicket.Summary || card.RemoteDescriptionMD != checkoutTicket.DescriptionMD || card.LocalDescriptionMD != "The serializer copies each item twice." {
		t.Fatalf("after editing: %q, remote %q, local %q", card.Title, card.RemoteDescriptionMD, card.LocalDescriptionMD)
	}

	// The worker never talks to Jira, even with a Jira of its own: its sync
	// does not take the card for a ticket, and moving the card moves no
	// ticket.
	workerJira := newFakeJira(checkoutTicket)
	workerSync := service.NewSyncService(worker.Store, worker.Bus, workerJira, "", []string{"ACME"}, time.Hour, true)
	if _, err := workerSync.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := workerSync.(service.CardMoveSync).PushMove(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	if calls := workerJira.Calls(); len(calls) != 0 {
		t.Fatalf("the worker asked Jira %v", calls)
	}
}

func TestOnlyTheLegatoDetailsOfAJiraBackedTaskComeBack(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	jira := newFakeJira(checkoutTicket)
	sync := jiraTask(t, main, jira, "Start with the cart serializer.", "")
	away, err := handOff(t, main, paired.InstanceID, checkoutTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	card := away.WorkerTaskID
	note, err := service.AddTaskNote(ctx, worker.Store, card, "The serializer copies each item twice.", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Board.UpdateTaskDescription(ctx, card, "Fixed by serializing once; see the PR."); err != nil {
		t.Fatal(err)
	}
	// Whatever the card's title and copy of Jira's text say on the worker,
	// they never come back.
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

	// While the task is away, the main's Jira sync goes on as before.
	changed := checkoutTicket
	changed.Summary, changed.DescriptionMD, changed.UpdatedAt = "Checkout times out on carts over 50 items", "Now also seen on mobile.", checkoutTicket.UpdatedAt.Add(time.Hour)
	jira.set(changed)
	if _, err := sync.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := main.Store.GetTask(ctx, checkoutTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != changed.Summary || task.RemoteDescriptionMD != changed.DescriptionMD {
		t.Fatalf("away, the sync left %q / %q", task.Title, task.RemoteDescriptionMD)
	}

	eventually(t, "done on the main", func() bool { return mainAway(t, main, checkoutTicket.ID).View == service.AwayViewDone })
	if _, err := main.Handoffs.Return(ctx, checkoutTicket.ID); err != nil {
		t.Fatal(err)
	}

	// The Legato details and the worker's note came back; the title, Jira's
	// text and the link to Jira are the main's own.
	task, err = main.Store.GetTask(ctx, checkoutTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != changed.Summary || task.RemoteDescriptionMD != changed.DescriptionMD || task.DescriptionMD != "Fixed by serializing once; see the PR." {
		t.Fatalf("returned %q: remote %q, local %q", task.Title, task.RemoteDescriptionMD, task.DescriptionMD)
	}
	if task.Provider == nil || *task.Provider != "jira" || task.RemoteID == nil || *task.RemoteID != "ACME-12" || task.Status != "Done" {
		t.Fatalf("returned from %v %v in %s", task.Provider, task.RemoteID, task.Status)
	}
	var back []store.TaskNote
	for _, n := range notesOf(t, main, checkoutTicket.ID) {
		if n.Source == service.NoteSourceWorker {
			back = append(back, n)
		}
	}
	if len(back) != 1 || back[0].RemoteID != note.ID {
		t.Fatalf("the worker's notes on the main: %+v", back)
	}

	// And once home, the Jira sync goes on.
	later := changed
	later.Summary, later.UpdatedAt = "Checkout is slow on large carts", changed.UpdatedAt.Add(time.Hour)
	jira.set(later)
	if _, err := sync.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	task, _ = main.Store.GetTask(ctx, checkoutTicket.ID)
	if task.Title != later.Summary || task.DescriptionMD != "Fixed by serializing once; see the PR." {
		t.Fatalf("home, the sync left %q / %q", task.Title, task.DescriptionMD)
	}
}

// The worker refuses a ticket it could not show safely, making nothing.
func TestTheWorkerRefusesAMalformedTicket(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	token := mainToken(t, main, paired.InstanceID)
	for name, ticket := range map[string]service.HandoffTicket{
		"no key":               {Provider: "jira", Text: "Carts time out."},
		"a script address":     {Provider: "jira", Key: "ACME-12", URL: "javascript:alert(1)"},
		"no provider":          {Key: "ACME-12", Text: "Carts time out."},
		"a provider not named": {Provider: "Jira <b>", Key: "ACME-12"},
	} {
		body, _ := json.Marshal(service.HandoffCall{HandoffID: "ho-" + strings.ReplaceAll(name, " ", "-"), MainTaskID: "ACME-12", Title: "Checkout times out",
			Ticket: &ticket, Notes: []service.HandoffCallNote{}, Repo: "acme/widget", Purpose: service.HandoffMakeChange,
			Kind: service.HandoffNewBranch, Branch: "legato/acme-12", AgentKind: "claude-code"})
		resp := worker.Do(http.MethodPost, service.HandoffCallPath, token, string(body))
		var refused struct{ Error, Message string }
		_ = json.NewDecoder(resp.Body).Decode(&refused)
		if resp.StatusCode != http.StatusBadRequest || refused.Error != "bad_request" || !strings.Contains(refused.Message, "ticket") {
			t.Fatalf("%s: %d %+v", name, resp.StatusCode, refused)
		}
	}
	if cards := workerCards(t, worker); len(cards) != 0 {
		t.Fatalf("the worker made %+v", cards)
	}
}
