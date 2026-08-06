package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// mockTmux is a TmuxManager test double whose internal maps are guarded by a
// mutex so that goroutines spawned by SpawnAgent (e.g. the deferred brief
// kickoff send-keys) don't race against the main test goroutine reading the
// same maps. Test code MUST use the accessor helpers (sessionAlive, sentLinesFor,
// envVarsFor, …) instead of touching the maps directly.
type mockTmux struct {
	mu               sync.Mutex
	sessions         map[string]bool
	captures         map[string]string
	envVars          map[string]map[string]string // session -> key -> value
	spawnDims        map[string][2]int            // session -> [width, height]
	options          map[string]map[string]string // session -> key -> value
	paneCommands     map[string]string            // session -> command
	paneCommandsErr  error
	spawnErr         error
	sentLines        map[string][]string // session -> ordered list of SendKeysLine payloads
	sendLineErr      error
	sentMultiline    map[string][]string // session -> ordered list of SendKeysMultiline payloads
	sendMultilineErr map[string]error
	sentKeys         map[string][]string // session -> ordered list of SendKey payloads
	sentRaw          map[string][]string // session -> ordered list of SendKeys (literal text) payloads
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		sessions:         make(map[string]bool),
		captures:         make(map[string]string),
		envVars:          make(map[string]map[string]string),
		spawnDims:        make(map[string][2]int),
		options:          make(map[string]map[string]string),
		sentLines:        make(map[string][]string),
		sentMultiline:    make(map[string][]string),
		sendMultilineErr: make(map[string]error),
		sentKeys:         make(map[string][]string),
		sentRaw:          make(map[string][]string),
	}
}

func (m *mockTmux) Spawn(name, workDir string, width, height int, envVars ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.spawnErr != nil {
		return m.spawnErr
	}
	if m.sessions[name] {
		return fmt.Errorf("session %s already exists", name)
	}
	m.sessions[name] = true
	m.spawnDims[name] = [2]int{width, height}
	if len(envVars) > 0 {
		if m.envVars[name] == nil {
			m.envVars[name] = make(map[string]string)
		}
		for _, e := range envVars {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				m.envVars[name][parts[0]] = parts[1]
			}
		}
	}
	return nil
}

func (m *mockTmux) SessionEnvironment(name string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[name] {
		return nil, fmt.Errorf("session %s not found", name)
	}
	env := make([]string, 0, len(m.envVars[name]))
	for key, value := range m.envVars[name] {
		env = append(env, key+"="+value)
	}
	return env, nil
}

func (m *mockTmux) Kill(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
	return nil
}

func (m *mockTmux) Capture(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[name] {
		return "", fmt.Errorf("session %s not found", name)
	}
	if c, ok := m.captures[name]; ok {
		return c, nil
	}
	return "$ ", nil
}

func (m *mockTmux) CaptureWithEscapes(name string) (string, error) {
	return m.Capture(name)
}

func (m *mockTmux) Attach(name string) *exec.Cmd {
	return exec.Command("echo", "attached to", name)
}

func (m *mockTmux) ListSessions() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []string
	for name := range m.sessions {
		result = append(result, name)
	}
	return result, nil
}

func (m *mockTmux) IsAlive(name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[name], nil
}

func (m *mockTmux) PaneCommands() (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.paneCommands != nil {
		return m.paneCommands, m.paneCommandsErr
	}
	return map[string]string{}, m.paneCommandsErr
}

func (m *mockTmux) SetOption(sessionName, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[sessionName] {
		return fmt.Errorf("session %s not found", sessionName)
	}
	if m.options[sessionName] == nil {
		m.options[sessionName] = make(map[string]string)
	}
	m.options[sessionName][key] = value
	return nil
}

func (m *mockTmux) SendKeys(name, keys string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[name] {
		return fmt.Errorf("session %s not found", name)
	}
	m.sentRaw[name] = append(m.sentRaw[name], keys)
	return nil
}

func (m *mockTmux) SendKey(name, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[name] {
		return fmt.Errorf("session %s not found", name)
	}
	m.sentKeys[name] = append(m.sentKeys[name], key)
	return nil
}

func (m *mockTmux) SendKeysLine(name, line string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[name] {
		return fmt.Errorf("session %s not found", name)
	}
	if m.sendLineErr != nil {
		return m.sendLineErr
	}
	m.sentLines[name] = append(m.sentLines[name], line)
	return nil
}

func (m *mockTmux) SendKeysMultiline(name, payload string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[name] {
		return fmt.Errorf("session %s not found", name)
	}
	if err := m.sendMultilineErr[name]; err != nil {
		return err
	}
	m.sentMultiline[name] = append(m.sentMultiline[name], payload)
	return nil
}

func (m *mockTmux) SendKeysShellCommand(name, command string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[name] {
		return fmt.Errorf("session %s not found", name)
	}
	// Mock treats this the same as SendKeysLine for simplicity — the
	// real distinction (split call + delay) is a tmux-level concern that
	// doesn't matter for service-layer tests.
	m.sentLines[name] = append(m.sentLines[name], command)
	return nil
}

func (m *mockTmux) PipeOutput(name string) (io.Reader, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[name] {
		return nil, nil, fmt.Errorf("session %s not found", name)
	}
	return strings.NewReader(""), func() {}, nil
}

func (m *mockTmux) SetEnv(sessionName, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[sessionName] {
		return fmt.Errorf("session %s not found", sessionName)
	}
	if m.envVars[sessionName] == nil {
		m.envVars[sessionName] = make(map[string]string)
	}
	m.envVars[sessionName][key] = value
	return nil
}

// --- Test-side accessors. All take the lock and return defensive copies so
// goroutines spawned by SpawnAgent don't race with reads from the test body.

