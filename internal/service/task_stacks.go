package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"regexp"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/stackgraph"
	"github.com/cpave3/legato/internal/engine/store"
)

// ErrStackNotImportable is a stack whose document st graph import could not
// use: a branch is not pushed, missing, misnamed or off the stack.
var ErrStackNotImportable = errors.New("the stack cannot be imported")

// TaskStackBranch is one branch of a recorded stack.
type TaskStackBranch struct {
	Name    string `json:"name"`
	Parent  string `json:"parent"`
	BaseSHA string `json:"base_sha"`
	HeadSHA string `json:"head_sha,omitempty"`
	// State is pushed, unpushed, missing or invalid.
	State   string `json:"state"`
	Problem string `json:"problem,omitempty"`
}

// TaskStack is the stack of branches Staccato keeps in one of a task's job
// clones, as Legato last read it.
type TaskStack struct {
	CloneID int64     `json:"clone_id"`
	Repo    string    `json:"repo"`
	Root    string    `json:"root"`
	ReadAt  time.Time `json:"read_at"`
	// Live is false once the clone's folder is removed; the record stays.
	Live bool `json:"live"`
	// Problem is why the stack could not be read, when it could not.
	Problem string `json:"problem,omitempty"`
	// Importable says whether st graph import can take its document; Reason
	// says why not.
	Importable bool   `json:"importable"`
	Reason     string `json:"reason,omitempty"`
	// DocumentName is the file the document downloads as, when importable.
	DocumentName string            `json:"document_name,omitempty"`
	Branches     []TaskStackBranch `json:"branches"`
}

// TaskStackDocument is a stack's st graph import document and the name to
// save it as.
type TaskStackDocument struct {
	Name string
	Data []byte
}

// TaskStackService records the stacks agents build in job clones, so the
// person can take a stack into their own checkout (st graph import), even
// after the clone is gone. It reads each clone's files and runs nothing in
// it (stackgraph).
type TaskStackService struct {
	store *store.Store
	now   func() time.Time
}

// NewTaskStackService returns the service over db; now defaults to the clock.
func NewTaskStackService(db *store.Store, now func() time.Time) *TaskStackService {
	if now == nil {
		now = time.Now
	}
	return &TaskStackService{store: db, now: now}
}

// Record reads a clone's stack and records it, replacing the record before.
// A clone with no stack has its record removed; one whose folder is gone
// keeps the record it has.
func (s *TaskStackService) Record(ctx context.Context, clone store.JobClone) error {
	if _, err := os.Lstat(clone.Path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	read, err := stackgraph.Read(clone.Path)
	if errors.Is(err, stackgraph.ErrNoStack) {
		return s.store.DeleteCloneStack(ctx, clone.ID)
	}
	rec := store.CloneStack{CloneID: clone.ID, Root: read.Root, Branches: "[]", ReadAt: s.now().UTC()}
	if err != nil {
		rec.Problem = err.Error()
	} else {
		branches := make([]TaskStackBranch, 0, len(read.Branches))
		for _, b := range read.Branches {
			branches = append(branches, TaskStackBranch{Name: b.Name, Parent: b.Parent, BaseSHA: b.BaseSHA, HeadSHA: b.HeadSHA, State: string(b.State), Problem: b.Problem})
		}
		data, err := json.Marshal(branches)
		if err != nil {
			return err
		}
		rec.Branches = string(data)
	}
	return s.store.PutCloneStack(ctx, rec)
}

// RecordTask records the stacks of every clone a task still works in.
func (s *TaskStackService) RecordTask(ctx context.Context, taskID string) error {
	clones, err := s.store.ListLiveTaskJobClones(ctx, taskID)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range clones {
		if err := s.Record(ctx, c); err != nil {
			errs = append(errs, fmt.Errorf("the stack of %s: %w", c.Path, err))
		}
	}
	return errors.Join(errs...)
}

// List returns a task's stacks, oldest clone first, reading those whose
// clones still exist again first.
func (s *TaskStackService) List(ctx context.Context, taskID string) ([]TaskStack, error) {
	if err := s.RecordTask(ctx, taskID); err != nil {
		log.Printf("stacks: %v", err)
	}
	recs, err := s.store.ListTaskStacks(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]TaskStack, 0, len(recs))
	for _, rec := range recs {
		st, _, err := view(rec, taskID)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// Document returns the st graph import document of the stack of one of a
// task's clones: store.ErrNotFound when the task has no stack there,
// ErrStackNotImportable (with why) when it cannot be imported.
func (s *TaskStackService) Document(ctx context.Context, taskID string, cloneID int64) (TaskStackDocument, error) {
	stacks, err := s.List(ctx, taskID)
	if err != nil {
		return TaskStackDocument{}, err
	}
	for _, st := range stacks {
		if st.CloneID != cloneID {
			continue
		}
		rec, err := s.store.CloneStackByID(ctx, cloneID)
		if err != nil {
			return TaskStackDocument{}, err
		}
		_, graph, err := view(rec, taskID)
		if err != nil {
			return TaskStackDocument{}, err
		}
		if !st.Importable {
			return TaskStackDocument{}, fmt.Errorf("%w: %s", ErrStackNotImportable, st.Reason)
		}
		data, err := graph.Document()
		if err != nil {
			return TaskStackDocument{}, fmt.Errorf("%w: %v", ErrStackNotImportable, err)
		}
		return TaskStackDocument{Name: st.DocumentName, Data: data}, nil
	}
	return TaskStackDocument{}, store.ErrNotFound
}

// StartEventLoop records a task's stacks whenever one of its agents ends,
// until ctx is done.
func (s *TaskStackService) StartEventLoop(ctx context.Context, bus *events.Bus) {
	if bus == nil {
		return
	}
	died := bus.Subscribe(events.EventAgentDied)
	go func() {
		defer bus.Unsubscribe(died)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-died:
				if !ok {
					return
				}
				p, ok := ev.Payload.(events.AgentDiedPayload)
				if !ok || p.TaskID == "" {
					continue
				}
				if err := s.RecordTask(ctx, p.TaskID); err != nil {
					log.Printf("stacks: %v", err)
				}
			}
		}
	}()
}

// view turns a record into what the person sees, and the stack it was read
// as, from which the document is made.
func view(rec store.CloneStack, taskID string) (TaskStack, stackgraph.Stack, error) {
	st := TaskStack{CloneID: rec.CloneID, Repo: rec.Repo, Root: rec.Root, ReadAt: rec.ReadAt, Live: rec.Live, Problem: rec.Problem}
	if err := json.Unmarshal([]byte(rec.Branches), &st.Branches); err != nil {
		return TaskStack{}, stackgraph.Stack{}, fmt.Errorf("the recorded stack of clone %d: %w", rec.CloneID, err)
	}
	graph := stackgraph.Stack{Root: rec.Root}
	for _, b := range st.Branches {
		graph.Branches = append(graph.Branches, stackgraph.Branch{Name: b.Name, Parent: b.Parent, BaseSHA: b.BaseSHA, HeadSHA: b.HeadSHA, State: stackgraph.State(b.State), Problem: b.Problem})
	}
	if rec.Problem != "" {
		st.Reason = rec.Problem
	} else {
		st.Importable, st.Reason = graph.Importable()
	}
	if st.Importable {
		st.DocumentName = documentName(taskID, rec.Repo)
	}
	return st, graph, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// documentName is the file a stack's document is saved as:
// <task>-<repository name>-stack.json.
func documentName(taskID, repo string) string {
	return unsafeName.ReplaceAllString(taskID+"-"+path.Base(repo), "-") + "-stack.json"
}
