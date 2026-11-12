package service

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
)

// feedResources is the configuration of a repository whose agents all get a
// NuGet feed token's folder, as autopilot's do.
func feedResources(t *testing.T) (*RepositoryResources, string) {
	t.Helper()
	feed := t.TempDir()
	return NewRepositoryResources(
		[]config.RepositoryResources{{Repo: "github.com/autopilot-dev/autopilot", Resources: []string{"NuGet feed token"}}},
		[]config.ResourceConfig{{Name: "NuGet feed token", Paths: []config.ResourcePathConfig{{Path: feed, Access: "read"}}}},
	), feed
}

func repositoryResourceService(t *testing.T, adapter AIToolAdapter, inspector CheckoutInspector) (AgentService, *store.Store, *mockTmux, string) {
	t.Helper()
	db := newServiceTestStore(t)
	createTask(t, db, "task-1")
	resources, feed := feedResources(t)
	mt := newMockTmux()
	svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{
		Adapter:              adapter,
		CheckoutInspector:    inspector,
		RepositoryResources:  resources,
		DisablePeerAwareness: true,
	})
	return svc, db, mt, feed
}

func originIs(dir, remote string) CheckoutInspectFunc {
	return func(d string) (*store.CodeContext, error) {
		if d != dir {
			return nil, nil
		}
		return &store.CodeContext{Repo: remote}, nil
	}
}

func TestARepositorysResourcesReachAnAgentInAnyWorktreeOfIt(t *testing.T) {
	for _, remote := range []string{"git@github.com:autopilot-dev/autopilot.git", "https://github.com/Autopilot-Dev/autopilot.git"} {
		t.Run(remote, func(t *testing.T) {
			worktree := t.TempDir()
			svc, _, mt, feed := repositoryResourceService(t, grantingAdapter{&fakeAdapter{name: "chimera"}}, originIs(worktree, remote))
			if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkingDir: worktree}); err != nil {
				t.Fatal(err)
			}
			prompt := spawnedLaunchPrompt(t, mt, "task-1")
			if !strings.Contains(prompt, "NuGet feed token") || !strings.Contains(prompt, "`"+feed+"` (read only)") {
				t.Fatalf("launch prompt does not list the repository's resource:\n%s", prompt)
			}
			if launch := strings.Join(mt.sentLinesFor("legato-task-1"), "\n"); !strings.Contains(launch, "READ["+feed+"]") {
				t.Fatalf("the resource was not granted on the command line: %q", launch)
			}
		})
	}
}

func TestARepositorysResourcesReachAnAgentInAFreshCloneOfIt(t *testing.T) {
	svc, _, mt, feed := repositoryResourceService(t, &fakeAdapter{name: "claude-code"}, nil)
	svc.(*agentService).SetRepositories(&recordingCloner{root: t.TempDir()})
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{Repo: "Autopilot-Dev/autopilot"}); err != nil {
		t.Fatal(err)
	}
	if prompt := spawnedLaunchPrompt(t, mt, "task-1"); !strings.Contains(prompt, "`"+feed+"` (read only)") {
		t.Fatalf("launch prompt does not list the repository's resource:\n%s", prompt)
	}
}

func TestAnAgentInAnotherRepositoryDoesNotGetThem(t *testing.T) {
	worktree := t.TempDir()
	svc, _, mt, _ := repositoryResourceService(t, &fakeAdapter{name: "claude-code"}, originIs(worktree, "git@github.com:acme/web.git"))
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkingDir: worktree}); err != nil {
		t.Fatal(err)
	}
	if prompt := spawnedLaunchPrompt(t, mt, "task-1"); strings.Contains(prompt, "NuGet feed token") {
		t.Fatalf("an agent in another repository got the resource:\n%s", prompt)
	}
}

func TestRepositoryResourcesMergeWithTickedOnesWithoutDuplicates(t *testing.T) {
	worktree := t.TempDir()
	svc, _, mt, feed := repositoryResourceService(t, &fakeAdapter{name: "claude-code"}, originIs(worktree, "autopilot-dev/autopilot"))
	notes := t.TempDir()
	ticked := []Resource{
		{Name: "nuget feed token", Paths: []ResourcePath{{Path: feed, Access: "read"}}},
		{Name: "Notes", Paths: []ResourcePath{{Path: notes, Access: "write"}}},
	}
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{WorkingDir: worktree, Resources: ticked}); err != nil {
		t.Fatal(err)
	}
	prompt := spawnedLaunchPrompt(t, mt, "task-1")
	if strings.Count(strings.ToLower(prompt), "nuget feed token") != 1 || !strings.Contains(prompt, "Notes") {
		t.Fatalf("launch prompt should list each resource once:\n%s", prompt)
	}
	attached, err := svc.(*agentService).AttachedResources(ctx, "task-1")
	if err != nil || len(attached) != 2 {
		t.Fatalf("attached = %v, %v; want the two resources once each", attached, err)
	}
}

// A restart starts from the resources the person chose; the repository's are
// worked out again by the spawn, so a mapping removed meanwhile is gone.
func TestTheResourcesARestartCarriesAreOnlyTheChosenOnes(t *testing.T) {
	worktree := t.TempDir()
	svc, _, _, _ := repositoryResourceService(t, &fakeAdapter{name: "claude-code"}, originIs(worktree, "autopilot-dev/autopilot"))
	notes := t.TempDir()
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{WorkingDir: worktree,
		Resources: []Resource{{Name: "Notes", Paths: []ResourcePath{{Path: notes, Access: "read"}}}}}); err != nil {
		t.Fatal(err)
	}
	agents := svc.(*agentService)
	attached, _ := agents.AttachedResources(ctx, "task-1")
	if !slices.Equal(attached, []string{"Notes", "NuGet feed token"}) {
		t.Fatalf("attached = %v, want the chosen one and the repository's", attached)
	}
	chosen, err := agents.ChosenResources(ctx, "task-1")
	if err != nil || !slices.Equal(chosen, []string{"Notes"}) {
		t.Fatalf("chosen = %v, %v; want only Notes", chosen, err)
	}
}

func TestRepositoryResourcesAreOnlyForTheirRepository(t *testing.T) {
	resources := NewRepositoryResources(
		[]config.RepositoryResources{{Repo: "github.com/acme/web", Resources: []string{"Gone"}}},
		[]config.ResourceConfig{{Name: "Gone", Paths: []config.ResourcePathConfig{{Path: filepath.Join(t.TempDir(), "missing"), Access: "read"}}}},
	)
	got, err := resources.ResourcesForRepository(context.Background(), "github.com/acme/web")
	if err != nil || len(got) != 1 {
		t.Fatalf("resources = %v, %v; want the repository's one (the spawn, not the source, leaves out missing paths)", got, err)
	}
	if got, _ := resources.ResourcesForRepository(context.Background(), "github.com/acme/other"); len(got) != 0 {
		t.Fatalf("another repository got %v", got)
	}
}

// A sandboxed agent with nothing ticked sees its repository's resource, at
// the resource's access.
func TestARepositorysResourceIsMountedInTheSandboxAtItsAccess(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	resources, feed := feedResources(t)
	worktree := t.TempDir()
	agents := svc.(*agentService)
	agents.repoResources = resources
	agents.checkoutInspector = originIs(worktree, "git@github.com:autopilot-dev/autopilot.git")
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkingDir: worktree}); err != nil {
		t.Fatal(err)
	}
	if m, ok := mountOf(backend.plans[0], feed); !ok || m.Kind != sandbox.ReadOnly {
		t.Fatalf("mount of %s = %+v, %v; want read-only", feed, m, ok)
	}
}
