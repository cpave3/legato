package handofftest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// workedOn hands a task with a note of the main's to a live main's worker
// and has the worker's agent work on it: a note of its own, the title and
// description edited, branches pushed, and the card moved to column. It
// returns the main's task ID, the away task and the worker's note.
func workedOn(t *testing.T, main *Main, worker *Worker, workerID, column string, branches ...string) (string, service.AwayTask, store.TaskNote) {
	t.Helper()
	ctx := context.Background()
	taskID, _ := localTask(t, main, "Fix the flaky login test", "It fails one run in ten.", "Seen on CI since Tuesday.")
	away, err := handOff(t, main, workerID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	card := away.WorkerTaskID
	note, err := service.AddTaskNote(ctx, worker.Store, card, "Chose to extend the cookie, not retry: retries hide the bug.", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Board.UpdateTaskTitle(ctx, card, "Fix the login test's expired cookie"); err != nil {
		t.Fatal(err)
	}
	if err := worker.Board.UpdateTaskDescription(ctx, card, "It fails when the cookie expires mid-run."); err != nil {
		t.Fatal(err)
	}
	if len(branches) > 0 {
		clone := filepath.Join(worker.Dir, "jobs", worker.Repos.Clones()[0].ExecutionID, "widget")
		pushBranches(t, clone, branches...)
	}
	if err := worker.Board.MoveCard(ctx, card, column); err != nil {
		t.Fatal(err)
	}
	return taskID, away, note
}

// notesOf are a main task's notes.
func notesOf(t *testing.T, main *Main, taskID string) []store.TaskNote {
	t.Helper()
	notes, err := main.Store.ListTaskNotes(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

// workerHandoff is the worker's record of a handoff.
func workerHandoff(t *testing.T, worker *Worker, main *Main, handoffID string) *store.Handoff {
	t.Helper()
	h, err := worker.Store.GetHandoff(context.Background(), main.InstanceID(), handoffID)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// returnedHome checks a task came back: not away, with the worker's title,
// description, notes and branches, in column.
func returnedHome(t *testing.T, main *Main, taskID, column string, workerNote store.TaskNote, branches ...string) {
	t.Helper()
	ctx := context.Background()
	if away, err := main.Handoffs.Away(ctx, taskID); err != nil || away != nil {
		t.Fatalf("still away: %+v, %v", away, err)
	}
	task, err := main.Store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "Fix the login test's expired cookie" || task.DescriptionMD != "It fails when the cookie expires mid-run." || task.Status != column {
		t.Fatalf("task %q / %q in %s", task.Title, task.DescriptionMD, task.Status)
	}
	var fromWorker []store.TaskNote
	mainNotes := 0
	for _, n := range notesOf(t, main, taskID) {
		switch n.Source {
		case service.NoteSourceWorker:
			fromWorker = append(fromWorker, n)
		case "agent":
			mainNotes++
		}
	}
	if mainNotes != 1 {
		t.Fatalf("the main's own note is there %d times", mainNotes)
	}
	if len(fromWorker) != 1 || fromWorker[0].Body != workerNote.Body || fromWorker[0].RemoteID != workerNote.ID {
		t.Fatalf("the worker's notes on the main: %+v", fromWorker)
	}
	cc, err := store.ParseCodeContext(task.CodeContext)
	if err != nil || cc == nil || cc.Repo != "acme/widget" {
		t.Fatalf("code context %+v, %v", cc, err)
	}
	if len(branches) > 0 && cc.Branch != branches[0] {
		t.Fatalf("code context branch %q, want %q", cc.Branch, branches[0])
	}
	tracked, err := main.Store.ListTaskBranches(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range tracked {
		names = append(names, b.Branch)
	}
	if len(branches) > 0 && !reflect.DeepEqual(names, branches) {
		t.Fatalf("tracked branches %v, want %v", names, branches)
	}
}

// archivedOnWorker checks the worker closed the handoff, stopped the agent
// and archived its card.
func archivedOnWorker(t *testing.T, worker *Worker, main *Main, away service.AwayTask) {
	t.Helper()
	ctx := context.Background()
	h := workerHandoff(t, worker, main, away.HandoffID)
	if h.ClosedAt == nil {
		t.Fatalf("the worker's handoff is open: %+v", h)
	}
	task, err := worker.Store.GetTask(ctx, away.WorkerTaskID)
	if err != nil || task.ArchivedAt == nil {
		t.Fatalf("the worker's card is not archived: %+v, %v", task, err)
	}
	if session, err := worker.Store.GetAgentSessionByTaskID(ctx, away.WorkerTaskID); err == nil {
		t.Fatalf("the worker's agent still runs: %+v", session)
	}
}

func TestReturningADoneTaskBringsItsResultsHome(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	branch := "legato/"
	taskID, away, note := workedOn(t, main, worker, paired.InstanceID, "Done")
	branch += strings.ToLower(taskID)
	clone := filepath.Join(worker.Dir, "jobs", worker.Repos.Clones()[0].ExecutionID, "widget")
	pushBranches(t, clone, branch)
	eventually(t, "done on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewDone })

	back, err := main.Handoffs.Return(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if back.TaskID != taskID || back.Column != "Done" || back.Recalled {
		t.Fatalf("returned %+v", back)
	}
	returnedHome(t, main, taskID, "Done", note, branch)
	archivedOnWorker(t, worker, main, away)
	// No recall note on a return.
	for _, n := range notesOf(t, main, taskID) {
		if strings.HasPrefix(n.Body, "Recalled") {
			t.Fatalf("a return added %q", n.Body)
		}
	}
	// Once returned, the main's task changes as any other.
	if err := main.Board.UpdateTaskTitle(ctx, taskID, "Fixed"); err != nil {
		t.Fatal(err)
	}
	// A second Return finds nothing away.
	if _, err := main.Handoffs.Return(ctx, taskID); !errors.Is(err, service.ErrReturnRefused) {
		t.Fatalf("returning a task at home: %v", err)
	}
}

// returnCall makes step 1 (archive false) or step 2 of a return as main,
// straight to the worker.
func returnCall(t *testing.T, main *Main, workerID, handoffID string, archive bool, into any) error {
	t.Helper()
	ctx := context.Background()
	client, err := main.Workers.Client(ctx, workerID)
	if err != nil {
		t.Fatal(err)
	}
	path := service.HandoffReturnPath(handoffID)
	if archive {
		path = service.HandoffArchivePath(handoffID)
	}
	return client.Call(ctx, "POST", path, nil, into)
}

func TestTheWorkersTwoStepsAreSafeToRepeatInAnyOrder(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	_, away, note := workedOn(t, main, worker, paired.InstanceID, "Review")
	if _, err := worker.Store.GetAgentSessionByTaskID(ctx, away.WorkerTaskID); err != nil {
		t.Fatalf("no agent to stop: %v", err)
	}

	// Step 1 stops the agent, freezes the card, marks the handoff
	// returning, and answers with the task, twice alike.
	var first, again service.HandoffReturn
	if err := returnCall(t, main, paired.InstanceID, away.HandoffID, false, &first); err != nil {
		t.Fatal(err)
	}
	if err := returnCall(t, main, paired.InstanceID, away.HandoffID, false, &again); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, again) {
		t.Fatalf("step 1 again answered\n%+v\nnot\n%+v", again, first)
	}
	want := service.HandoffReturn{HandoffID: away.HandoffID, WorkerTaskID: away.WorkerTaskID, WorkerInstanceID: worker.InstanceID(),
		State: store.HandoffReturning, Title: "Fix the login test's expired cookie", Details: "It fails when the cookie expires mid-run.",
		Column: "Review", Notes: []service.HandoffCallNote{{ID: note.ID, Body: note.Body, CreatedAt: first.Notes[0].CreatedAt}},
		Repo: "acme/widget", Branches: []string{}}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("step 1 answered\n got %+v\nwant %+v", first, want)
	}
	if h := workerHandoff(t, worker, main, away.HandoffID); h.State != store.HandoffReturning || h.ClosedAt != nil {
		t.Fatalf("handoff %+v", h)
	}
	if _, err := worker.Store.GetAgentSessionByTaskID(ctx, away.WorkerTaskID); err == nil {
		t.Fatal("the agent still runs after step 1")
	}
	// The card is frozen: no moves, edits or agent starts.
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); !errors.Is(err, service.ErrTaskAway) ||
		err.Error() != "task "+away.WorkerTaskID+" is going back to workstation: it no longer changes here" {
		t.Fatalf("moving a returning card: %v", err)
	}
	if err := worker.Board.UpdateTaskTitle(ctx, away.WorkerTaskID, "Changed"); !errors.Is(err, service.ErrTaskAway) {
		t.Fatalf("editing a returning card: %v", err)
	}
	if err := worker.Agents.SpawnAgent(ctx, away.WorkerTaskID, 80, 24); !errors.Is(err, service.ErrTaskAway) {
		t.Fatalf("starting an agent on a returning card: %v", err)
	}

	// Step 2 archives the card and closes the handoff, twice alike; step 1
	// after it still answers.
	var archived service.HandoffArchived
	for range 2 {
		if err := returnCall(t, main, paired.InstanceID, away.HandoffID, true, &archived); err != nil {
			t.Fatal(err)
		}
	}
	archivedOnWorker(t, worker, main, away)
	var late service.HandoffReturn
	if err := returnCall(t, main, paired.InstanceID, away.HandoffID, false, &late); err != nil || late.Title != first.Title || len(late.Notes) != 1 {
		t.Fatalf("step 1 after step 2: %+v, %v", late, err)
	}

	// Another main's handoff is not found.
	other := NewMain(t, MainOptions{Name: "laptop", Clock: worker.Clock})
	otherPaired := Pair(t, other, worker)
	var refusal *service.WorkerRefusal
	if err := returnCall(t, other, otherPaired.InstanceID, away.HandoffID, false, &late); !errors.As(err, &refusal) || refusal.Code != "not_found" {
		t.Fatalf("another main's step 1: %v", err)
	}
	if err := returnCall(t, other, otherPaired.InstanceID, away.HandoffID, true, &archived); !errors.As(err, &refusal) || refusal.Code != "not_found" {
		t.Fatalf("another main's step 2: %v", err)
	}
}

func TestStep2WithoutStep1StopsTheAgentAndArchives(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	_, away, _ := workedOn(t, main, worker, paired.InstanceID, "Doing")
	var archived service.HandoffArchived
	if err := returnCall(t, main, paired.InstanceID, away.HandoffID, true, &archived); err != nil {
		t.Fatal(err)
	}
	archivedOnWorker(t, worker, main, away)
}

// dropper drops the worker's answers to the steps of a return: the worker
// does the step, then the connection closes with no answer, as a network
// that fails at the wrong moment does.
type dropper struct {
	mu   sync.Mutex
	drop map[string]int // "return" or "archive": how many answers to drop
}

func (d *dropper) dropNext(step string, n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.drop == nil {
		d.drop = map[string]int{}
	}
	d.drop[step] = n
}

func (d *dropper) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		d.mu.Lock()
		dropping := r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, service.HandoffCallPath+"/") && d.drop[step] > 0
		if dropping {
			d.drop[step]--
		}
		d.mu.Unlock()
		if !dropping {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(httptest.NewRecorder(), r)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	})
}

func TestAReturnWhoseStep2GoesUnansweredIsArchivedLater(t *testing.T) {
	ctx := context.Background()
	drops := &dropper{}
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", Handler: drops.handler})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away, note := workedOn(t, main, worker, paired.InstanceID, "Done")
	eventually(t, "done on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewDone })

	drops.dropNext("archive", 1)
	if _, err := main.Handoffs.Return(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	// The task is home, whatever happens to step 2.
	returnedHome(t, main, taskID, "Done", note)
	// The main sends it again and the worker archives its card.
	eventually(t, "the card archived on the worker", func() bool { return workerHandoff(t, worker, main, away.HandoffID).ClosedAt != nil })
	archivedOnWorker(t, worker, main, away)
	eventually(t, "nothing owed", func() bool {
		owes, err := main.Handoffs.Owes(ctx, paired.InstanceID)
		return err == nil && !owes
	})
	returnedHome(t, main, taskID, "Done", note)
}

func TestAReturnWhoseStep1GoesUnansweredIsFinishedOnceTheWorkerSaysItIsReturning(t *testing.T) {
	ctx := context.Background()
	drops := &dropper{}
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", Handler: drops.handler})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away, note := workedOn(t, main, worker, paired.InstanceID, "Done")
	eventually(t, "done on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewDone })

	drops.dropNext("return", 1)
	if _, err := main.Handoffs.Return(ctx, taskID); !errors.Is(err, service.ErrWorkerUnreachable) {
		t.Fatalf("a return with no answer: %v", err)
	}
	// The worker did step 1 and says so; the main finishes the return.
	eventually(t, "the task home", func() bool {
		a, err := main.Handoffs.Away(ctx, taskID)
		return err == nil && a == nil
	})
	eventually(t, "the card archived on the worker", func() bool { return workerHandoff(t, worker, main, away.HandoffID).ClosedAt != nil })
	returnedHome(t, main, taskID, "Done", note)
	archivedOnWorker(t, worker, main, away)
}

func TestAReturnLeftReturningByAMainThatStoppedIsFinishedOnConnect(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	taskID, away, note := workedOn(t, main, worker, paired.InstanceID, "Done")
	// The main made step 1 and stopped before applying its answer.
	var answer service.HandoffReturn
	if err := returnCall(t, main, paired.InstanceID, away.HandoffID, false, &answer); err != nil {
		t.Fatal(err)
	}
	// Started again, it connects, pulls, and finishes the return.
	main.StartConnections()
	eventually(t, "the task home", func() bool {
		a, err := main.Handoffs.Away(ctx, taskID)
		return err == nil && a == nil
	})
	eventually(t, "the card archived on the worker", func() bool { return workerHandoff(t, worker, main, away.HandoffID).ClosedAt != nil })
	returnedHome(t, main, taskID, "Done", note)
	archivedOnWorker(t, worker, main, away)
}

func TestRecallingAWorkingTaskBringsBackWhatThereIsWithANote(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	// A column the main does not have: the task lands in the one it left.
	if err := worker.Store.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Testing", RemoteStatuses: "[]", SortOrder: 9}); err != nil {
		t.Fatal(err)
	}
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away, note := workedOn(t, main, worker, paired.InstanceID, "Testing")
	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "working", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "working on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewWorking })

	// Return is for a task done there.
	if _, err := main.Handoffs.Return(ctx, taskID); !errors.Is(err, service.ErrReturnRefused) {
		t.Fatalf("returning a working task: %v", err)
	}
	back, err := main.Handoffs.Recall(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Recalled || back.Column != "Ready" {
		t.Fatalf("recalled %+v", back)
	}
	returnedHome(t, main, taskID, "Ready", note)
	archivedOnWorker(t, worker, main, away)
	var recalled []string
	for _, n := range notesOf(t, main, taskID) {
		if strings.HasPrefix(n.Body, "Recalled") {
			recalled = append(recalled, n.Body)
		}
	}
	if want := []string{"Recalled from vm-1, where its agent was working."}; !reflect.DeepEqual(recalled, want) {
		t.Fatalf("recall notes %q, want %q", recalled, want)
	}
}

func TestAFailedHandoffCanOnlyBeRecalled(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	worker.Repos.Fail("acme/widget", errors.New("clone failed: no space left"))
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, _ := localTask(t, main, "Fix it", "Details.", "")
	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	eventually(t, "failed on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewFailed })

	if _, err := main.Handoffs.Return(ctx, taskID); !errors.Is(err, service.ErrReturnRefused) || !strings.Contains(err.Error(), "recall it") {
		t.Fatalf("returning a failed handoff: %v", err)
	}
	back, err := main.Handoffs.Recall(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	// Its card was in the worker's Doing, which the main has too.
	if back.Column != "Doing" {
		t.Fatalf("recalled %+v", back)
	}
	notes := notesOf(t, main, taskID)
	if len(notes) != 1 || notes[0].Body != "Recalled from vm-1, where it had failed: clone failed: no space left." {
		t.Fatalf("notes %+v", notes)
	}
	if h := workerHandoff(t, worker, main, away.HandoffID); h.ClosedAt == nil {
		t.Fatalf("handoff %+v", h)
	}
}

func TestRecallingAPendingHandoffCancelsItsStart(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	worker.HoldStarts(true)
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix it", "Details.", "")
	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := main.Handoffs.Recall(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	// Started again, the worker starts nothing for it.
	worker.HoldStarts(false)
	worker.Restart()
	worker.Settle()
	if spawned := worker.Tmux.Spawned(); len(spawned) != 0 {
		t.Fatalf("an agent started for a recalled handoff: %+v", spawned)
	}
	if notes := notesOf(t, main, taskID); len(notes) != 1 || notes[0].Body != "Recalled from vm-1, where it was starting." {
		t.Fatalf("notes %+v", notes)
	}
	if h := workerHandoff(t, worker, main, away.HandoffID); h.ClosedAt == nil {
		t.Fatalf("handoff %+v", h)
	}
}

func TestReturnAllDoneReturnsEveryDoneTaskOfOneWorker(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	first, _, _ := workedOn(t, main, worker, paired.InstanceID, "Done")
	second, _, _ := workedOn(t, main, worker, paired.InstanceID, "Done")
	working, _, _ := workedOn(t, main, worker, paired.InstanceID, "Doing")
	eventually(t, "both done on the main", func() bool {
		return mainAway(t, main, first).View == service.AwayViewDone && mainAway(t, main, second).View == service.AwayViewDone
	})

	back, err := main.Handoffs.ReturnAllDone(ctx, paired.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, b := range back {
		ids = append(ids, b.TaskID)
	}
	if !reflect.DeepEqual(ids, []string{first, second}) {
		t.Fatalf("returned %v, want %v", ids, []string{first, second})
	}
	if a := mainAway(t, main, working); a.View == "" {
		t.Fatalf("the working task: %+v", a)
	}
}

func TestATaskHandedOffAgainAfterAReturnIsANewHandoffAndCard(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, first, _ := workedOn(t, main, worker, paired.InstanceID, "Done")
	eventually(t, "done on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewDone })
	if _, err := main.Handoffs.Return(ctx, taskID); err != nil {
		t.Fatal(err)
	}

	again, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	if again.HandoffID == first.HandoffID || again.WorkerTaskID == first.WorkerTaskID {
		t.Fatalf("handed off again as %+v, first %+v", again, first)
	}
	// The worker's notes that came back travel as the main's now, so a
	// second return brings none of them twice.
	notes, err := worker.Store.ListTaskNotes(ctx, again.WorkerTaskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if n.Source != service.NoteSourceMain {
			t.Fatalf("note %+v on the new card is not the main's", n)
		}
	}
	if err := worker.Board.MoveCard(ctx, again.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "done again", func() bool { return mainAway(t, main, taskID).View == service.AwayViewDone })
	before := len(notesOf(t, main, taskID))
	if _, err := main.Handoffs.Return(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	if after := len(notesOf(t, main, taskID)); after != before {
		t.Fatalf("the second return added %d notes", after-before)
	}
}