func (m *mockTmux) sessionAlive(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[name]
}

func (m *mockTmux) markSessionDead(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
}

func (m *mockTmux) setCapture(name, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.captures[name] = value
}

func (m *mockTmux) sentLinesFor(name string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.sentLines[name]))
	copy(out, m.sentLines[name])
	return out
}

func (m *mockTmux) envVarsFor(name string) map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.envVars[name]
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (m *mockTmux) sentKeysFor(name string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.sentKeys[name]))
	copy(out, m.sentKeys[name])
	return out
}

func (m *mockTmux) sentRawFor(name string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.sentRaw[name]))
	copy(out, m.sentRaw[name])
	return out
}

func (m *mockTmux) optionsFor(name string) map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.options[name]
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (m *mockTmux) spawnDimsFor(name string) [2]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spawnDims[name]
}

func newTestAgentService(t *testing.T) (AgentService, *store.Store, *mockTmux) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir())
	return svc, s, mt
}

func TestSendPeerMessageQueuesForBusyChimeraAndAddsReplyFooter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTask(t, s, "recipient")
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir())
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "recipient", 80, 24); err != nil {
		t.Fatal(err)
	}

	var gotPath, gotContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var payload struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		gotContent = payload.Content
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	if err := s.UpdateChimeraDiscovery(ctx, "recipient", "chimera-recipient", server.URL); err != nil {
		t.Fatal(err)
	}

	if err := svc.SendPeerMessage(ctx, "sender", "recipient", "Can you avoid agent.go?"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/sessions/chimera-recipient/messages?append=true" {
		t.Fatalf("request URI = %q, want queued append", gotPath)
	}
	for _, want := range []string{"Can you avoid agent.go?", "Agent-to-agent message from `sender`", `legato agent message sender "<reply>"`} {
		if !strings.Contains(gotContent, want) {
			t.Fatalf("message content %q missing %q", gotContent, want)
		}
	}
}

func TestSendPeerMessageAcceptsExecutionIDAsRecipient(t *testing.T) {
	svc, _, mt := newTestAgentService(t)
	ctx := context.Background()
	db := svc.(*agentService).store
	createTask(t, db, "recipient")
	if err := svc.SpawnAgent(ctx, "recipient", 80, 24); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetAgentSessionByTaskID(ctx, "recipient")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.SendPeerMessage(ctx, "sender", session.ExecutionID, "hello by execution"); err != nil {
		t.Fatal(err)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	messages := mt.sentMultiline["legato-recipient"]
	if len(messages) != 1 || !strings.Contains(messages[0], "hello by execution") {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestListPeersGroupsAgentsByGitWorktreeRoot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, taskID := range []string{"task-a", "task-b", "task-c"} {
		createTask(t, s, taskID)
	}

	repo := initTestRepo(t)
	dirA := filepath.Join(repo, "a")
	dirB := filepath.Join(repo, "b")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatal(err)
	}
	otherRepo := initTestRepo(t)
	mt := newMockTmux()
	svc := NewAgentService(s, mt, repo)
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "task-a", 80, 24, AgentSpawnOptions{WorkingDir: dirA}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "task-b", 80, 24, AgentSpawnOptions{WorkingDir: dirB}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "task-c", 80, 24, AgentSpawnOptions{WorkingDir: otherRepo}); err != nil {
		t.Fatal(err)
	}

	peers, err := svc.ListPeers(ctx, "task-a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].TaskID != "task-b" {
		t.Fatalf("peers = %#v, want task-b only", peers)
	}
	if peers[0].WorkingDir != repo {
		t.Fatalf("peer working directory = %q, want worktree root %q", peers[0].WorkingDir, repo)
	}
}

func TestListPeersReconcilesDeadSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTask(t, s, "caller")
	createTask(t, s, "dead")
	repo := initTestRepo(t)
	mt := newMockTmux()
	svc := NewAgentService(s, mt, repo)
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "caller", 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "dead", 80, 24); err != nil {
		t.Fatal(err)
	}
	mt.Kill("legato-dead")

	peers, err := svc.ListPeers(ctx, "caller", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Fatalf("peers = %#v, want stale session excluded", peers)
	}
}

func TestBroadcastToPeersExcludesSenderAndReportsPartialFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, taskID := range []string{"sender", "healthy", "stale"} {
		createTask(t, s, taskID)
	}
	repo := initTestRepo(t)
	mt := newMockTmux()
	svc := NewAgentService(s, mt, repo)
	ctx := context.Background()
	for _, taskID := range []string{"sender", "healthy", "stale"} {
		if err := svc.SpawnAgent(ctx, taskID, 80, 24); err != nil {
			t.Fatal(err)
		}
	}
	mt.mu.Lock()
	mt.sendMultilineErr["legato-stale"] = errors.New("delivery failed")
	mt.mu.Unlock()

	result, err := svc.BroadcastToPeers(ctx, "sender", "", "", "touching agent.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Delivered) != 1 || result.Delivered[0] != "healthy" {
		t.Fatalf("delivered = %#v", result.Delivered)
	}
	if len(result.Failed) != 1 || result.Failed[0].TaskID != "stale" {
		t.Fatalf("failed = %#v", result.Failed)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if got := mt.sentMultiline["legato-healthy"]; len(got) != 1 || !strings.Contains(got[0], "touching agent.go") || !strings.Contains(got[0], "Agent-to-agent message from `sender`") {
		t.Fatalf("healthy messages = %#v", got)
	}
	if got := mt.sentMultiline["legato-sender"]; len(got) != 0 {
		t.Fatalf("sender messages = %#v", got)
	}
}

