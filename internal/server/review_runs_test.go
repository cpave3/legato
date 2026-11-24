package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"nhooyr.io/websocket"
)

type fakeReviewRuns struct {
	runID, stepID string
	err           error
}

func (f *fakeReviewRuns) AnotherPass(_ context.Context, runID, stepID string) error {
	f.runID, f.stepID = runID, stepID
	return f.err
}

type showingWorkflows struct{ forceWorkflowService }

func (showingWorkflows) Show(_ context.Context, runID string) (*store.WorkflowRunView, error) {
	return &store.WorkflowRunView{Run: store.WorkflowRun{ID: runID, CurrentStepID: "review"}}, nil
}

func TestRunAnotherPassReachesTheRunsReviewStep(t *testing.T) {
	runs := &fakeReviewRuns{}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(&showingWorkflows{}, "", "")
	s.SetReviewRuns(runs)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/review/another-pass", nil))
	if rec.Code != http.StatusOK || runs.runID != "wr-1" || runs.stepID != "review" {
		t.Fatalf("status=%d body=%s runs=%+v", rec.Code, rec.Body.String(), runs)
	}

	runs.err = errors.New("the review loop is not waiting for its author")
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/review/another-pass", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("refused: status=%d", rec.Code)
	}
}

func TestRunAnotherPassWithoutReviewLoopsIsUnavailable(t *testing.T) {
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(&showingWorkflows{}, "", "")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/review/another-pass", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAReviewLoopChangeReachesAnOpenRunPage(t *testing.T) {
	bus := events.New()
	srv := NewWithSwarm(nil, nil, nil, ":0", nil, bus, "")
	srv.StartWorkflowEvents()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = srv.Serve(ln) }()
	defer srv.Stop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s/ws", ln.Addr()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	time.Sleep(150 * time.Millisecond)
	bus.Publish(events.Event{Type: events.EventWorkflowChanged, Payload: events.WorkflowChangedPayload{RunID: "wr-1", StepID: "review", Kind: ReviewLoopChanged}})
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("no workflow_changed: %v", err)
		}
		var msg WSMessage
		_ = json.Unmarshal(data, &msg)
		if msg.Type == MsgWorkflowChanged {
			if msg.WorkflowRunID != "wr-1" {
				t.Fatalf("msg = %+v", msg)
			}
			return
		}
	}
}
