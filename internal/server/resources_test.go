package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type fakeAttacher struct {
	taskID    string
	resources []service.Resource
	attached  []string
	err       error
}

func (f *fakeAttacher) AttachResources(_ context.Context, taskID string, resources []service.Resource) error {
	if f.err != nil {
		return f.err
	}
	f.taskID, f.resources = taskID, resources
	for _, resource := range resources {
		f.attached = append(f.attached, resource.Name)
	}
	return nil
}

func (f *fakeAttacher) AttachedResources(context.Context, string) ([]string, error) {
	return f.attached, nil
}

func resourceServer(t *testing.T) (*Server, string, *fakeAttacher) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "My Vault"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(filepath.Join(t.TempDir(), "resources.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewWithSwarm(nil, nil, nil, ":0", nil, nil, home)
	resources := service.NewResourceService(db, []service.Resource{{Name: "Specs", Paths: []service.ResourcePath{{Path: "/specs", Access: "read"}}}}, home)
	attacher := &fakeAttacher{}
	resources.SetAttacher(attacher)
	srv.SetResourceService(resources)
	return srv, home, attacher
}

func TestResourcesCanBeSavedListedChangedAndRemoved(t *testing.T) {
	srv, home, _ := resourceServer(t)

	var created service.Resource
	if status := serveJSON(t, srv, http.MethodPost, "/api/resources", `{"name":"Vault","paths":[{"path":"~/My Vault","access":"write"}]}`, &created); status != http.StatusCreated {
		t.Fatalf("create status = %d", status)
	}
	if created.ID == 0 || created.Paths[0].Path != filepath.Join(home, "My Vault") {
		t.Fatalf("created = %+v", created)
	}

	var listed struct{ Resources []service.Resource }
	serveJSON(t, srv, http.MethodGet, "/api/resources", "", &listed)
	if len(listed.Resources) != 2 || !listed.Resources[0].Configured || listed.Resources[1].Name != "Vault" {
		t.Fatalf("listed = %+v", listed)
	}

	var changed service.Resource
	target := "/api/resources/" + itoa(created.ID)
	if status := serveJSON(t, srv, http.MethodPut, target, `{"name":"Notes","paths":[{"path":"~/My Vault"}]}`, &changed); status != http.StatusOK {
		t.Fatalf("update status = %d", status)
	}
	if changed.Name != "Notes" || changed.Paths[0].Access != "read" {
		t.Fatalf("changed = %+v", changed)
	}

	var failure struct{ Error string }
	if status := serveJSON(t, srv, http.MethodPost, "/api/resources", `{"name":"Bad","paths":[{"path":"/nowhere"}]}`, &failure); status != http.StatusBadRequest || !strings.Contains(failure.Error, "does not exist") {
		t.Fatalf("invalid = %d %q", status, failure.Error)
	}

	if status := serveJSON(t, srv, http.MethodDelete, target, "", nil); status != http.StatusNoContent {
		t.Fatalf("delete status = %d", status)
	}
	if status := serveJSON(t, srv, http.MethodDelete, target, "", nil); status != http.StatusNotFound {
		t.Fatalf("second delete status = %d", status)
	}
}

func TestResourcesAreAttachedToARunningAgentByName(t *testing.T) {
	srv, _, attacher := resourceServer(t)

	var answer struct{ Attached []string }
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/task-1/resources", `{"names":["specs"]}`, &answer); status != http.StatusOK {
		t.Fatalf("attach status = %d", status)
	}
	if attacher.taskID != "task-1" || len(answer.Attached) != 1 || answer.Attached[0] != "Specs" {
		t.Fatalf("attached %+v, answer %+v", attacher, answer)
	}

	var listed struct{ Attached []string }
	serveJSON(t, srv, http.MethodGet, "/api/agents/task-1/resources", "", &listed)
	if len(listed.Attached) != 1 {
		t.Fatalf("listed = %+v", listed)
	}

	attacher.err = service.ErrNoNativeEndpoint
	var failure struct{ Error string }
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/task-1/resources", `{"names":["Specs"]}`, &failure); status != http.StatusConflict {
		t.Fatalf("no endpoint status = %d", status)
	}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestATaskAgentIsStartedWithTheResourcesNamed(t *testing.T) {
	agents := &taskAgentFake{}
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}
	srv := New(board, agents, nil, ":0")
	srv.workDir = "/default"
	srv.SetResourceService(service.NewResourceService(nil, []service.Resource{{Name: "Vault", Paths: []service.ResourcePath{{Path: "/home/me/My Vault", Access: "write"}}}}, "/home/me"))

	if status := serveJSON(t, srv, http.MethodPost, "/api/tasks/T-1/agent", `{"resources":["nope"]}`, nil); status != http.StatusBadRequest {
		t.Fatalf("unknown resource status = %d", status)
	}
	if agents.spawned != "" {
		t.Fatal("an agent was started with a resource that does not exist")
	}

	if status := serveJSON(t, srv, http.MethodPost, "/api/tasks/T-1/agent", `{"resources":["vault"]}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d", status)
	}
	if len(agents.opts.Resources) != 1 || agents.opts.Resources[0].Paths[0].Path != "/home/me/My Vault" {
		t.Fatalf("resources = %+v", agents.opts.Resources)
	}
}
