package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/credential"
)

// After an agent pushes, the cache fetches, so it has the agent's commits
// whatever its age.
func TestAPushFetchesTheRepositoryCache(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	ctx := context.Background()
	path, err := f.svc.Ensure(ctx, "acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	newer := gh.push(t, "acme/widget", "feature/x")

	f.svc.FetchPushed(ctx, "acme/widget")
	if got := gitIn(t, "", "--git-dir", path, "rev-parse", "refs/heads/feature/x"); got != newer {
		t.Fatalf("feature/x in the cache = %s, want the pushed %s", got, newer)
	}
}

// A push to a repository Legato keeps no copy of makes none.
func TestAPushToARepositoryWithNoCacheMakesNone(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	f.svc.FetchPushed(context.Background(), "acme/widget")
	if _, err := os.Stat(filepath.Join(f.data, "repos", "acme", "widget.git")); !os.IsNotExist(err) {
		t.Fatalf("a cache was made: %v", err)
	}
	if f.git.clones.Load() != 0 {
		t.Fatal("a push cloned the repository")
	}
}

// The credential channels pass each push on to whoever listens.
func TestTheCredentialChannelsPassPushesOn(t *testing.T) {
	var got []string
	c := &CredentialChannels{}
	c.Pushed(context.Background(), credential.Execution{}, "github", "acme/widget", []string{"refs/heads/legato/x"})
	c.SetPushListener(func(repo string) { got = append(got, repo) })
	c.Pushed(context.Background(), credential.Execution{}, "github", "acme/widget", []string{"refs/heads/legato/x"})
	if len(got) != 1 || got[0] != "acme/widget" {
		t.Fatalf("pushes heard = %v", got)
	}
}
