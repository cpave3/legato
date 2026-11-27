package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

// These tests pin what prepareTaskResume does with a spawn's options on
// every path: which conversation it picks, which fields it fills, and the
// exact refusal. A resumed conversation keeps its account; it never falls
// back to defaults that changed since it ran.

const prepTask = "task-prep"

type prepFixture struct {
	t     *testing.T
	svc   *agentService
	store *store.Store
	home  string
	runs  int
}

// newPrepFixture is an agent service whose default tool is the first
// adapter, with every adapter configured by name.
func newPrepFixture(t *testing.T, accounts []config.ClaudeAccount, adapters ...AIToolAdapter) *prepFixture {
	t.Helper()
	for _, key := range namedAccountAuthVars {
		t.Setenv(key, "")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	s, err := store.New(filepath.Join(t.TempDir(), "prepare.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTask(t, s, prepTask)
	byName := map[string]AIToolAdapter{}
	for _, adapter := range adapters {
		byName[adapter.Name()] = adapter
	}
	svc := NewAgentService(s, newMockTmux(), t.TempDir(), AgentServiceOptions{
		Adapter: adapters[0], Adapters: byName, ClaudeAccounts: accounts, DisablePeerAwareness: true,
	})
	return &prepFixture{t: t, svc: svc.(*agentService), store: s, home: home}
}

// save records a conversation the task's main slot ran, as its bridge
// does, with the account its execution launched with (nil for none).
func (f *prepFixture) save(kind, id, cwd, storage string, identity *store.AgentAccountIdentity) {
	t := f.t
	t.Helper()
	ctx := context.Background()
	f.runs++
	execution := fmt.Sprintf("ex-prep-%d", f.runs)
	session, err := f.store.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: prepTask, AgentKind: kind, ExecutionID: execution, TmuxSession: execution, Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.CreateExecution(ctx, store.Execution{ID: execution, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: prepTask, ExecutionTaskID: prepTask, AgentKind: kind, AgentSessionID: &session, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if identity != nil {
		if err := f.store.PutExecutionAccount(ctx, execution, *identity); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.store.RecordTaskNativeSession(ctx, prepTask, session, execution, kind, id, cwd, storage); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateAgentSessionStatus(ctx, prepTask, "dead"); err != nil {
		t.Fatal(err)
	}
}

func (f *prepFixture) prepare(opt AgentSpawnOptions) (AgentSpawnOptions, error) {
	err := f.svc.prepareTaskResume(context.Background(), prepTask, &opt)
	return opt, err
}

// defaultIdentity is the implicit Claude account of the fixture's home.
func (f *prepFixture) defaultIdentity() *store.AgentAccountIdentity {
	dir := filepath.Join(f.home, ".claude")
	resolved, err := resolvedAccountRoot(dir)
	if err != nil {
		f.t.Fatal(err)
	}
	return &store.AgentAccountIdentity{Provider: "claude", ConfigDir: dir, ResolvedDir: resolved}
}

// resumeFields are the options prepareTaskResume may fill.
type resumeFields struct {
	AgentKind, ResumeSessionID, ResumeWorkingDir, ResumeStorageDir, WorkingDir string
	AccountIdentity                                                            *store.AgentAccountIdentity
}

func fieldsOf(opt AgentSpawnOptions) resumeFields {
	return resumeFields{opt.AgentKind, opt.ResumeSessionID, opt.ResumeWorkingDir, opt.ResumeStorageDir, opt.WorkingDir, opt.AccountIdentity}
}

func wantFields(t *testing.T, opt AgentSpawnOptions, err error, want resumeFields) {
	t.Helper()
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := fieldsOf(opt); !reflect.DeepEqual(got, want) {
		t.Fatalf("options = %+v (identity %+v)\nwant      %+v (identity %+v)", got, got.AccountIdentity, want, want.AccountIdentity)
	}
}

func wantRefusal(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("prepare succeeded, want %q", want)
	}
	if err.Error() != want {
		t.Fatalf("error = %q\nwant    %q", err, want)
	}
}

const refused = "task session cannot be resumed: "

func TestPrepareResumeLeavesOptionsAloneWithoutAResumePolicy(t *testing.T) {
	for _, policy := range []string{"", "error"} {
		t.Run("policy "+policy, func(t *testing.T) {
			// No adapter is configured for codex: these policies never look.
			f := newPrepFixture(t, nil, hooks.NewClaudeCodeAdapter("legato"))
			opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: policy, AgentKind: "codex", WorkingDir: "/elsewhere"})
			wantFields(t, opt, err, resumeFields{AgentKind: "codex", WorkingDir: "/elsewhere"})
		})
	}
}

func TestPrepareResumeNewPolicyChecksOnlyTheAdapter(t *testing.T) {
	f := newPrepFixture(t, nil, hooks.NewClaudeCodeAdapter("legato"))
	f.save("claude-code", "saved", t.TempDir(), filepath.Join(f.home, ".claude"), nil)
	opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "new", WorkingDir: "/elsewhere"})
	// The tool stays unnamed: new does not infer it from saved sessions.
	wantFields(t, opt, err, resumeFields{WorkingDir: "/elsewhere"})

	_, err = f.prepare(AgentSpawnOptions{SessionPolicy: "new", AgentKind: "codex"})
	wantRefusal(t, err, refused+"adapter codex is not configured")

	claude := hooks.NewClaudeCodeAdapter("legato")
	claude.SetLaunchArgs([]string{"--continue"})
	f = newPrepFixture(t, nil, claude)
	_, err = f.prepare(AgentSpawnOptions{SessionPolicy: "new"})
	wantRefusal(t, err, refused+"configured argument --continue overrides the requested conversation or workspace; remove it from launch_args/tier arguments")
}

func TestPrepareResumeRefusesAnUnsupportedPolicy(t *testing.T) {
	f := newPrepFixture(t, nil, hooks.NewCodexAdapter("legato"))
	_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "fork"})
	wantRefusal(t, err, refused+"unsupported session policy for codex")
}

