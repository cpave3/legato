package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// hookingClaude is Claude Code as a tool that runs managed hooks.
type hookingClaude struct{ *hooks.ClaudeCodeAdapter }

func (hookingClaude) HooksBundle(root string, _ []string, _ map[string]string) (string, error) {
	dir := filepath.Join(root, "legato-hooks", "hooks")
	return dir, os.MkdirAll(dir, 0o755)
}

// spawnHooksServer is a server over a real agent service whose Claude Code
// runs managed hooks and whose Codex does not, with gate and picked in the
// managed-hooks folder.
func spawnHooksServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	db, err := store.New(filepath.Join(t.TempDir(), "spawn-hooks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.UpsertTask(context.Background(), store.Task{ID: "task-d", Title: "Dialog", Status: "Doing", Priority: "medium"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "managed-hooks")
	for _, name := range []string{"gate", "picked"} {
		dropManagedHook(t, dir, name, "name: "+name+"\ndescription: "+name+".\nevents:\n  stop: {command: ./stop.sh}\n")
	}
	hookSvc := service.NewManagedHookService(dir, db)
	claude := hookingClaude{hooks.NewClaudeCodeAdapter("legato")}
	agents := service.NewAgentService(db, &mockTmuxManager{}, t.TempDir(), service.AgentServiceOptions{
		Adapter:      claude,
		Adapters:     map[string]service.AIToolAdapter{"claude-code": claude, "codex": hooks.NewCodexAdapter("legato")},
		ManagedHooks: hookSvc, CapabilityFolders: service.CapabilityFolders{Root: t.TempDir()},
		DisablePeerAwareness: true,
	})
	srv := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "task-d"}}, agents, nil, ":0")
	srv.notifyTaskChanged = func(string) {}
	srv.SetManagedHookService(hookSvc)
	return srv
}

func TestTheSpawnDialogsRequestRecordsTheHookItUnticked(t *testing.T) {
	srv := spawnHooksServer(t)

	// The dialog learns which tools run managed hooks from the settings.
	var listed struct {
		SupportedTools []string `json:"supported_tools"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/settings/managed-hooks", "", &listed); status != http.StatusOK || !slices.Equal(listed.SupportedTools, []string{"claude-code"}) {
		t.Fatalf("settings: %d, supported tools %v; want only claude-code", status, listed.SupportedTools)
	}

	// What TaskAgentAction sends with picked unticked.
	body := `{"agent_kind":"claude-code","working_dir":"` + t.TempDir() + `","hooks_off":["picked"]}`
	if status := serveJSON(t, srv, http.MethodPost, "/api/tasks/task-d/agent", body, nil); status != http.StatusCreated {
		t.Fatalf("spawn: status %d", status)
	}
	var recorded service.AgentManagedHooks
	if status := serveJSON(t, srv, http.MethodGet, "/api/agents/task-d/managed-hooks", "", &recorded); status != http.StatusOK {
		t.Fatalf("managed hooks: status %d", status)
	}
	got := map[string]service.AgentManagedHook{}
	for _, hook := range recorded.Hooks {
		got[hook.Name] = hook
	}
	if !got["gate"].On || !slices.Equal(got["picked"].Off, []string{"off:spawn"}) || !slices.Equal(got["picked"].ChosenOff, []string{"spawn"}) {
		t.Fatalf("recorded %+v, want gate on and picked off:spawn chosen by spawn", recorded.Hooks)
	}
}
