package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type recordingReflectionWorkflow struct {
	runID, stepID, attemptID string
	values                   map[string]string
	err                      error
}

func (f *recordingReflectionWorkflow) Complete(_ context.Context, runID, stepID, attemptID string, values, _ map[string]string) (*store.WorkflowRunView, error) {
	f.runID, f.stepID, f.attemptID, f.values = runID, stepID, attemptID, values
	if f.err != nil {
		return nil, f.err
	}
	return &store.WorkflowRunView{}, nil
}

func newLearningAPITestServer(t *testing.T) (*Server, *service.LearningService, *store.Store) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "learning.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	learning := service.NewLearningService(db, nil)
	srv := New(nil, nil, nil, "")
	srv.SetLearningService(learning)
	srv.SetReflectionWorkflowService(&recordingReflectionWorkflow{})
	return srv, learning, db
}

func learningRequest(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestLearningAPIRecordListShowSearchAndContext(t *testing.T) {
	srv, _, _ := newLearningAPITestServer(t)
	record := learningRequest(t, srv, http.MethodPost, "/api/learnings", service.LearningRecordInput{
		Scope: "user", Kind: "preference", Claim: "Prefer table tests", SourceKind: "explicit", SourceRef: "api-test", Author: "user",
	})
	if record.Code != http.StatusCreated {
		t.Fatalf("record status=%d body=%s", record.Code, record.Body.String())
	}
	var view service.LearningView
	if err := json.Unmarshal(record.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	accepted := learningRequest(t, srv, http.MethodPost, "/api/learnings/"+view.Learning.ID+"/accept", map[string]any{"revision": 1})
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept status=%d body=%s", accepted.Code, accepted.Body.String())
	}

	for _, path := range []string{"/api/learnings", "/api/learnings/search?q=table", "/api/learnings/context"} {
		rec := learningRequest(t, srv, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var items []store.Learning
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ID != view.Learning.ID {
			t.Fatalf("GET %s items=%+v", path, items)
		}
	}
	show := learningRequest(t, srv, http.MethodGet, "/api/learnings/"+view.Learning.ID, nil)
	if show.Code != http.StatusOK {
		t.Fatalf("show status=%d body=%s", show.Code, show.Body.String())
	}
	var shown service.LearningView
	if err := json.Unmarshal(show.Body.Bytes(), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.Incorporations == nil {
		t.Fatal("show must return an incorporation history array")
	}
}

func TestLearningAPIDecisionsIncorporationAndRevisionConflict(t *testing.T) {
	srv, learning, _ := newLearningAPITestServer(t)
	view, err := learning.Record(context.Background(), service.LearningRecordInput{Scope: "user", Kind: "fact", Claim: "original", SourceKind: "explicit", SourceRef: "decision", Author: "user"})
	if err != nil {
		t.Fatal(err)
	}

	accept := learningRequest(t, srv, http.MethodPost, "/api/learnings/"+view.Learning.ID+"/accept", map[string]any{"revision": 1})
	if accept.Code != http.StatusOK {
		t.Fatalf("accept status=%d body=%s", accept.Code, accept.Body.String())
	}
	conflict := learningRequest(t, srv, http.MethodPost, "/api/learnings/"+view.Learning.ID+"/reject", map[string]any{"revision": 1})
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	propose := learningRequest(t, srv, http.MethodPost, "/api/learnings/"+view.Learning.ID+"/incorporations", map[string]any{"revision": 2, "destination": "docs", "target": "README.md", "action": "add"})
	if propose.Code != http.StatusCreated {
		t.Fatalf("propose status=%d body=%s", propose.Code, propose.Body.String())
	}
	var inc store.LearningIncorporation
	if err := json.Unmarshal(propose.Body.Bytes(), &inc); err != nil {
		t.Fatal(err)
	}
	submit := learningRequest(t, srv, http.MethodPost, "/api/learning-incorporations/"+inc.ID+"/submit", map[string]any{"revision": 1, "references": []string{"README.md:1"}, "verification_kind": "manual"})
	if submit.Code != http.StatusOK {
		t.Fatalf("submit status=%d body=%s", submit.Code, submit.Body.String())
	}
	verify := learningRequest(t, srv, http.MethodPost, "/api/learning-incorporations/"+inc.ID+"/verify", map[string]any{"revision": 2, "passed": true, "result": "ok"})
	if verify.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verify.Code, verify.Body.String())
	}
	show := learningRequest(t, srv, http.MethodGet, "/api/learnings/"+view.Learning.ID, nil)
	var shown service.LearningView
	if err := json.Unmarshal(show.Body.Bytes(), &shown); err != nil {
		t.Fatal(err)
	}
	if len(shown.Incorporations) != 1 || shown.Incorporations[0].ID != inc.ID || shown.Incorporations[0].Status != "verified" {
		t.Fatalf("incorporation history=%+v", shown.Incorporations)
	}
}

func stringPtr(value string) *string { return &value }

func TestLearningAPIPromptActivationAndPreview(t *testing.T) {
	srv, learning, db := newLearningAPITestServer(t)
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "preview-task", Title: "preview", Status: "Backlog", CodeContext: stringPtr(`{"repo":"acme/widget"}`)}); err != nil {
		t.Fatal(err)
	}
	view, err := learning.Record(ctx, service.LearningRecordInput{Scope: "user", Kind: "preference", Claim: "Prefer focused diffs.", SourceKind: "test", SourceRef: "activation", Author: "user"})
	if err != nil {
		t.Fatal(err)
	}
	view, err = learning.Accept(ctx, view.Learning.ID, view.Learning.Revision, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	preview := learningRequest(t, srv, http.MethodGet, "/api/learnings/prompt-preview?task_id=preview-task", nil)
	if preview.Code != http.StatusOK || strings.Contains(preview.Body.String(), "Prefer focused diffs") {
		t.Fatalf("disabled preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	enabled := learningRequest(t, srv, http.MethodPost, "/api/learnings/"+view.Learning.ID+"/prompt", map[string]any{"revision": view.Learning.Revision, "enabled": true})
	if enabled.Code != http.StatusOK {
		t.Fatalf("activation status=%d body=%s", enabled.Code, enabled.Body.String())
	}
	preview = learningRequest(t, srv, http.MethodGet, "/api/learnings/prompt-preview?task_id=preview-task", nil)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "Prefer focused diffs") {
		t.Fatalf("enabled preview status=%d body=%s", preview.Code, preview.Body.String())
	}
}

func TestLearningAPIReflectionManifestValidationAndAuth(t *testing.T) {
	srv, learning, db := newLearningAPITestServer(t)
	reflection, err := learning.CreateReflectionRequest(context.Background(), service.CreateReflectionRequestInput{WorkflowRunID: "run-1", StepID: "step-1", AttemptID: "attempt-1", TaskID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	workflow := &recordingReflectionWorkflow{}
	srv.SetReflectionWorkflowService(workflow)
	submit := learningRequest(t, srv, http.MethodPost, "/api/learning-reflections/"+reflection.ID+"/submit", map[string]any{"schema_version": 1, "observations": []map[string]any{{"kind": "fact", "content": "A durable fact"}}})
	if submit.Code != http.StatusOK {
		t.Fatalf("reflection status=%d body=%s", submit.Code, submit.Body.String())
	}
	if workflow.runID != "run-1" || workflow.stepID != "step-1" || workflow.attemptID != "attempt-1" {
		t.Fatalf("workflow completion did not use exact reflection attempt: %+v", workflow)
	}
	if _, ok := workflow.values["evidence_count"]; ok {
		t.Fatalf("undeclared completion output present: %#v", workflow.values)
	}
	bad := learningRequest(t, srv, http.MethodPost, "/api/learnings", map[string]any{"scope": "invalid"})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("validation status=%d body=%s", bad.Code, bad.Body.String())
	}

	manifest := store.LearningPromptManifest{ID: "lpm-api", TaskID: "task-1", Adapter: "test", IncludedJSON: "[]", ExcludedJSON: "[]", RenderedDigest: "sha256:test"}
	if err := db.CreateLearningPromptManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	show := learningRequest(t, srv, http.MethodGet, "/api/learning-prompt-manifests/lpm-api", nil)
	if show.Code != http.StatusOK {
		t.Fatalf("manifest status=%d body=%s", show.Code, show.Body.String())
	}

	srv.SetAuthToken("secret")
	unauthorized := learningRequest(t, srv, http.MethodGet, "/api/learnings", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/learnings", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLearningAPIReflectionCompletionFailureIsRecoverableAfterPersistence(t *testing.T) {
	srv, learning, _ := newLearningAPITestServer(t)
	reflection, err := learning.CreateReflectionRequest(context.Background(), service.CreateReflectionRequestInput{WorkflowRunID: "run", StepID: "step", AttemptID: "attempt", TaskID: "task"})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetReflectionWorkflowService(&recordingReflectionWorkflow{err: errors.New("workflow unavailable")})
	body := map[string]any{"schema_version": 1, "observations": []map[string]any{{"kind": "fact", "content": "persist me"}}}
	rec := learningRequest(t, srv, http.MethodPost, "/api/learning-reflections/"+reflection.ID+"/submit", body)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := learning.SubmitReflectionJSON(context.Background(), reflection.ID, mustJSON(t, body))
	if err != nil || !stored.Replayed || len(stored.Evidence) != 1 {
		t.Fatalf("persisted replay=%+v err=%v", stored, err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLearningErrorClassification(t *testing.T) {
	srv := New(nil, nil, nil, "")
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"validation", service.LearningValidationError("bad input"), http.StatusBadRequest},
		{"conflict", service.ErrLearningConflict, http.StatusConflict},
		{"not found", store.ErrNotFound, http.StatusNotFound},
		{"internal", errors.New("database failed"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.learningError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestLearningVerifyTargetAndDriftPublicPaths(t *testing.T) {
	srv, learning, _ := newLearningAPITestServer(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("durable"), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := learning.Record(context.Background(), service.LearningRecordInput{Scope: "user", Kind: "fact", Claim: "durable", SourceKind: "explicit", SourceRef: "paths", Author: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = learning.Accept(context.Background(), view.Learning.ID, 1, "", "", ""); err != nil {
		t.Fatal(err)
	}
	inc, err := learning.ProposeIncorporation(context.Background(), view.Learning.ID, 2, "documentation", "README.md", "add")
	if err != nil {
		t.Fatal(err)
	}
	inc, err = learning.SubmitIncorporation(context.Background(), inc.ID, 1, []string{"README.md"}, "content", "durable")
	if err != nil {
		t.Fatal(err)
	}

	unsafeView, err := learning.Record(context.Background(), service.LearningRecordInput{Scope: "user", Kind: "fact", Claim: "unsafe", SourceKind: "explicit", SourceRef: "unsafe-path", Author: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = learning.Accept(context.Background(), unsafeView.Learning.ID, 1, "", "", ""); err != nil {
		t.Fatal(err)
	}
	unsafeInc, err := learning.ProposeIncorporation(context.Background(), unsafeView.Learning.ID, 2, "documentation", "../outside.md", "add")
	if err != nil {
		t.Fatal(err)
	}
	unsafeInc, err = learning.SubmitIncorporation(context.Background(), unsafeInc.ID, 1, []string{"outside"}, "content", "unsafe")
	if err != nil {
		t.Fatal(err)
	}
	unsafe := learningRequest(t, srv, http.MethodPost, "/api/learning-incorporations/"+unsafeInc.ID+"/verify-target", map[string]any{"revision": unsafeInc.Revision, "repository_root": root})
	if unsafe.Code != http.StatusBadRequest {
		t.Fatalf("unsafe target status=%d body=%s", unsafe.Code, unsafe.Body.String())
	}

	verified := learningRequest(t, srv, http.MethodPost, "/api/learning-incorporations/"+inc.ID+"/verify-target", map[string]any{"revision": inc.Revision, "repository_root": root})
	if verified.Code != http.StatusOK {
		t.Fatalf("verify-target status=%d body=%s", verified.Code, verified.Body.String())
	}
	drift := learningRequest(t, srv, http.MethodPost, "/api/learning-incorporations/drift", map[string]any{"repository_root": root})
	if drift.Code != http.StatusOK {
		t.Fatalf("drift status=%d body=%s", drift.Code, drift.Body.String())
	}
	var results []service.IncorporationDriftResult
	if err := json.Unmarshal(drift.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "unchanged" {
		t.Fatalf("results=%+v", results)
	}
	badDrift := learningRequest(t, srv, http.MethodPost, "/api/learning-incorporations/drift", map[string]any{"repository_root": ""})
	if badDrift.Code != http.StatusBadRequest {
		t.Fatalf("unsafe drift status=%d body=%s", badDrift.Code, badDrift.Body.String())
	}
}

func TestStartLearningEventsBroadcastsLearningChanged(t *testing.T) {
	bus := events.New()
	srv := NewWithSwarm(nil, nil, nil, "", nil, bus, "")
	messages := make(chan WSMessage, 1)
	client := &wsClient{ctx: context.Background(), sendFn: func(message WSMessage) { messages <- message }}
	srv.hub.add(client)
	srv.StartLearningEvents()
	bus.Publish(events.Event{Type: events.EventLearningChanged, Payload: events.LearningChangedPayload{LearningID: "learn-1", Kind: "accepted"}})
	select {
	case message := <-messages:
		if message.Type != MsgLearningChanged || message.LearningID != "learn-1" || message.Kind != "accepted" {
			t.Fatalf("message=%+v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for learning websocket message")
	}
}