func TestPrepareResumeRefusesAToolThatIsNotConfigured(t *testing.T) {
	f := newPrepFixture(t, nil, hooks.NewClaudeCodeAdapter("legato"))
	_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex"})
	wantRefusal(t, err, refused+"adapter codex is not configured")

	// The tool inferred from the newest saved session must be configured too.
	f.save("codex", "saved", t.TempDir(), "", nil)
	opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume"})
	wantRefusal(t, err, refused+"adapter codex is not configured")
	if opt.AgentKind != "codex" {
		t.Fatalf("inferred kind not kept on refusal: %q", opt.AgentKind)
	}
}

func TestPrepareResumeInfersTheToolFromTheNewestSavedSession(t *testing.T) {
	t.Run("none saved", func(t *testing.T) {
		f := newPrepFixture(t, nil, hooks.NewClaudeCodeAdapter("legato"))
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume"})
		wantRefusal(t, err, refused+"no saved conversation")
	})
	t.Run("codex", func(t *testing.T) {
		f := newPrepFixture(t, nil, hooks.NewClaudeCodeAdapter("legato"), hooks.NewCodexAdapter("legato"))
		cwd, codexHome := t.TempDir(), t.TempDir()
		f.save("claude-code", "older", t.TempDir(), filepath.Join(f.home, ".claude"), nil)
		f.save("codex", "newest", cwd, codexHome, nil)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume"})
		wantFields(t, opt, err, resumeFields{AgentKind: "codex", ResumeSessionID: "newest", ResumeWorkingDir: cwd, ResumeStorageDir: codexHome, WorkingDir: cwd})
	})
	t.Run("chimera takes the saved ID and stops there", func(t *testing.T) {
		f := newPrepFixture(t, nil, hooks.NewClaudeCodeAdapter("legato"), hooks.NewChimeraAdapter("legato"))
		f.save("chimera", "chimera-conversation", t.TempDir(), "", nil)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", WorkingDir: "/elsewhere"})
		wantFields(t, opt, err, resumeFields{AgentKind: "chimera", ResumeSessionID: "chimera-conversation", WorkingDir: "/elsewhere"})
	})
	t.Run("chimera keeps a caller's ID", func(t *testing.T) {
		f := newPrepFixture(t, nil, hooks.NewClaudeCodeAdapter("legato"), hooks.NewChimeraAdapter("legato"))
		f.save("chimera", "chimera-conversation", t.TempDir(), "", nil)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", ResumeSessionID: "hook-captured"})
		wantFields(t, opt, err, resumeFields{AgentKind: "chimera", ResumeSessionID: "hook-captured"})
	})
	t.Run("explicit chimera checks nothing saved", func(t *testing.T) {
		f := newPrepFixture(t, nil, hooks.NewChimeraAdapter("legato"))
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "chimera"})
		wantFields(t, opt, err, resumeFields{AgentKind: "chimera"})
	})
}

