package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type fakeTaskStacks struct {
	stacks []service.TaskStack
	doc    service.TaskStackDocument
	docErr error
	asked  []string
}

func (f *fakeTaskStacks) List(_ context.Context, taskID string) ([]service.TaskStack, error) {
	f.asked = append(f.asked, taskID)
	return f.stacks, nil
}

func (f *fakeTaskStacks) Document(_ context.Context, taskID string, cloneID int64) (service.TaskStackDocument, error) {
	f.asked = append(f.asked, fmt.Sprintf("%s/%d", taskID, cloneID))
	return f.doc, f.docErr
}

func TestATasksStacksAreListed(t *testing.T) {
	stacks := &fakeTaskStacks{stacks: []service.TaskStack{{CloneID: 3, Repo: "acme/widget", Root: "main", Live: true, Importable: true,
		Branches: []service.TaskStackBranch{{Name: "legato/t1-a", Parent: "main", State: "pushed"}}}}}
	srv := New(nil, nil, nil, ":0")
	srv.SetTaskStackService(stacks)

	var listed struct {
		Stacks []service.TaskStack `json:"stacks"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/tasks/t1/stacks", "", &listed); status != http.StatusOK ||
		len(listed.Stacks) != 1 || listed.Stacks[0].Branches[0].Name != "legato/t1-a" || stacks.asked[0] != "t1" {
		t.Fatalf("status %d listed %+v asked %v", status, listed, stacks.asked)
	}
	stacks.stacks = nil
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks/t1/stacks", nil))
	if !strings.Contains(rec.Body.String(), `"stacks":[]`) {
		t.Fatalf("no stacks = %s", rec.Body.String())
	}
}

func TestAStacksDocumentIsDownloaded(t *testing.T) {
	stacks := &fakeTaskStacks{doc: service.TaskStackDocument{Name: "t1-widget-stack.json", Data: []byte(`{"format": 1}`)}}
	srv := New(nil, nil, nil, ":0")
	srv.SetTaskStackService(stacks)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks/t1/stacks/3/document", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != `{"format": 1}` || stacks.asked[0] != "t1/3" {
		t.Fatalf("status %d body %q asked %v", rec.Code, rec.Body.String(), stacks.asked)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="t1-widget-stack.json"` {
		t.Errorf("Content-Disposition = %q", got)
	}

	for _, tc := range []struct {
		err    error
		target string
		status int
		says   string
	}{
		{fmt.Errorf("%w: not pushed: legato/t1-b", service.ErrStackNotImportable), "/api/tasks/t1/stacks/3/document", http.StatusConflict, "legato/t1-b"},
		{store.ErrNotFound, "/api/tasks/t1/stacks/4/document", http.StatusNotFound, ""},
		{nil, "/api/tasks/t1/stacks/x/document", http.StatusBadRequest, ""},
	} {
		stacks.docErr = tc.err
		var body struct {
			Error string `json:"error"`
		}
		if status := serveJSON(t, srv, http.MethodGet, tc.target, "", &body); status != tc.status || !strings.Contains(body.Error, tc.says) {
			t.Errorf("%s with %v: status %d %q", tc.target, tc.err, status, body.Error)
		}
	}
}

func TestStacksWithoutTheService(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	if status := serveJSON(t, srv, http.MethodGet, "/api/tasks/t1/stacks", "", nil); status != http.StatusServiceUnavailable {
		t.Fatalf("status %d", status)
	}
	srv.SetTaskStackService(&fakeTaskStacks{})
	if status := serveJSON(t, srv, http.MethodPost, "/api/tasks/t1/stacks", "", nil); status != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status %d", status)
	}
}
