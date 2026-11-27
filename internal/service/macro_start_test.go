package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/cpave3/legato/config"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// macroAgentsSpy records what a macro start asks of the agent service.
type macroAgentsSpy struct {
	defaults NewWorkDefaults
	running  map[string]bool
	spawned  []AgentSpawnOptions
	queued   []string
	saved    *TaskResumeSession
	queueErr error
	// gone are the folders that are not there; every other one is.
	gone map[string]bool
}

func (m *macroAgentsSpy) CaptureOutput(_ context.Context, taskID string) (string, error) {
	if m.running[taskID] {
		return "", nil
	}
	return "", errors.New("no agent")
}
func (m *macroAgentsSpy) QueueMessage(_ context.Context, _ string, message string) error {
	m.queued = append(m.queued, message)
	return m.queueErr
}
func (m *macroAgentsSpy) SpawnAgent(_ context.Context, _ string, _, _ int, opts ...AgentSpawnOptions) error {
	m.spawned = append(m.spawned, opts...)
	return nil
}
func (m *macroAgentsSpy) NewWorkDefaults(context.Context) (NewWorkDefaults, error) {
	if m.defaults.Agent != "" {
		return m.defaults, nil
	}
	return NewWorkDefaults{Agent: "claude-code"}, nil
}
func (m *macroAgentsSpy) GetTaskResumeSession(context.Context, string, string) (*TaskResumeSession, error) {
	return m.saved, nil
}