func TestSpawnAgentNotifiesNewAndExistingWorktreePeers(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTask(t, s, "first")
	createTask(t, s, "second")
	repo := initTestRepo(t)
	mt := newMockTmux()
	svc := NewAgentService(s, mt, repo, AgentServiceOptions{BriefKickoffDelay: time.Millisecond})
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "first", 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "second", 80, 24); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		mt.mu.Lock()
		first := append([]string(nil), mt.sentMultiline["legato-first"]...)
		second := append([]string(nil), mt.sentMultiline["legato-second"]...)
		mt.mu.Unlock()
		if len(first) == 1 && len(second) == 1 {
			if !strings.Contains(first[0], "second") || !strings.Contains(first[0], "joined") {
				t.Fatalf("existing peer notice = %q", first[0])
			}
			if !strings.Contains(second[0], "first") || !strings.Contains(second[0], "already working") {
				t.Fatalf("new peer notice = %q", second[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("notices first=%#v second=%#v", first, second)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDisabledPeerAwarenessSuppressesNoticesButKeepsDiscovery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTask(t, s, "first")
	createTask(t, s, "second")
	repo := initTestRepo(t)
	mt := newMockTmux()
	svc := NewAgentService(s, mt, repo, AgentServiceOptions{DisablePeerAwareness: true})
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "first", 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "second", 80, 24); err != nil {
		t.Fatal(err)
	}
	peers, err := svc.ListPeers(ctx, "second", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].TaskID != "first" {
		t.Fatalf("peers = %#v", peers)
	}
	time.Sleep(10 * time.Millisecond)
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.sentMultiline["legato-first"]) != 0 || len(mt.sentMultiline["legato-second"]) != 0 {
		t.Fatalf("automatic notices were sent: %#v", mt.sentMultiline)
	}
}

func TestSpawnAgentBeginsReviewCaptureInOrdinaryRepository(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTask(t, s, "task-review")
	repo := initTestRepo(t)
	base := gitRun(t, repo, "rev-parse", "HEAD")
	mt := newMockTmux()
	svc := NewAgentService(s, mt, repo)

	if err := svc.SpawnAgent(context.Background(), "task-review", 80, 24); err != nil {
		t.Fatal(err)
	}
	tour, err := s.GetDefaultReviewTour(context.Background(), "task-review")
	if err != nil {
		t.Fatal(err)
	}
	if tour.BaseSHA != base || tour.RepositoryPath != repo {
		t.Fatalf("tour = %+v, want base %s and repo %s", tour, base, repo)
	}
}

func newTestAgentServiceWithNotifier(t *testing.T, notifier Notifier) (AgentService, *store.Store, *mockTmux) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Notifier: notifier,
	})
	return svc, s, mt
}

func createTask(t *testing.T, s *store.Store, id string) {
	t.Helper()
	ctx := context.Background()
	err := s.CreateTask(ctx, store.Task{
		ID:        id,
		Title:     "Test " + id,
		Status:    "Doing",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSpawnAgentCreatesSessionAndDBRow(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Tmux session should exist
	if !mt.sessionAlive("legato-REX-1238") {
		t.Error("expected tmux session legato-REX-1238 to exist")
	}

	// DB row should exist
	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].TaskID != "REX-1238" {
		t.Errorf("TaskID = %q, want %q", agents[0].TaskID, "REX-1238")
	}
	if agents[0].Status != "running" {
		t.Errorf("Status = %q, want %q", agents[0].Status, "running")
	}
}

func TestSpawnAgentRemembersWorkingDirectoryAndPreservesCodeContext(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task-remember")
	existing := &store.CodeContext{
		Repo: "acme/widget", Branch: "main", HeadSHA: "abc123", WorkingDir: "/old",
		BaseBranch: "main", RepoSource: "manual", BranchSource: "worktree", LinkedAt: "2024-01-01T00:00:00Z",
	}
	if err := s.SetCodeContext(ctx, "task-remember", existing); err != nil {
		t.Fatal(err)
	}

	if err := svc.SpawnAgent(ctx, "task-remember", 0, 0, AgentSpawnOptions{WorkingDir: "/repos/widget", AgentKind: "shell"}); err != nil {
		t.Fatal(err)
	}

	task, err := s.GetTask(ctx, "task-remember")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.WorkingDir != "/repos/widget" || got.UpdatedAt == "" {
		t.Fatalf("code context = %#v", got)
	}
	if got.Repo != existing.Repo || got.Branch != existing.Branch || got.HeadSHA != existing.HeadSHA || got.BaseBranch != existing.BaseBranch || got.RepoSource != existing.RepoSource || got.BranchSource != existing.BranchSource || got.LinkedAt != existing.LinkedAt {
		t.Fatalf("repository context was not preserved: %#v", got)
	}
}

func TestSpawnAgentRemembersFallbackDirectoryForAdapter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTask(t, s, "task-adapter")
	mt := newMockTmux()
	svc := NewAgentService(s, mt, "/global/default", AgentServiceOptions{Adapter: &testAdapter{}})

	if err := svc.SpawnAgent(context.Background(), "task-adapter", 0, 0); err != nil {
		t.Fatal(err)
	}
	task, _ := s.GetTask(context.Background(), "task-adapter")
	got, _ := store.ParseCodeContext(task.CodeContext)
	if got == nil || got.WorkingDir != "/global/default" {
		t.Fatalf("code context = %#v, want fallback working directory", got)
	}
}

func TestSpawnAgentFailureDoesNotReplaceRememberedDirectory(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task-failure")
	if err := s.SetCodeContext(ctx, "task-failure", &store.CodeContext{WorkingDir: "/remembered"}); err != nil {
		t.Fatal(err)
	}
	mt.spawnErr = errors.New("spawn failed")

	if err := svc.SpawnAgent(ctx, "task-failure", 0, 0, AgentSpawnOptions{WorkingDir: "/replacement"}); err == nil {
		t.Fatal("expected spawn failure")
	}
	task, _ := s.GetTask(ctx, "task-failure")
	got, _ := store.ParseCodeContext(task.CodeContext)
	if got == nil || got.WorkingDir != "/remembered" {
		t.Fatalf("code context = %#v, want prior directory", got)
	}
}

func TestSpawnAgentAutomationDoesNotRememberWorkingDirectory(t *testing.T) {
	tests := []struct {
		name string
		opts AgentSpawnOptions
	}{
		{name: "workflow", opts: AgentSpawnOptions{ExecutionKind: store.ExecutionWorkflowAgent}},
		{name: "swarm", opts: AgentSpawnOptions{Role: "conductor", ParentTaskID: "parent", ExecutionKind: store.ExecutionSwarmConductor}},
		{name: "ephemeral", opts: AgentSpawnOptions{Ephemeral: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, s, _ := newTestAgentService(t)
			ctx := context.Background()
			taskID := "task-" + tt.name
			createTask(t, s, taskID)
			tt.opts.WorkingDir = "/automation"
			if err := svc.SpawnAgent(ctx, taskID, 0, 0, tt.opts); err != nil {
				t.Fatal(err)
			}
			task, _ := s.GetTask(ctx, taskID)
			if task.CodeContext != nil {
				t.Fatalf("automation launch stored code context: %s", *task.CodeContext)
			}
		})
	}
}

func TestSpawnAgentDuplicateReturnsError(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err == nil {
		t.Error("expected error for duplicate spawn, got nil")
	}
}

func TestKillAgentDestroysSessionAndUpdatesDB(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	// Record a working interval so we can verify it gets closed on kill
	if err := svc.SetAgentActivity(ctx, "REX-1238", "working", ""); err != nil {
		t.Fatal(err)
	}

	if err := svc.KillAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}

	// Tmux session should be gone
	if mt.sessionAlive("legato-REX-1238") {
		t.Error("expected tmux session to be killed")
	}

	// Dead sessions are cleaned up — list should be empty
	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Fatalf("got %d agents, want 0 (dead sessions cleaned up)", len(agents))
	}

	// Any open intervals for the killed task should now be closed
	var openCount int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM execution_activity a JOIN executions e ON e.id=a.execution_id WHERE e.execution_task_id = ? AND a.ended_at IS NULL", "REX-1238").Scan(&openCount); err != nil {
		t.Fatal(err)
	}
	if openCount != 0 {
		t.Errorf("expected 0 open intervals after KillAgent, got %d", openCount)
	}
}