func TestPrepareResumeSelectsTheSavedOrAnEarlierConversation(t *testing.T) {
	codex := func(t *testing.T) *prepFixture { return newPrepFixture(t, nil, hooks.NewCodexAdapter("legato")) }

	t.Run("none saved", func(t *testing.T) {
		f := codex(t)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex"})
		wantRefusal(t, err, refused+"no matching saved codex conversation")
	})
	t.Run("the saved one", func(t *testing.T) {
		f := codex(t)
		cwd, storage := t.TempDir(), t.TempDir()
		f.save("codex", "saved", cwd, storage, nil)
		for _, opt := range []AgentSpawnOptions{
			{SessionPolicy: "resume", AgentKind: "codex"},
			// The same ID in the same storage needs no history lookup.
			{SessionPolicy: "resume", AgentKind: "codex", ResumeSessionID: "saved", ResumeStorageDir: storage},
		} {
			got, err := f.prepare(opt)
			wantFields(t, got, err, resumeFields{AgentKind: "codex", ResumeSessionID: "saved", ResumeWorkingDir: cwd, ResumeStorageDir: storage, WorkingDir: cwd})
		}
	})
	t.Run("a different session ID", func(t *testing.T) {
		f := codex(t)
		earlierCWD, earlierStorage := t.TempDir(), t.TempDir()
		f.save("codex", "earlier", earlierCWD, earlierStorage, nil)
		f.save("codex", "newest", t.TempDir(), t.TempDir(), nil)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex", ResumeSessionID: "earlier"})
		wantFields(t, opt, err, resumeFields{AgentKind: "codex", ResumeSessionID: "earlier", ResumeWorkingDir: earlierCWD, ResumeStorageDir: earlierStorage, WorkingDir: earlierCWD})
	})
	t.Run("no match", func(t *testing.T) {
		f := codex(t)
		f.save("codex", "saved", t.TempDir(), t.TempDir(), nil)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex", ResumeSessionID: "unknown"})
		wantRefusal(t, err, refused+"no matching saved codex conversation")
	})

	// One provider ID kept in two storage roots.
	twoRoots := func(t *testing.T) (f *prepFixture, firstCWD, firstStorage, secondCWD, secondStorage string) {
		f = codex(t)
		firstCWD, firstStorage, secondCWD, secondStorage = t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
		f.save("codex", "shared", firstCWD, firstStorage, nil)
		f.save("codex", "shared", secondCWD, secondStorage, nil)
		return
	}
	t.Run("an ID with no storage asks the history, which is ambiguous", func(t *testing.T) {
		f, _, _, _, _ := twoRoots(t)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex", ResumeSessionID: "shared"})
		wantRefusal(t, err, refused+store.ErrConflict.Error()+`: ambiguous codex conversation "shared" exists in multiple storage roots`)
		if !errors.Is(err, ErrTaskResumeConflict) || !errors.Is(err, store.ErrConflict) {
			t.Fatalf("error does not wrap both conflicts: %v", err)
		}
	})
	t.Run("no ID resumes the saved one without asking", func(t *testing.T) {
		f, _, _, secondCWD, secondStorage := twoRoots(t)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex"})
		wantFields(t, opt, err, resumeFields{AgentKind: "codex", ResumeSessionID: "shared", ResumeWorkingDir: secondCWD, ResumeStorageDir: secondStorage, WorkingDir: secondCWD})
	})
	t.Run("storage that differs from the saved one", func(t *testing.T) {
		f, firstCWD, firstStorage, _, _ := twoRoots(t)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex", ResumeStorageDir: firstStorage})
		// Without an ID the history has nothing to find.
		wantRefusal(t, err, refused+"no matching saved codex conversation")
		opt, err = f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex", ResumeSessionID: "shared", ResumeStorageDir: firstStorage})
		wantFields(t, opt, err, resumeFields{AgentKind: "codex", ResumeSessionID: "shared", ResumeWorkingDir: firstCWD, ResumeStorageDir: firstStorage, WorkingDir: firstCWD})
	})
	t.Run("an account's directory is the storage", func(t *testing.T) {
		f := newPrepFixture(t, nil, hooks.NewClaudeCodeAdapter("legato"))
		defaultCWD, otherCWD := t.TempDir(), t.TempDir()
		identity := f.defaultIdentity()
		f.save("claude-code", "shared", defaultCWD, identity.ConfigDir, nil)
		f.save("claude-code", "shared", otherCWD, t.TempDir(), nil)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "claude-code", ResumeSessionID: "shared", ResumeStorageDir: "/ignored", AccountIdentity: identity})
		wantFields(t, opt, err, resumeFields{AgentKind: "claude-code", ResumeSessionID: "shared", ResumeWorkingDir: defaultCWD, ResumeStorageDir: identity.ConfigDir, WorkingDir: defaultCWD, AccountIdentity: identity})
	})
}

