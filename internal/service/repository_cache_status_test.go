package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// statusLog records what the cache reports, in order.
type statusLog struct {
	mu   sync.Mutex
	seen []RepositoryCacheStatus
}

func (l *statusLog) record(s RepositoryCacheStatus) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, s)
}

func (l *statusLog) states(repo string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, s := range l.seen {
		if s.Repo == repo && (len(out) == 0 || out[len(out)-1] != s.State) {
			out = append(out, s.State)
		}
	}
	return out
}

func TestTheCacheSaysWhatItHoldsAndReportsACopy(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	log := &statusLog{}
	f.svc.SetCacheListener(log.record)

	if got := f.svc.CacheStatus("acme/widget"); got.State != CacheMissing || got.FetchedAt != nil {
		t.Fatalf("before a copy: %+v", got)
	}
	if _, err := f.svc.Ensure(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	got := f.svc.CacheStatus("acme/widget")
	if got.State != CacheReady || got.FetchedAt == nil || !got.FetchedAt.Equal(*f.now) {
		t.Fatalf("after a copy: %+v", got)
	}
	if states := log.states("acme/widget"); len(states) != 2 || states[0] != CacheCopying || states[1] != CacheReady {
		t.Fatalf("reported %v, want copying then ready", states)
	}

	*f.now = f.now.Add(time.Hour)
	if _, err := f.svc.Ensure(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	if states := log.states("acme/widget"); len(states) != 4 || states[2] != CacheFetching || states[3] != CacheReady {
		t.Fatalf("reported %v, want a fetch after the copy", states)
	}
}

func TestACopyThatFailsSaysWhy(t *testing.T) {
	gh := newRepoUpstream(t)
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	log := &statusLog{}
	f.svc.SetCacheListener(log.record)
	if _, err := f.svc.Ensure(context.Background(), "acme/missing"); err == nil {
		t.Fatal("cloned a repository that does not exist")
	}
	got := f.svc.CacheStatus("acme/missing")
	if got.State != CacheFailed || got.Error == "" {
		t.Fatalf("after a failed copy: %+v", got)
	}
	if states := log.states("acme/missing"); len(states) != 2 || states[1] != CacheFailed {
		t.Fatalf("reported %v", states)
	}
}

func TestWarmCopiesEachRepositoryItCanAndSkipsTheRest(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget", "acme/gadget")
	tokens := &refusingTokens{fakeTokens: fakeTokens{gh: gh}, refused: map[string]bool{"acme/secret": true}}
	f := newRepoFixture(t, tokens, gh)

	f.svc.Warm(context.Background(), []string{"acme/widget", "acme/secret", "acme/gadget", "acme/widget", ""})

	for _, repo := range []string{"acme/widget", "acme/gadget"} {
		if got := f.svc.CacheStatus(repo); got.State != CacheReady {
			t.Errorf("%s after warming: %+v", repo, got)
		}
	}
	if got := f.svc.CacheStatus("acme/secret"); got.State != CacheMissing {
		t.Errorf("a repository the credential does not list: %+v", got)
	}
	if n := f.git.clones.Load(); n != 2 {
		t.Fatalf("%d clones, want 2", n)
	}
}

// refusingTokens refuses the repositories in refused, as a credential that
// does not list them.
type refusingTokens struct {
	fakeTokens
	refused map[string]bool
}

func (r *refusingTokens) CanRead(repo string) error {
	if r.refused[repo] {
		return errors.New(repo + " is not listed")
	}
	return nil
}
