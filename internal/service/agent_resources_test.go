package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpawnAgent_ResourcesAreListedInTheLaunchPrompt(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "My Vault")
	templates := filepath.Join(root, "templates")
	for _, dir := range []string{vault, templates} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	resources := []Resource{{Name: "Obsidian vault", Paths: []ResourcePath{
		{Path: vault, Access: "write"},
		{Path: templates, Access: "read"},
		{Path: filepath.Join(root, "gone"), Access: "read"},
	}}}

	prompt, ok := spawnAndReadPrompt(t, &fakeAdapter{name: "plain"}, "task-1", AgentSpawnOptions{Resources: resources})

	if !ok {
		t.Fatal("no launch prompt was written, so the agent never learns of its resources")
	}
	for _, want := range []string{"## Resources", "Obsidian vault", "`" + vault + "` (read and write)", "`" + templates + "` (read only)"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("launch prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "gone") {
		t.Errorf("a path that does not exist was listed:\n%s", prompt)
	}
}

// grantingAdapter stands in for an agent tool that can be granted files and
// folders on its command line, as Chimera can.
type grantingAdapter struct {
	*fakeAdapter
}

func (grantingAdapter) PathGrantFlags(read, write []string) string {
	return " READ[" + strings.Join(read, "|") + "] WRITE[" + strings.Join(write, "|") + "]"
}

func TestSpawnAgent_ResourcesAreGrantedOnTheCommandLineOfAToolThatTakesThem(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "My Vault")
	specs := filepath.Join(root, "specs")
	for _, dir := range []string{vault, specs} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s := newResourceStore(t)
	mt := newMockTmux()
	adapter := grantingAdapter{&fakeAdapter{name: "chimera-like"}}
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter})
	createTask(t, s, "task-1")

	err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Resources: []Resource{
		{Name: "Vault", Paths: []ResourcePath{{Path: vault, Access: "read"}, {Path: specs, Access: "read"}}},
		{Name: "Writing", Paths: []ResourcePath{{Path: vault, Access: "write"}, {Path: filepath.Join(root, "gone"), Access: "write"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	lines := mt.sentLines["legato-task-1"]
	if len(lines) == 0 {
		t.Fatal("nothing was launched")
	}
	want := " READ[" + specs + "] WRITE[" + vault + "]"
	if !strings.HasSuffix(lines[0], want) {
		t.Fatalf("launch command = %q, want it to end with %q", lines[0], want)
	}

	session, err := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	names, err := s.ListExecutionResources(context.Background(), session.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "Vault,Writing" {
		t.Fatalf("execution resources = %v", names)
	}
}

func TestAttachingResourcesToARunningNativeSessionGrantsThemAndTellsTheAgent(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "My Vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	type grant struct{ Kind, Path string }
	var grants []grant
	var appended []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sessions/sess-1/paths":
			var body grant
			json.NewDecoder(r.Body).Decode(&body)
			grants = append(grants, body)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/v1/sessions/sess-1/messages" && r.URL.Query().Get("append") == "true":
			var body struct{ Content string }
			json.NewDecoder(r.Body).Decode(&body)
			appended = append(appended, body.Content)
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := newResourceStore(t)
	ctx := context.Background()
	svc := NewAgentService(s, newMockTmux(), t.TempDir(), AgentServiceOptions{Adapter: &fakeAdapter{name: "chimera"}})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscovery(ctx, "task-1", "sess-1", srv.URL); err != nil {
		t.Fatal(err)
	}

	attacher := svc.(ResourceAttacher)
	if err := attacher.AttachResources(ctx, "task-1", []Resource{{Name: "Vault", Paths: []ResourcePath{{Path: vault, Access: "write"}}}}); err != nil {
		t.Fatal(err)
	}

	if len(grants) != 1 || grants[0] != (grant{Kind: "write", Path: vault}) {
		t.Fatalf("grants = %+v", grants)
	}
	if len(appended) != 1 || !strings.HasPrefix(appended[0], "[legato resources]") || !strings.Contains(appended[0], "`"+vault+"` (read and write)") {
		t.Fatalf("appended = %q", appended)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	names, _ := s.ListExecutionResources(ctx, session.ExecutionID)
	if strings.Join(names, ",") != "Vault" {
		t.Fatalf("execution resources = %v", names)
	}
}

func TestAttachingResourcesNeedsANativeSession(t *testing.T) {
	s := newResourceStore(t)
	ctx := context.Background()
	svc := NewAgentService(s, newMockTmux(), t.TempDir(), AgentServiceOptions{Adapter: &fakeAdapter{name: "codex"}})
	createTask(t, s, "task-1")
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}

	err := svc.(ResourceAttacher).AttachResources(ctx, "task-1", []Resource{{Name: "Vault", Paths: []ResourcePath{{Path: t.TempDir(), Access: "read"}}}})
	if !errors.Is(err, ErrNoNativeEndpoint) {
		t.Fatalf("err = %v", err)
	}
}