func newMacroStartFixture(t *testing.T) (*MacroService, *macroAgentsSpy) {
	t.Helper()
	settings, db := newSettingsFixture(t, "")
	account, _ := json.Marshal(map[string]string{"config_dir": t.TempDir(), "label": "Synthetic work"})
	if err := settings.SetEntry(context.Background(), "claude_accounts", "work", account, "test"); err != nil {
		t.Fatal(err)
	}
	remote := "LEG-7"
	if err := db.CreateTask(context.Background(), store.Task{ID: "abc12345", Title: "Fix the login", Status: "Doing", RemoteID: &remote, CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	createTask(t, db, "local01")
	svc := NewMacroService(settings)
	spy := &macroAgentsSpy{running: map[string]bool{}, gone: map[string]bool{}}
	svc.SetAgents(spy)
	svc.folderExists = func(path string) bool { return !spy.gone[path] }
	return svc, spy
}

func TestAMacroStartsTheTasksAgentWithItsToolAndTier(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	ctx := context.Background()
	if err := svc.Save(ctx, "deep review", config.Macro{Keys: "Review it deeply.\n", Agent: "codex", Tier: "smart"}, "web"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Start(ctx, MacroStart{Name: "deep review", TaskID: "abc12345"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Started || got.Queued || len(spy.spawned) != 1 {
		t.Fatalf("result = %+v, spawned %v", got, spy.spawned)
	}
	opts := spy.spawned[0]
	if opts.AgentKind != "codex" || opts.Tier != "smart" {
		t.Fatalf("spawned %q tier %q", opts.AgentKind, opts.Tier)
	}
	want := "Task LEG-7: Fix the login\n\nReview it deeply."
	if opts.Prompt != want {
		t.Fatalf("prompt = %q, want %q", opts.Prompt, want)
	}
	if strings.HasPrefix(opts.Prompt, "[legato") {
		t.Fatal("a macro is the operator's words and is never tagged")
	}
}

func TestAMacroWithNoToolResumesTheSavedToolNotTheNewDefault(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	spy.saved = &TaskResumeSession{AgentKind: "codex", ID: "sess-1", CWD: "/work"}
	if _, err := svc.Start(context.Background(), MacroStart{Name: "plan", TaskID: "local01"}); err != nil {
		t.Fatal(err)
	}
	opts := spy.spawned[0]
	if opts.AgentKind != "codex" || opts.Tier != "" {
		t.Fatalf("spawned %q tier %q, want the saved tool", opts.AgentKind, opts.Tier)
	}
	if opts.SessionPolicy != "resume" || opts.ResumeSessionID != "sess-1" || opts.WorkingDir != "/work" {
		t.Fatalf("resume = %+v", opts)
	}
	if !strings.HasPrefix(opts.Prompt, "Task local01: Test local01\n\nPlan this task") {
		t.Fatalf("prompt = %q", opts.Prompt)
	}
}

func TestAMacroOnARunningAgentIsQueuedAsItsNextMessage(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	spy.running["abc12345"] = true
	got, err := svc.Start(context.Background(), MacroStart{Name: "Review", TaskID: "abc12345", PR: &MacroPR{Repo: "acme/web", Number: 42, URL: "https://github.com/acme/web/pull/42"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Started || !got.Queued || len(spy.spawned) != 0 || len(spy.queued) != 1 {
		t.Fatalf("result = %+v, queued %v, spawned %v", got, spy.queued, spy.spawned)
	}
	want := "Task LEG-7: Fix the login\nPull request acme/web#42: https://github.com/acme/web/pull/42\n\nReview this pull request."
	if !strings.HasPrefix(spy.queued[0], want) || strings.HasSuffix(spy.queued[0], "\n") {
		t.Fatalf("message = %q", spy.queued[0])
	}
}

func TestAMacroStartRefusesAnUnknownMacroOrTask(t *testing.T) {
	svc, _ := newMacroStartFixture(t)
	ctx := context.Background()
	if _, err := svc.Start(ctx, MacroStart{Name: "nope", TaskID: "abc12345"}); !errors.Is(err, ErrMacroNotFound) {
		t.Fatalf("unknown macro = %v", err)
	}
	if _, err := svc.Start(ctx, MacroStart{Name: "Plan", TaskID: "missing"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown task = %v", err)
	}
}

func TestTheSuggestedMacroFollowsWhatItIsStartedOn(t *testing.T) {
	cases := []struct {
		target MacroTarget
		want   string
	}{
		{MacroTarget{}, "Plan"},
		{MacroTarget{PR: true, Mine: true}, "Address feedback"},
		{MacroTarget{PR: true}, "Review"},
	}
	for _, c := range cases {
		if got := SuggestedMacro(c.target); got != c.want {
			t.Errorf("SuggestedMacro(%+v) = %q, want %q", c.target, got, c.want)
		}
	}
	if s := MacroSuggestionsFor(); s.Task != "Plan" || s.MyPR != "Address feedback" || s.OthersPR != "Review" {
		t.Fatalf("suggestions = %+v", s)
	}
}

func TestAMacroOnAPullRequestWithoutAURLLinksToGitHub(t *testing.T) {
	svc, _ := newMacroStartFixture(t)
	got, err := svc.Message(context.Background(), "Look.\n", "local01", &MacroPR{Repo: "acme/web", Number: 7})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Task local01: Test local01\nPull request acme/web#7: https://github.com/acme/web/pull/7\n\nLook." {
		t.Fatalf("message = %q", got)
	}
}

func TestAMacroCapturesProfileAgentAndTierTogether(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	spy.defaults = NewWorkDefaults{Agent: "codex", Tier: "profile"}
	result, err := svc.Start(context.Background(), MacroStart{Name: "plan", TaskID: "local01"})
	if err != nil {
		t.Fatal(err)
	}
	if result.AgentKind != "codex" || len(spy.spawned) != 1 || spy.spawned[0].AgentKind != "codex" || spy.spawned[0].Tier != "profile" {
		t.Fatalf("result=%+v spawn=%+v", result, spy.spawned)
	}
}

func TestMacroAccountIsUsedForFreshWorkAndSavedIdentityForResume(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	ctx := context.Background()
	if err := svc.Save(ctx, "account", config.Macro{Keys: "do it", Agent: "claude-code", Account: "work"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(ctx, MacroStart{Name: "account", TaskID: "local01"}); err != nil {
		t.Fatal(err)
	}
	if spy.spawned[0].Account != "work" {
		t.Fatalf("fresh options = %+v", spy.spawned[0])
	}
	identity := &store.AgentAccountIdentity{Provider: "claude", Account: "personal", ConfigDir: "/old", ResolvedDir: "/old"}
	spy.saved = &TaskResumeSession{AgentKind: "claude-code", ID: "s1", CWD: "/work", AccountIdentity: identity}
	if _, err := svc.Start(ctx, MacroStart{Name: "account", TaskID: "local01"}); err != nil {
		t.Fatal(err)
	}
	got := spy.spawned[1]
	if got.Account != "personal" || got.AccountIdentity != identity {
		t.Fatalf("resumed options = %+v", got)
	}
	spy.gone["/work"] = true
	if err := svc.settings.db.SetCodeContext(ctx, "local01", &store.CodeContext{Repo: "acme/app"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(ctx, MacroStart{Name: "account", TaskID: "local01"}); err != nil {
		t.Fatal(err)
	}
	if got := spy.spawned[2]; got.Account != "work" || got.AccountIdentity != nil {
		t.Fatalf("fresh clone options = %+v", got)
	}
}

func TestMacroExplicitAccountCannotSwitchAResumedConversation(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	spy.saved = &TaskResumeSession{AgentKind: "claude-code", ID: "s1", CWD: "/work", AccountIdentity: &store.AgentAccountIdentity{Provider: "claude", Account: "work"}}
	_, err := svc.Start(context.Background(), MacroStart{Name: "Plan", TaskID: "local01", Account: "personal"})
	if err == nil || !strings.Contains(err.Error(), "account") || len(spy.spawned) != 0 {
		t.Fatalf("start = %v, spawns %+v", err, spy.spawned)
	}
}

func TestMacroAccountSelectsClaudeWithoutInheritingOtherToolProfile(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	spy.defaults = NewWorkDefaults{Agent: "codex", Tier: "codex-tier"}
	if err := svc.Save(context.Background(), "account", config.Macro{Keys: "do it", Account: "work"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), MacroStart{Name: "account", TaskID: "local01"}); err != nil {
		t.Fatal(err)
	}
	if got := spy.spawned[0]; got.Account != "work" || got.AgentKind != "claude-code" || got.Tier != "" {
		t.Fatalf("spawn = %+v", got)
	}
}