func TestPrepareResumeChecksTheSavedDirectory(t *testing.T) {
	codex := func(t *testing.T) *prepFixture { return newPrepFixture(t, nil, hooks.NewCodexAdapter("legato")) }

	t.Run("missing", func(t *testing.T) {
		f := codex(t)
		cwd := t.TempDir()
		f.save("codex", "saved", cwd, "", nil)
		if err := os.Remove(cwd); err != nil {
			t.Fatal(err)
		}
		_, statErr := os.Stat(cwd)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex"})
		wantRefusal(t, err, fmt.Sprintf(refused+"saved codex directory %q is unavailable: %v", cwd, statErr))
	})
	t.Run("not a directory", func(t *testing.T) {
		f := codex(t)
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		f.save("codex", "saved", file, "", nil)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex"})
		wantRefusal(t, err, fmt.Sprintf(refused+"saved directory %q is not a directory", file))
	})
	t.Run("a different working directory", func(t *testing.T) {
		f := codex(t)
		cwd, other := t.TempDir(), t.TempDir()
		f.save("codex", "saved", cwd, "", nil)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex", WorkingDir: other})
		wantRefusal(t, err, fmt.Sprintf(refused+"saved codex conversation belongs to %q, not %q; choose that directory or start a new session", cwd, other))
	})
	t.Run("an unavailable working directory", func(t *testing.T) {
		f := codex(t)
		f.save("codex", "saved", t.TempDir(), "", nil)
		gone := filepath.Join(t.TempDir(), "gone")
		_, evalErr := filepath.EvalSymlinks(gone)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex", WorkingDir: gone})
		wantRefusal(t, err, fmt.Sprintf(refused+"selected directory is unavailable: %v", evalErr))
	})
	t.Run("the same directory through symlinks", func(t *testing.T) {
		f := codex(t)
		real := t.TempDir()
		links := t.TempDir()
		savedLink, chosenLink := filepath.Join(links, "saved"), filepath.Join(links, "chosen")
		for _, link := range []string{savedLink, chosenLink} {
			if err := os.Symlink(real, link); err != nil {
				t.Fatal(err)
			}
		}
		f.save("codex", "saved", savedLink, "", nil)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex", WorkingDir: chosenLink})
		// The saved spelling wins.
		wantFields(t, opt, err, resumeFields{AgentKind: "codex", ResumeSessionID: "saved", ResumeWorkingDir: savedLink, WorkingDir: savedLink})
	})
	t.Run("a repository or clone keeps the working directory", func(t *testing.T) {
		for _, opt := range []AgentSpawnOptions{
			{SessionPolicy: "resume", AgentKind: "codex", Repo: "owner/name"},
			{SessionPolicy: "resume", AgentKind: "codex", Clone: 7},
		} {
			f := codex(t)
			cwd := t.TempDir()
			f.save("codex", "saved", cwd, "", nil)
			got, err := f.prepare(opt)
			wantFields(t, got, err, resumeFields{AgentKind: "codex", ResumeSessionID: "saved", ResumeWorkingDir: cwd})
		}
		f := codex(t)
		cwd := t.TempDir()
		f.save("codex", "saved", cwd, "", nil)
		got, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", AgentKind: "codex", Repo: "owner/name", WorkingDir: cwd})
		wantFields(t, got, err, resumeFields{AgentKind: "codex", ResumeSessionID: "saved", ResumeWorkingDir: cwd, WorkingDir: cwd})
	})
}

