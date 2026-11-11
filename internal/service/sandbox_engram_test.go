package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/engram"
	"github.com/cpave3/legato/internal/engine/sandbox"
)

func engramInput() SandboxPlanInput {
	in := baseInput()
	fs := hostFS()
	fs["/opt/engram/bin/engram"] = "file"
	fs["/var/home/me/.local/share/engram"] = "dir"
	in.FS = fs
	in.Engram = SandboxEngram{
		Binary: "/opt/engram/bin/engram", BinMount: "/run/user/1000/legato/sandbox/ex-1/bin/engram",
		Store: "/var/home/me/.local/share/engram",
	}
	return in
}

func TestAnEngramSessionSeesItsBinaryAndItsStore(t *testing.T) {
	plan, err := BuildSandboxPlan(engramInput())
	if err != nil {
		t.Fatal(err)
	}
	m := mounted(plan)
	if got := m["/run/user/1000/legato/sandbox/ex-1/bin/engram"]; got.Kind != sandbox.ReadOnly || got.Path != "/opt/engram/bin/engram" {
		t.Errorf("binary mount = %+v", got)
	}
	if got := m["/var/home/me/.local/share/engram"]; got.Kind != sandbox.ReadWrite {
		t.Errorf("store mount = %+v", got)
	}
}

func TestAnEngramStoreThatIsMissingRefusesTheSandbox(t *testing.T) {
	in := engramInput()
	delete(in.FS.(fakeFS), "/var/home/me/.local/share/engram")
	if _, err := BuildSandboxPlan(in); err == nil || !strings.Contains(err.Error(), "store") {
		t.Fatalf("err = %v, want the missing store", err)
	}
}

func TestAnEngramStoreInsideLegatosStateIsRefused(t *testing.T) {
	in := engramInput()
	in.Engram.Store = "/var/home/me/.local/share/legato/engram"
	in.FS.(fakeFS)[in.Engram.Store] = "dir"
	if _, err := BuildSandboxPlan(in); err == nil {
		t.Fatal("a store inside Legato's data folder was mounted")
	}
}

func TestANormalSessionHasNoEngramMounts(t *testing.T) {
	plan, err := BuildSandboxPlan(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range plan.Mounts {
		if strings.Contains(m.Path, "engram") || strings.Contains(m.Dest, "engram") {
			t.Errorf("a normal session mounts %+v", m)
		}
	}
}

func TestEngramWritesWorkInsideTheSandbox(t *testing.T) {
	in := bwrapFixture(t)
	base := filepath.Dir(in.Home)
	real := filepath.Join(base, "opt", "engram")
	_ = os.MkdirAll(filepath.Dir(real), 0o755)
	_ = os.WriteFile(real, []byte("#!/bin/sh\necho \"$ENGRAM_IDENTITY: $*\" >> \"$ENGRAM_HOME/notes\"\necho real-engram\n"), 0o755)
	store := filepath.Join(in.Home, ".local", "share", "engram")
	_ = os.MkdirAll(store, 0o700)
	binDir := filepath.Join(in.RuntimeDir, "sandbox", "ex-1", "bin")
	_ = os.MkdirAll(binDir, 0o700)
	mount := filepath.Join(binDir, "engram")
	_ = os.WriteFile(mount, nil, 0o600)
	in.Engram = SandboxEngram{Binary: real, BinMount: mount, Store: store}

	out, err := runInSandbox(t, in, `export PATH=`+binDir+`:$PATH ENGRAM_HOME=`+store+` ENGRAM_IDENTITY=mneme; engram note hello`)
	if err != nil || !strings.Contains(out, "real-engram") {
		t.Fatalf("err=%v out=%s", err, out)
	}
	notes, _ := os.ReadFile(filepath.Join(store, "notes"))
	if string(notes) != "mneme: note hello\n" {
		t.Fatalf("the store holds %q", notes)
	}
}

func TestASandboxedEngramSessionHasItsBinaryFirstOnPathAndItsStore(t *testing.T) {
	backend := &fakeBackend{}
	var runtime string
	svc, s, mt := sandboxedService(t, backend, func(settings *SandboxSettings) { runtime = settings.Base.RuntimeDir })
	createTask(t, s, "task-1")
	dir := t.TempDir()
	binary := filepath.Join(dir, "engram")
	_ = os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755)
	storeDir := filepath.Join(dir, "store")
	_ = os.MkdirAll(storeDir, 0o700)
	where := testWhere
	where.Binary, where.Store.Root = binary, storeDir
	cli := &fakeEngramCLI{where: where, ids: []engram.Identity{{Name: "mneme"}}, boot: engram.BootResult{Packet: "# mneme\n"}}
	engrams, _ := bootService(cli)
	svc.(interface{ SetEngrams(AgentEngrams) }).SetEngrams(engrams)

	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Engram: "mneme"}); err != nil {
		t.Fatal(err)
	}
	session, _ := s.GetAgentSessionByTaskID(context.Background(), "task-1")
	binDir := filepath.Join(runtime, "sandbox", session.ExecutionID, "bin")
	lines := mt.sentLinesFor("legato-task-1")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "PATH="+binDir+`:"$PATH" SANDBOX`) {
		t.Fatalf("launch = %v", lines)
	}
	plan := backend.plans[0]
	if m, ok := mountOf(plan, storeDir); !ok || m.Kind != sandbox.ReadWrite {
		t.Errorf("store mount = %+v %v", m, ok)
	}
	found := false
	for _, m := range plan.Mounts {
		if m.Dest == filepath.Join(binDir, "engram") && m.Path == binary && m.Kind == sandbox.ReadOnly {
			found = true
		}
	}
	if !found {
		t.Errorf("the engram binary is not mounted at %s: %+v", binDir, plan.Mounts)
	}
}

// The Reeve's store is inside the Reeve's folder, which its session is
// given read-write: the store is still mounted, with the binary.
func TestTheReevesEngramStoreInsideItsFolderIsMountedWithTheBinary(t *testing.T) {
	in := engramInput()
	folder := "/var/home/me/.local/state/legato/reeve"
	in.FS.(fakeFS)[folder] = "dir"
	in.FS.(fakeFS)[folder+"/engram"] = "dir"
	in.Engram.Store = folder + "/engram"
	in.Extra = append(in.Extra, SandboxPath{Path: folder, Access: "write"})
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	m := mounted(plan)
	if got := m["/run/user/1000/legato/sandbox/ex-1/bin/engram"]; got.Kind != sandbox.ReadOnly || got.Path != "/opt/engram/bin/engram" {
		t.Errorf("binary mount = %+v", got)
	}
	if got := m[folder+"/engram"]; got.Kind != sandbox.ReadWrite {
		t.Errorf("store mount = %+v", got)
	}
	if got := m[folder]; got.Kind != sandbox.ReadWrite {
		t.Errorf("folder mount = %+v", got)
	}
}