func TestKillAlreadyDeadAgentNoError(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.KillAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}
	// Kill again — should not error
	if err := svc.KillAgent(ctx, "REX-1238"); err != nil {
		t.Errorf("expected no error killing dead agent, got %v", err)
	}
}

func TestReconcileSessionsMarksDeadSessions(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")
	createTask(t, s, "REX-1239")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "REX-1239", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Simulate REX-1238's tmux session dying externally
	mt.markSessionDead("legato-REX-1238")

	if err := svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}

	// ListAgents only returns running sessions — dead ones are filtered out
	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1 (only running)", len(agents))
	}
	if agents[0].TaskID != "REX-1239" {
		t.Errorf("expected REX-1239 (running), got %s", agents[0].TaskID)
	}
	if agents[0].Status != "running" {
		t.Errorf("REX-1239 status = %q, want running", agents[0].Status)
	}

	// REX-1238 should no longer be spawnable as duplicate (it's dead, will be cleaned on re-spawn)
	// Verify by re-spawning — should succeed since dead sessions are cleaned up
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Errorf("re-spawn after reconcile should succeed, got %v", err)
	}
	mt.Kill("legato-REX-1238") // cleanup
}

func TestSpawnAgentWithStaleDBSession(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	// Spawn an agent, then simulate the tmux session dying without reconciliation
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	// Record a working interval so we can verify it gets closed during stale cleanup
	if err := svc.SetAgentActivity(ctx, "REX-1238", "working", ""); err != nil {
		t.Fatal(err)
	}
	// Kill tmux session externally (DB still says "running")
	mt.markSessionDead("legato-REX-1238")

	// Re-spawn should succeed — stale DB record should be cleaned up
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Errorf("re-spawn with stale DB session should succeed, got %v", err)
	}

	// Tmux session should exist again
	if !mt.sessionAlive("legato-REX-1238") {
		t.Error("expected tmux session legato-REX-1238 to exist after re-spawn")
	}

	// Any open intervals from the stale session should now be closed
	var openCount int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM execution_activity a JOIN executions e ON e.id=a.execution_id WHERE e.execution_task_id = ? AND a.ended_at IS NULL", "REX-1238").Scan(&openCount); err != nil {
		t.Fatal(err)
	}
	// The stale execution was terminalized before the replacement was created.
	if openCount > 1 {
		t.Errorf("expected at most 1 open interval after stale-session re-spawn, got %d", openCount)
	}
}

func TestCaptureOutputReturnsContent(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	mt.setCapture("legato-REX-1238", "hello world\n$ ")

	output, err := svc.CaptureOutput(ctx, "REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if output != "hello world\n$ " {
		t.Errorf("output = %q, want %q", output, "hello world\n$ ")
	}
}

type testAdapter struct{}