func TestPrepareResumeBindsTheClaudeAccountTheConversationRanWith(t *testing.T) {
	claude := func(t *testing.T, accounts ...config.ClaudeAccount) *prepFixture {
		return newPrepFixture(t, accounts, hooks.NewClaudeCodeAdapter("legato"))
	}

	t.Run("the saved identity", func(t *testing.T) {
		f := claude(t)
		cwd := t.TempDir()
		identity := f.defaultIdentity()
		f.save("claude-code", "saved", cwd, identity.ConfigDir, identity)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume"})
		wantFields(t, opt, err, resumeFields{AgentKind: "claude-code", ResumeSessionID: "saved", ResumeWorkingDir: cwd, ResumeStorageDir: identity.ConfigDir, WorkingDir: cwd, AccountIdentity: identity})
	})
	t.Run("the saved identity after the default moved", func(t *testing.T) {
		f := claude(t)
		identity := f.defaultIdentity()
		f.save("claude-code", "saved", t.TempDir(), identity.ConfigDir, identity)
		t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume"})
		wantRefusal(t, err, "saved Claude account configuration changed; restore its original directory mapping before resuming")
	})
	t.Run("a saved named account no longer configured", func(t *testing.T) {
		f := claude(t)
		dir := t.TempDir()
		identity := &store.AgentAccountIdentity{Provider: "claude", Account: "work", ConfigDir: dir, ResolvedDir: dir, ConfigEnvSet: true}
		f.save("claude-code", "saved", t.TempDir(), dir, identity)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume"})
		wantRefusal(t, err, `Claude account "work" is not configured on this instance; restore its configuration before resuming`)
	})
	t.Run("the saved identity against another selected account", func(t *testing.T) {
		f := claude(t)
		identity := f.defaultIdentity()
		f.save("claude-code", "saved", t.TempDir(), identity.ConfigDir, identity)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", Account: "work"})
		wantRefusal(t, err, "saved Claude account conflicts with the selected account")
	})
	t.Run("legacy storage of the default account", func(t *testing.T) {
		f := claude(t)
		cwd := t.TempDir()
		identity := f.defaultIdentity()
		f.save("claude-code", "saved", cwd, identity.ConfigDir, nil)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume"})
		wantFields(t, opt, err, resumeFields{AgentKind: "claude-code", ResumeSessionID: "saved", ResumeWorkingDir: cwd, ResumeStorageDir: identity.ConfigDir, WorkingDir: cwd, AccountIdentity: identity})
	})
	t.Run("legacy storage of no account", func(t *testing.T) {
		f := claude(t)
		f.save("claude-code", "saved", t.TempDir(), t.TempDir(), nil)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume"})
		wantRefusal(t, err, refused+"legacy Claude storage has no matching local account; restore its original account mapping")
	})
	t.Run("legacy storage with an unknown account selected", func(t *testing.T) {
		f := claude(t)
		f.save("claude-code", "saved", t.TempDir(), f.defaultIdentity().ConfigDir, nil)
		_, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", Account: "missing"})
		wantRefusal(t, err, `Claude account "missing" is not configured on this instance; restore its configuration before resuming`)
	})
	t.Run("no storage binds nothing but is still checked", func(t *testing.T) {
		f := claude(t)
		cwd := t.TempDir()
		f.save("claude-code", "saved", cwd, "", nil)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume"})
		wantFields(t, opt, err, resumeFields{AgentKind: "claude-code", ResumeSessionID: "saved", ResumeWorkingDir: cwd, WorkingDir: cwd})
		_, err = f.prepare(AgentSpawnOptions{SessionPolicy: "resume", Account: "missing"})
		wantRefusal(t, err, `Claude account "missing" is not configured on this instance; restore its configuration before resuming`)
	})
	t.Run("codex binds no account", func(t *testing.T) {
		f := newPrepFixture(t, nil, hooks.NewCodexAdapter("legato"))
		cwd, storage := t.TempDir(), t.TempDir()
		f.save("codex", "saved", cwd, storage, nil)
		opt, err := f.prepare(AgentSpawnOptions{SessionPolicy: "resume", Account: "work"})
		wantFields(t, opt, err, resumeFields{AgentKind: "codex", ResumeSessionID: "saved", ResumeWorkingDir: cwd, ResumeStorageDir: storage, WorkingDir: cwd})
	})
}
