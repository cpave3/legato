package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
)

type capabilityFixture struct {
	svc    AgentService
	store  *store.Store
	tmux   *mockTmux
	skills *SkillService
	dir    string // the skills folder
	root   string // the capability folders
}

func newCapabilityFixture(t *testing.T) capabilityFixture {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "capabilities.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	dir := filepath.Join(t.TempDir(), "skills")
	skillSvc := NewSkillService(dir, s)
	root := filepath.Join(t.TempDir(), "capabilities")
	mt := newMockTmux()
	claude := hooks.NewClaudeCodeAdapter("legato")
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter: claude,
		Adapters: map[string]AIToolAdapter{
			"claude-code": claude, "codex": hooks.NewCodexAdapter("legato"), "chimera": hooks.NewChimeraAdapter("legato"),
		},
		Capabilities:         skillSvc,
		CapabilityFolders:    CapabilityFolders{Root: root},
		DisablePeerAwareness: true, BriefKickoffDelay: time.Millisecond,
	})
	svc.(*agentService).nativeBindTimeout = time.Millisecond
	return capabilityFixture{svc: svc, store: s, tmux: mt, skills: skillSvc, dir: dir, root: root}
}

func (f capabilityFixture) spawn(t *testing.T, taskID, kind string) (execution string, env map[string]string) {
	t.Helper()
	createTask(t, f.store, taskID)
	if err := f.svc.SpawnAgent(context.Background(), taskID, 0, 0, AgentSpawnOptions{AgentKind: kind, WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	env = f.tmux.envVarsFor("legato-" + taskID)
	return env["LEGATO_EXECUTION_ID"], env
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestAClaudeCodeAgentGetsTheSkillsThatAreOnAsAPluginOfItsOwn(t *testing.T) {
	f := newCapabilityFixture(t)
	ctx := context.Background()
	writeLegatoSkill(t, f.dir, "tdd", "Tests first.")
	writeLegatoSkill(t, f.dir, "deploy", "Ship it.")
	if err := f.skills.SetEnabled(ctx, "deploy", false); err != nil {
		t.Fatal(err)
	}

	execution, env := f.spawn(t, "task-c", "claude-code")
	plugin := filepath.Join(f.root, execution, "claude-code", "legato-skills")
	if !slices.Contains(filepath.SplitList(env["CLAUDE_CODE_PLUGIN_DIRS"]), plugin) {
		t.Fatalf("CLAUDE_CODE_PLUGIN_DIRS = %q, want it to hold %s", env["CLAUDE_CODE_PLUGIN_DIRS"], plugin)
	}
	var manifest struct {
		Name string `json:"name"`
	}
	content, err := os.ReadFile(filepath.Join(plugin, ".claude-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(content, &manifest) != nil || manifest.Name != "legato-skills" {
		t.Fatalf("plugin.json = %s, %v; want a plugin named legato-skills", content, err)
	}
	if skill, err := os.ReadFile(filepath.Join(plugin, "skills", "tdd", "SKILL.md")); err != nil || !strings.Contains(string(skill), "name: tdd") {
		t.Fatalf("tdd's SKILL.md = %q, %v", skill, err)
	}
	if pathExists(filepath.Join(plugin, "skills", "deploy")) {
		t.Fatal("deploy is off, yet the agent was given it")
	}

	// A change reaches the next agent; the running one keeps its own copy.
	if err := f.skills.SetEnabled(ctx, "tdd", false); err != nil {
		t.Fatal(err)
	}
	if err := f.skills.SetEnabled(ctx, "deploy", true); err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(plugin, "skills", "tdd")) {
		t.Fatal("turning tdd off took it from the running agent")
	}
	next, _ := f.spawn(t, "task-d", "claude-code")
	nextPlugin := filepath.Join(f.root, next, "claude-code", "legato-skills")
	if pathExists(filepath.Join(nextPlugin, "skills", "tdd")) || !pathExists(filepath.Join(nextPlugin, "skills", "deploy")) {
		t.Fatal("the next agent was not given the skills that are on now")
	}

	// The folder goes with the execution.
	if err := f.svc.KillAgent(ctx, "task-c"); err != nil {
		t.Fatal(err)
	}
	if pathExists(filepath.Join(f.root, execution)) {
		t.Fatalf("%s is still there after its agent was stopped", filepath.Join(f.root, execution))
	}
}

func TestAChimeraAgentGetsTheSkillsAsAModOfItsOwn(t *testing.T) {
	f := newCapabilityFixture(t)
	writeLegatoSkill(t, f.dir, "tdd", "Tests first.")

	execution, env := f.spawn(t, "task-m", "chimera")
	mod := filepath.Join(f.root, execution, "chimera", "legato-skills")
	if !slices.Contains(filepath.SplitList(env["CHIMERA_MOD_DIRS"]), mod) {
		t.Fatalf("CHIMERA_MOD_DIRS = %q, want it to hold %s", env["CHIMERA_MOD_DIRS"], mod)
	}
	var manifest map[string]any
	content, err := os.ReadFile(filepath.Join(mod, "chimera-mod.json"))
	if err != nil || json.Unmarshal(content, &manifest) != nil {
		t.Fatalf("chimera-mod.json = %s, %v", content, err)
	}
	if manifest["name"] != "legato-skills" || manifest["apiVersion"] != float64(1) || manifest["skills"] != "skills" || manifest["version"] == "" {
		t.Fatalf("manifest = %v", manifest)
	}
	if _, hasModule := manifest["module"]; hasModule {
		t.Fatalf("manifest = %v: a mod of skills alone needs no module", manifest)
	}
	if !pathExists(filepath.Join(mod, "skills", "tdd", "SKILL.md")) {
		t.Fatal("tdd was not put in the mod's skills folder")
	}
}

func TestAnAgentWhoseToolCannotLoadSkillsOrWhoHasNoneGetsNoFolder(t *testing.T) {
	f := newCapabilityFixture(t)
	execution, env := f.spawn(t, "task-none", "claude-code")
	if pathExists(filepath.Join(f.root, execution)) || strings.Contains(env["CLAUDE_CODE_PLUGIN_DIRS"], "legato-skills") {
		t.Fatalf("with no skill on, the agent got a skills folder: %q", env["CLAUDE_CODE_PLUGIN_DIRS"])
	}

	writeLegatoSkill(t, f.dir, "tdd", "Tests first.")
	execution, env = f.spawn(t, "task-x", "codex")
	if pathExists(filepath.Join(f.root, execution)) {
		t.Fatal("Codex has no way to load Legato's skills, yet got a folder")
	}
	for key, value := range env {
		if strings.Contains(value, f.root) {
			t.Fatalf("%s = %q names the capability folders", key, value)
		}
	}
}

func TestTheSweepClearsTheFoldersOfEndedExecutionsOnly(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"ex-ended", "ex-running", "ex-unknown"} {
		if err := os.MkdirAll(filepath.Join(root, id, "claude-code"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	status := func(_ context.Context, id string) (string, bool, error) {
		switch id {
		case "ex-ended":
			return store.ExecutionSucceeded, true, nil
		case "ex-running":
			return store.ExecutionRunning, true, nil
		}
		return "", false, nil
	}
	cleared, err := CapabilityFolders{Root: root}.Sweep(context.Background(), status)
	if err != nil || cleared != 1 {
		t.Fatalf("Sweep = %d, %v; want 1", cleared, err)
	}
	if pathExists(filepath.Join(root, "ex-ended")) || !pathExists(filepath.Join(root, "ex-running")) || !pathExists(filepath.Join(root, "ex-unknown")) {
		t.Fatal("the sweep removed the wrong folders")
	}
}

func TestASandboxedAgentSeesItsOwnSkillsReadOnlyAndNotTheSkillsFolder(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "sandboxed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	real := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	home, dataDir := real(t.TempDir()), real(t.TempDir())
	skillsDir := filepath.Join(home, ".legato", "skills")
	writeLegatoSkill(t, skillsDir, "tdd", "Tests first.")
	backend := &fakeBackend{}
	claude := hooks.NewClaudeCodeAdapter("legato")
	svc := NewAgentService(s, newMockTmux(), t.TempDir(), AgentServiceOptions{
		Adapter: claude,
		Sandbox: &SandboxSettings{Backend: backend, Base: SandboxPlanInput{
			Home: home, DataDir: dataDir, RuntimeDir: filepath.Join(t.TempDir(), "legato"), SkillsDir: skillsDir, DefaultMounts: true,
		}},
		Capabilities:         NewSkillService(skillsDir, s),
		CapabilityFolders:    CapabilityFolders{Root: filepath.Join(dataDir, "capabilities")},
		DisablePeerAwareness: true, BriefKickoffDelay: time.Millisecond,
	})
	svc.(*agentService).nativeBindTimeout = time.Millisecond
	svc.(*agentService).SetBrokerEnv(testBrokerEnv)
	createTask(t, s, "task-s")
	if err := svc.SpawnAgent(context.Background(), "task-s", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if len(backend.plans) != 1 {
		t.Fatalf("plans = %d", len(backend.plans))
	}
	plan := backend.plans[0]
	session, err := s.GetAgentSessionByTaskID(context.Background(), "task-s")
	if err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(dataDir, "capabilities", session.ExecutionID)
	if m, ok := mountOf(plan, own); !ok || m.Kind != sandbox.ReadOnly {
		t.Fatalf("the execution's capability folder: %+v, %v; want it read-only", m, ok)
	}
	for _, m := range plan.Mounts {
		if m.Kind != sandbox.Hide && m.Kind != sandbox.HideDir && (within(skillsDir, m.Path) || within(m.Path, skillsDir) || m.Path == filepath.Join(dataDir, "capabilities")) {
			t.Errorf("mount %+v shows the skills folder or every execution's capabilities", m)
		}
	}
}

func TestTheSandboxNeverShowsTheSkillsFolderWritable(t *testing.T) {
	in := baseInput()
	fs := hostFS()
	fs["/var/home/me/.legato"] = "dir"
	fs["/var/home/me/.legato/skills"] = "dir"
	fs["/var/home/me/.local/share/legato/capabilities/ex-1"] = "dir"
	in.FS = fs
	in.SkillsDir = "/var/home/me/.legato/skills"
	in.CapabilitiesDir = "/var/home/me/.local/share/legato/capabilities/ex-1"

	in.Extra = []SandboxPath{{Path: "/var/home/me/.legato", Access: "write"}}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	m := mounted(plan)
	if got := m["/var/home/me/.local/share/legato/capabilities/ex-1"]; got.Kind != sandbox.ReadOnly {
		t.Fatalf("capability folder: %+v, want read-only", got)
	}
	if got, ok := m["/var/home/me/.legato/skills"]; !ok || (got.Kind != sandbox.HideDir && got.Kind != sandbox.Hide) {
		t.Fatalf("skills folder under a writable mount: %+v, want it hidden", got)
	}

	in.Extra = nil
	in.WorkDir = "/var/home/me/.legato/skills"
	if _, err := BuildSandboxPlan(in); err == nil {
		t.Fatal("an agent was let work in the skills folder")
	}
}