func (a *testAdapter) Name() string                           { return "test-tool" }
func (a *testAdapter) InstallHooks(projectDir string) error   { return nil }
func (a *testAdapter) UninstallHooks(projectDir string) error { return nil }
func (a *testAdapter) EnvVars(taskID, socketPath string) map[string]string {
	return map[string]string{
		"LEGATO_TASK_ID":    taskID,
		"LEGATO_FUTURE_ENV": "available",
	}
}

func TestSpawnAgentInjectsEnvVarsWhenAdapterConfigured(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter:    &testAdapter{},
		SocketPath: "/tmp/legato.sock",
	})

	ctx := context.Background()
	createTask(t, s, "task1")

	if err := svc.SpawnAgent(ctx, "task1", 0, 0); err != nil {
		t.Fatal(err)
	}

	envs := mt.envVarsFor("legato-task1")
	if envs == nil {
		t.Fatal("no env vars set on tmux session")
	}
	if envs["LEGATO_TASK_ID"] != "task1" {
		t.Errorf("LEGATO_TASK_ID = %q, want %q", envs["LEGATO_TASK_ID"], "task1")
	}
	if _, ok := envs["LEGATO_TASK_EPHEMERAL"]; ok {
		t.Error("LEGATO_TASK_EPHEMERAL should not be set for a board-card task")
	}

	provided, ok := svc.(AgentEnvironmentProvider)
	if !ok {
		t.Fatal("agent service does not provide its launch environment")
	}
	env, err := provided.GetAgentEnvironment(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	for _, want := range []string{"LEGATO_TASK_ID=task1", "LEGATO_FUTURE_ENV=available"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("provided environment missing %q: %v", want, env)
		}
	}
	if _, ok := envs["LEGATO_SOCKET"]; ok {
		t.Error("LEGATO_SOCKET should not be set (CLI uses broadcast)")
	}
}

func TestSpawnAgentSkipsEnvVarsWithoutAdapter(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task2")

	if err := svc.SpawnAgent(ctx, "task2", 0, 0); err != nil {
		t.Fatal(err)
	}

	env := mt.envVarsFor("legato-task2")
	if len(env) != 1 || env["LEGATO_EXECUTION_ID"] == "" {
		t.Errorf("expected only durable execution identity without adapter, got %v", env)
	}
}

func TestSpawnAgentPassesDimensionsToTmux(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 90, 40); err != nil {
		t.Fatal(err)
	}

	dims := mt.spawnDimsFor("legato-REX-1238")
	if dims[0] != 90 {
		t.Errorf("width = %d, want 90", dims[0])
	}
	if dims[1] != 40 {
		t.Errorf("height = %d, want 40", dims[1])
	}
}

func TestAttachCmdReturnsCommand(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	cmd, err := svc.AttachCmd(ctx, "REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
}

func TestListAgentsOverridesCommandWithLiveValue(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Set live pane command
	mt.paneCommands = map[string]string{
		"legato-REX-1238": "claude",
	}

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].Command != "claude" {
		t.Errorf("Command = %q, want %q", agents[0].Command, "claude")
	}
	if agents[0].AgentKind != "shell" {
		t.Errorf("AgentKind = %q, want shell", agents[0].AgentKind)
	}
}

