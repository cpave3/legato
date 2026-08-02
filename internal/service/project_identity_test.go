package service

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestProjectIdentityPrefersNormalizedCodeContextRepository(t *testing.T) {
	called := false
	inspector := CheckoutInspectFunc(func(string) (*store.CodeContext, error) {
		called = true
		return &store.CodeContext{Repo: "other/repo"}, nil
	})

	identity, err := NewProjectIdentityResolver(inspector).Resolve(
		&store.CodeContext{Repo: "  GitHub.com/Acme/Widget.git  "}, "/not/a/checkout",
	)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Key != "repo:github.com/acme/widget" || identity.Label != "github.com/acme/widget" {
		t.Fatalf("identity = %+v", identity)
	}
	if called {
		t.Fatal("checkout inspector called despite durable repository identity")
	}
}

func TestProjectIdentityUsesCheckoutInspectorRepository(t *testing.T) {
	inspector := CheckoutInspectFunc(func(directory string) (*store.CodeContext, error) {
		if directory != "/checkout" {
			t.Fatalf("directory = %q", directory)
		}
		return &store.CodeContext{Repo: "Acme/Widget"}, nil
	})

	identity, err := NewProjectIdentityResolver(inspector).Resolve(nil, "/checkout")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Key != "repo:github.com/acme/widget" {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestProjectIdentityFallsBackWhenNoRemoteCanBeInspected(t *testing.T) {
	repo := initIdentityTestRepository(t)
	inspector := CheckoutInspectFunc(func(string) (*store.CodeContext, error) {
		return nil, errors.New("origin is not configured")
	})

	identity, err := NewProjectIdentityResolver(inspector).Resolve(nil, repo)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Key == "" {
		t.Fatal("expected common-directory identity")
	}
}

func TestProjectIdentityGitCommonDirectoryConvergesForSiblingWorktrees(t *testing.T) {
	repo := initIdentityTestRepository(t)
	worktree := filepath.Join(t.TempDir(), "sibling")
	runIdentityGit(t, repo, "worktree", "add", "-b", "sibling", worktree)
	resolver := NewProjectIdentityResolver(nil)

	first, err := resolver.Resolve(nil, repo)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(nil, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if first.Key == "" || first.Key != second.Key {
		t.Fatalf("main = %+v, sibling = %+v", first, second)
	}
}

func TestProjectIdentityKeepsUnrelatedRepositoriesIsolated(t *testing.T) {
	firstRepo := initIdentityTestRepository(t)
	secondRepo := initIdentityTestRepository(t)
	resolver := NewProjectIdentityResolver(nil)

	first, err := resolver.Resolve(nil, firstRepo)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(nil, secondRepo)
	if err != nil {
		t.Fatal(err)
	}
	if first.Key == second.Key {
		t.Fatalf("unrelated repositories share %q", first.Key)
	}
}

func TestProjectIdentityDoesNotInventScopeWhenUnresolved(t *testing.T) {
	identity, err := NewProjectIdentityResolver(nil).Resolve(nil, t.TempDir())
	if err == nil {
		t.Fatal("expected resolution error")
	}
	if identity.Key != "" || identity.Label != "" {
		t.Fatalf("identity = %+v", identity)
	}
}

func initIdentityTestRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runIdentityGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runIdentityGit(t, dir, "add", "README")
	runIdentityGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	return dir
}

func runIdentityGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