func TestSpawnAgentMatchesChimeraLockfileByExpectedSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	instancesDir := filepath.Join(home, ".chimera", "instances")
	if err := os.MkdirAll(instancesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instance":
			fmt.Fprint(w, `{"pid":123}`)
		case "/v1/sessions/task-fast":
			fmt.Fprint(w, `{"id":"task-fast"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(chimeraServer.Close)
	adapter := &fakeAdapter{name: "chimera", launchFn: func(map[string]string, string, string) string {
		lockfiles := map[string]string{
			"100.json": `{"sessionId":"other-task","url":"http://127.0.0.1:43100"}`,
			"200.json": fmt.Sprintf(`{"sessionId":"task-fast","url":%q}`, chimeraServer.URL),
		}
		for name, lockfile := range lockfiles {
			if err := os.WriteFile(filepath.Join(instancesDir, name), []byte(lockfile), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return "chimera"
	}}
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter})
	ctx := context.Background()
	createTask(t, s, "task-fast")

	if err := svc.SpawnAgent(ctx, "task-fast", 0, 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session, err := s.GetAgentSessionByTaskID(ctx, "task-fast")
		if err == nil && session.ChimeraEndpoint != nil {
			if *session.ChimeraEndpoint != chimeraServer.URL || session.ChimeraSessionID == nil || *session.ChimeraSessionID != "task-fast" {
				t.Fatalf("discovery = %#v", session)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected Chimera lockfile was not discovered")
}

func TestSpawnAgentPersistsResolvedAdapterKind(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	adapter := &fakeAdapter{name: "codex"}
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter})
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].AgentKind != "codex" {
		t.Errorf("AgentKind = %q, want codex", agents[0].AgentKind)
	}
}

func TestListAgentsKeepsDBCommandForDeadSessions(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Kill tmux session externally and reconcile
	mt.markSessionDead("legato-REX-1238")
	if err := svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}

	// PaneCommands returns empty (session is dead)
	mt.paneCommands = map[string]string{}

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Dead sessions still in DB should keep stored command
	for _, a := range agents {
		if a.TaskID == "REX-1238" {
			if a.Command != "shell" {
				t.Errorf("dead session Command = %q, want %q", a.Command, "shell")
			}
		}
	}
}

func TestReconcileClosesOrphanedIntervals(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Record a working interval
	if err := svc.SetAgentActivity(ctx, "REX-1238", "working", ""); err != nil {
		t.Fatal(err)
	}

	// Simulate tmux session dying
	mt.markSessionDead("legato-REX-1238")

	if err := svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}

	// The open interval should be closed
	durations, err := s.GetStateDurations(ctx, "REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	// Duration should exist and be very small (just created)
	if _, ok := durations["working"]; !ok {
		t.Error("expected working duration to exist after reconcile")
	}

	// Verify no open intervals remain
	var openCount int
	if err := s.DB().GetContext(ctx, &openCount,
		"SELECT COUNT(*) FROM execution_activity a JOIN executions e ON e.id=a.execution_id WHERE e.execution_task_id = ? AND a.ended_at IS NULL", "REX-1238"); err != nil {
		t.Fatal(err)
	}
	if openCount != 0 {
		t.Errorf("got %d open intervals, want 0 after reconcile", openCount)
	}
}

func seedAgentTestExecution(t *testing.T, s *store.Store, id, taskID, state string, start, end time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateExecution(ctx, store.Execution{ID: id, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, TaskTitle: taskID, StartedAt: start}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionActivityAt(ctx, id, state, "", start); err != nil {
		t.Fatal(err)
	}
	if err := s.TerminalizeExecutionAt(ctx, id, store.ExecutionSucceeded, end); err != nil {
		t.Fatal(err)
	}
}

func TestGetTaskDurations(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task1")
	createTask(t, s, "task2")

	start := time.Now().UTC().Add(-2 * time.Minute)
	seedAgentTestExecution(t, s, "exec-task1-work", "task1", store.ActivityWorking, start, start.Add(30*time.Second))
	seedAgentTestExecution(t, s, "exec-task1-wait", "task1", store.ActivityWaiting, start.Add(40*time.Second), start.Add(50*time.Second))
	seedAgentTestExecution(t, s, "exec-task2-work", "task2", store.ActivityWorking, start, start.Add(time.Minute))

	result, err := svc.GetTaskDurations(ctx, []string{"task1", "task2"})
	if err != nil {
		t.Fatal(err)
	}

	// task1 should have both working and waiting
	if _, ok := result["task1"]; !ok {
		t.Fatal("expected task1 in results")
	}
	if result["task1"].Working == 0 {
		t.Error("expected task1 to have working duration")
	}
	if result["task1"].Waiting == 0 {
		t.Error("expected task1 to have waiting duration")
	}

	// task2 should have working
	if _, ok := result["task2"]; !ok {
		t.Fatal("expected task2 in results")
	}
	if result["task2"].Working == 0 {
		t.Error("expected task2 to have working duration")
	}
}

func TestListAgentsPreservesBackingTaskAndExposesExecutionAttribution(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "user-task")
	createTask(t, s, "workflow-task")

	if err := svc.SpawnAgent(ctx, "workflow-task", 0, 0, AgentSpawnOptions{
		ExecutionKind: store.ExecutionWorkflowAgent, AttributionTaskID: "user-task", WorkflowRunID: "run-1",
	}); err != nil {
		t.Fatal(err)
	}

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	got := agents[0]
	if got.TaskID != "workflow-task" || got.AttributionTaskID != "user-task" || got.ExecutionID == "" || got.ExecutionKind != store.ExecutionWorkflowAgent || got.WorkflowRunID != "run-1" {
		t.Fatalf("agent identity = %#v", got)
	}
}

func TestListAgentsPopulatesTaskTitle(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].Title != "Test REX-1238" {
		t.Errorf("Title = %q, want %q", agents[0].Title, "Test REX-1238")
	}
}

func TestSetTaskGroupIsReflectedByListAgentsAndCanBeCleared(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	const taskID = "REX-GROUP"
	createTask(t, s, taskID)
	if err := svc.SpawnAgent(ctx, taskID, 0, 0); err != nil {
		t.Fatal(err)
	}

	group := "backend"
	if err := svc.SetTaskGroup(ctx, taskID, &group); err != nil {
		t.Fatal(err)
	}
	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Group != group {
		t.Fatalf("agent Group = %q, want %q", agents[0].Group, group)
	}
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Group == nil || *task.Group != group {
		t.Fatalf("persisted Group = %v, want %q", task.Group, group)
	}

	if err := svc.SetTaskGroup(ctx, taskID, nil); err != nil {
		t.Fatal(err)
	}
	agents, err = svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if agents[0].Group != "" {
		t.Fatalf("agent Group after clear = %q, want empty", agents[0].Group)
	}
}

func TestListAgentsEmptyTitleWhenTaskMissing(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Delete the task from the store so lookup fails
	s.DB().ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", "REX-1238")

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].Title != "" {
		t.Errorf("Title = %q, want empty string for missing task", agents[0].Title)
	}
}

func TestSpawnAgentAppliesTmuxOptions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		TmuxOptions: map[string]string{
			"mouse":         "on",
			"history-limit": "50000",
		},
	})

	ctx := context.Background()
	createTask(t, s, "task1")

	if err := svc.SpawnAgent(ctx, "task1", 0, 0); err != nil {
		t.Fatal(err)
	}

	opts := mt.optionsFor("legato-task1")
	if opts == nil {
		t.Fatal("no tmux options set on session")
	}
	if opts["mouse"] != "on" {
		t.Errorf("mouse = %q, want %q", opts["mouse"], "on")
	}
	if opts["history-limit"] != "50000" {
		t.Errorf("history-limit = %q, want %q", opts["history-limit"], "50000")
	}
}

func TestSpawnAgentNoOptionsWithoutConfig(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task1")

	if err := svc.SpawnAgent(ctx, "task1", 0, 0); err != nil {
		t.Fatal(err)
	}

	if len(mt.optionsFor("legato-task1")) != 0 {
		t.Errorf("expected no tmux options without config, got %v", mt.optionsFor("legato-task1"))
	}
}

func TestGetAgentSummary_MixedStates(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task1")
	createTask(t, s, "task2")
	createTask(t, s, "task3")

	svc.SpawnAgent(ctx, "task1", 0, 0)
	svc.SpawnAgent(ctx, "task2", 0, 0)
	svc.SpawnAgent(ctx, "task3", 0, 0)

	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "waiting")
	// task3 stays idle

	working, waiting, _, idle, err := svc.GetAgentSummary(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if working != 1 {
		t.Errorf("working = %d, want 1", working)
	}
	if waiting != 1 {
		t.Errorf("waiting = %d, want 1", waiting)
	}
	if idle != 1 {
		t.Errorf("idle = %d, want 1", idle)
	}
}

func TestGetAgentSummary_ExcludesTask(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task1")
	createTask(t, s, "task2")

	svc.SpawnAgent(ctx, "task1", 0, 0)
	svc.SpawnAgent(ctx, "task2", 0, 0)

	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "working")

	working, _, _, _, err := svc.GetAgentSummary(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if working != 1 {
		t.Errorf("working = %d, want 1 (task1 excluded)", working)
	}
}

func TestGetAgentSummary_ReconcilesCleansDeadSessions(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task1")
	createTask(t, s, "task2")

	svc.SpawnAgent(ctx, "task1", 0, 0)
	svc.SpawnAgent(ctx, "task2", 0, 0)

	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "working")

	// Simulate task2 tmux session dying
	mt.markSessionDead("legato-task2")

	working, _, _, _, err := svc.GetAgentSummary(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if working != 1 {
		t.Errorf("working = %d, want 1 (dead session reconciled)", working)
	}
}

func TestSpawnAgentInjectsStatusLineOptions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		BinaryPath: "/usr/bin/legato",
	})

	ctx := context.Background()
	createTask(t, s, "task1")

	if err := svc.SpawnAgent(ctx, "task1", 0, 0); err != nil {
		t.Fatal(err)
	}

	opts := mt.optionsFor("legato-task1")
	if opts == nil {
		t.Fatal("no tmux options set on session")
	}
	if opts["status-right"] != "#(/usr/bin/legato agent summary --exclude task1)" {
		t.Errorf("status-right = %q, want legato agent summary command", opts["status-right"])
	}
	if opts["status-interval"] != "5" {
		t.Errorf("status-interval = %q, want %q", opts["status-interval"], "5")
	}
}

func TestSpawnAgentSwarmParticipantUsesStatusCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		BinaryPath: "/usr/bin/legato",
	})

	ctx := context.Background()
	createTask(t, s, "parent-1")
	createTask(t, s, "st-01")

	if err := svc.SpawnAgent(ctx, "st-01", 0, 0, AgentSpawnOptions{
		Role:         "backend",
		ParentTaskID: "parent-1",
		SubtaskID:    "st-01",
	}); err != nil {
		t.Fatal(err)
	}

	opts := mt.optionsFor("legato-st-01")
	if opts == nil {
		t.Fatal("no tmux options set on session")
	}
	want := "#(/usr/bin/legato agent status st-01 --format tmux)"
	if opts["status-right"] != want {
		t.Errorf("status-right = %q, want %q", opts["status-right"], want)
	}
}

func TestSpawnAgentUserOptionsOverrideStatusLine(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		BinaryPath: "/usr/bin/legato",
		TmuxOptions: map[string]string{
			"status-right": "my custom status",
		},
	})

	ctx := context.Background()
	createTask(t, s, "task1")

	if err := svc.SpawnAgent(ctx, "task1", 0, 0); err != nil {
		t.Fatal(err)
	}

	opts := mt.optionsFor("legato-task1")
	// User's status-right should override legato's default
	if opts["status-right"] != "my custom status" {
		t.Errorf("status-right = %q, want user override %q", opts["status-right"], "my custom status")
	}
}

func TestSpawnAgentSkipsStatusLineWithoutBinaryPath(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task1")

	if err := svc.SpawnAgent(ctx, "task1", 0, 0); err != nil {
		t.Fatal(err)
	}

	opts := mt.optionsFor("legato-task1")
	if _, ok := opts["status-right"]; ok {
		t.Error("expected no status-right without binary path")
	}
}

func TestSpawnEphemeralAgentCreatesTaskAndSession(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()

	// Seed a column mapping so CreateEphemeralTask finds a status
	if err := s.CreateColumnMapping(ctx, store.ColumnMapping{
		ColumnName: "Backlog", SortOrder: 0,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.SpawnEphemeralAgent(ctx, "debugging auth", 120, 40); err != nil {
		t.Fatal(err)
	}

	// Should have one agent session
	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}

	// Agent should have the ephemeral task's title
	if agents[0].Title != "debugging auth" {
		t.Errorf("Title = %q, want %q", agents[0].Title, "debugging auth")
	}

	// Tmux session should exist with legato- prefix
	taskID := agents[0].TaskID
	if !mt.sessionAlive("legato-" + taskID) {
		t.Errorf("expected tmux session legato-%s to exist", taskID)
	}

	// The spawned agent can distinguish this hidden backing task from a board card.
	envs := mt.envVarsFor("legato-" + taskID)
	if envs["LEGATO_TASK_EPHEMERAL"] != "1" {
		t.Errorf("LEGATO_TASK_EPHEMERAL = %q, want 1", envs["LEGATO_TASK_EPHEMERAL"])
	}

	// The backing task should be ephemeral
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !task.Ephemeral {
		t.Error("expected backing task to be ephemeral")
	}
}

func TestListAgentsFallsBackOnPaneCommandsError(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Simulate PaneCommands failure
	mt.paneCommandsErr = fmt.Errorf("tmux not available")

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	// Should fall back to DB value
	if agents[0].Command != "shell" {
		t.Errorf("Command = %q, want %q (fallback)", agents[0].Command, "shell")
	}
}

func TestGetStateTimelineReturnsBuckets(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task1")

	now := time.Now().UTC()
	seedAgentTestExecution(t, s, "exec-timeline-work", "task1", store.ActivityWorking, now.Add(-6*time.Minute), now.Add(-4*time.Minute))
	seedAgentTestExecution(t, s, "exec-timeline-wait", "task1", store.ActivityWaiting, now.Add(-4*time.Minute), now.Add(-2*time.Minute))

	window := 6 * time.Minute
	buckets := 3
	res, err := svc.GetStateTimeline(ctx, "task1", window, buckets)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("got %d buckets, want 3", len(res))
	}
	if res[0] != "working" {
		t.Errorf("bucket[0] = %q, want working", res[0])
	}
	if res[1] != "waiting" {
		t.Errorf("bucket[1] = %q, want waiting", res[1])
	}
	if res[2] != "" {
		t.Errorf("bucket[2] = %q, want idle", res[2])
	}
}

// fakeNotifier records notification calls for testing.
type fakeNotifier struct {
	calls      []struct{ title, message string }
	configured bool
	canNotify  bool
}

func (f *fakeNotifier) Notify(title, message string) error {
	f.calls = append(f.calls, struct{ title, message string }{title, message})
	return nil
}

func (f *fakeNotifier) Configured() bool        { return f.configured }
func (f *fakeNotifier) CanNotify(_ string) bool { return f.canNotify }

func TestSetAgentActivity_FiresNotification(t *testing.T) {
	fn := &fakeNotifier{configured: true, canNotify: true}
	svc, s, _ := newTestAgentServiceWithNotifier(t, fn)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	if err := svc.SetTaskNotifyEnabled(ctx, "REX-1238", true); err != nil {
		t.Fatal(err)
	}

	// working -> waiting should fire
	if err := svc.SetAgentActivity(ctx, "REX-1238", "working", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAgentActivity(ctx, "REX-1238", "waiting", ""); err != nil {
		t.Fatal(err)
	}

	if len(fn.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(fn.calls))
	}
	if fn.calls[0].title != "Agent ready" {
		t.Errorf("title = %q, want %q", fn.calls[0].title, "Agent ready")
	}
	wantMsg := "Agent Test REX-1238 is now waiting"
	if fn.calls[0].message != wantMsg {
		t.Errorf("message = %q, want %q", fn.calls[0].message, wantMsg)
	}
}

func TestSetAgentActivity_ErrorNotifiesOnEntryOnly(t *testing.T) {
	push := &fakeNotifier{configured: true, canNotify: true}
	osNotifier := &fakeNotifier{configured: true, canNotify: true}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc := NewAgentService(s, newMockTmux(), t.TempDir(), AgentServiceOptions{Notifier: push, OSNotifier: osNotifier})
	ctx := context.Background()
	createTask(t, s, "REX-1238")
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTaskNotifyEnabled(ctx, "REX-1238", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAgentActivity(ctx, "REX-1238", store.ActivityError, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAgentActivity(ctx, "REX-1238", store.ActivityError, ""); err != nil {
		t.Fatal(err)
	}
	for name, notifier := range map[string]*fakeNotifier{"push": push, "os": osNotifier} {
		if len(notifier.calls) != 1 {
			t.Fatalf("%s notifications = %+v", name, notifier.calls)
		}
		if notifier.calls[0].title != "Agent error" || notifier.calls[0].message != "Agent Test REX-1238 had an error" {
			t.Fatalf("%s notification = %+v", name, notifier.calls[0])
		}
	}
}

func TestSetAgentActivity_ErrorOSNotificationIgnoresPushPreference(t *testing.T) {
	push := &fakeNotifier{configured: true, canNotify: true}
	osNotifier := &fakeNotifier{configured: true, canNotify: true}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc := NewAgentService(s, newMockTmux(), t.TempDir(), AgentServiceOptions{Notifier: push, OSNotifier: osNotifier})
	ctx := context.Background()
	createTask(t, s, "REX-1238")
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAgentActivity(ctx, "REX-1238", store.ActivityError, ""); err != nil {
		t.Fatal(err)
	}
	if len(push.calls) != 0 || len(osNotifier.calls) != 1 {
		t.Fatalf("push=%+v os=%+v", push.calls, osNotifier.calls)
	}
}

func TestSetAgentActivity_NoNotificationWhenDisabled(t *testing.T) {
	fn := &fakeNotifier{configured: true, canNotify: true}
	svc, s, _ := newTestAgentServiceWithNotifier(t, fn)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	// notify is NOT enabled (defaults to false)

	if err := svc.SetAgentActivity(ctx, "REX-1238", "working", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAgentActivity(ctx, "REX-1238", "waiting", ""); err != nil {
		t.Fatal(err)
	}

	if len(fn.calls) != 0 {
		t.Fatalf("expected 0 notifications, got %d", len(fn.calls))
	}
}

func TestSetAgentActivity_NoNotificationForWorkingTransition(t *testing.T) {
	fn := &fakeNotifier{configured: true, canNotify: true}
	svc, s, _ := newTestAgentServiceWithNotifier(t, fn)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTaskNotifyEnabled(ctx, "REX-1238", true); err != nil {
		t.Fatal(err)
	}

	// waiting -> working should NOT fire
	if err := svc.SetAgentActivity(ctx, "REX-1238", "waiting", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAgentActivity(ctx, "REX-1238", "working", ""); err != nil {
		t.Fatal(err)
	}

	if len(fn.calls) != 0 {
		t.Fatalf("expected 0 notifications, got %d", len(fn.calls))
	}
}
