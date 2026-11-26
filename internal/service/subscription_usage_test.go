package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// fakeUsageReader answers with what the test sets and counts its reads.
type fakeUsageReader struct {
	provider string
	mu       sync.Mutex
	reads    int
	usage    ProviderUsage
	err      error
}

func (f *fakeUsageReader) Provider() string { return f.provider }

func (f *fakeUsageReader) Read(context.Context) (ProviderUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return f.usage, f.err
}

func (f *fakeUsageReader) set(usage ProviderUsage, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage, f.err = usage, err
}

func (f *fakeUsageReader) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

type usageFixture struct {
	svc    *SubscriptionUsageService
	db     *store.Store
	now    time.Time
	claude *fakeUsageReader
	codex  *fakeUsageReader
	change int
}

func newUsageFixture(t *testing.T) *usageFixture {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &usageFixture{
		db:     db,
		now:    time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC),
		claude: &fakeUsageReader{provider: UsageProviderClaude},
		codex:  &fakeUsageReader{provider: UsageProviderCodex},
	}
	f.svc = NewSubscriptionUsageService(SubscriptionUsageOptions{
		Store:        db,
		Readers:      []SubscriptionUsageReader{f.claude, f.codex},
		PollInterval: 5 * time.Minute,
		Now:          func() time.Time { return f.now },
		OnChange:     func() { f.change++ },
	})
	return f
}

func claudeWindows(weekly float64) []UsageWindow {
	return []UsageWindow{
		{Meter: UsageMeterFiveHour, UsedPercent: 42, ResetsAt: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)},
		{Meter: UsageMeterWeekly, UsedPercent: weekly, ResetsAt: time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)},
	}
}

func (f *usageFixture) snapshot(t *testing.T) map[string]ProviderUsage {
	t.Helper()
	snap, err := f.svc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ProviderUsage{}
	for _, p := range snap.Providers {
		out[p.Provider] = p
	}
	if len(snap.Providers) != 2 || snap.Providers[0].Provider != UsageProviderClaude {
		t.Fatalf("providers = %+v, want claude then codex", snap.Providers)
	}
	return out
}

func TestUsageNeverReadIsUnavailableAndASnapshotNeverReads(t *testing.T) {
	f := newUsageFixture(t)
	got := f.snapshot(t)
	if got["claude"].State != UsageUnavailable || got["claude"].Reason != UsageReasonNotRead {
		t.Fatalf("claude = %+v", got["claude"])
	}
	if f.claude.count() != 0 || f.codex.count() != 0 {
		t.Fatal("a snapshot asked a provider")
	}
}

func TestUsageRefreshReadsAndCachesEachProvider(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{Windows: claudeWindows(91.5), Plan: "max", Source: "endpoint"}, nil)
	codexAsOf := f.now.Add(-time.Minute)
	f.codex.set(ProviderUsage{Windows: []UsageWindow{{Meter: UsageMeterWeekly, UsedPercent: 5, WindowMinutes: 10080}}, AsOf: &codexAsOf, Source: "logs"}, nil)

	f.svc.Refresh(context.Background())

	got := f.snapshot(t)
	claude := got["claude"]
	if claude.State != UsageOK || claude.Stale || claude.Reason != "" || len(claude.Windows) != 2 || claude.Windows[1].UsedPercent != 91.5 {
		t.Fatalf("claude = %+v", claude)
	}
	if claude.AsOf == nil || !claude.AsOf.Equal(f.now) || claude.Plan != "max" {
		t.Fatalf("claude as of = %v, plan %q; want now (the endpoint is live)", claude.AsOf, claude.Plan)
	}
	if codex := got["codex"]; codex.AsOf == nil || !codex.AsOf.Equal(codexAsOf) || codex.State != UsageOK {
		t.Fatalf("codex = %+v; want as of its log event", codex)
	}
	if f.change == 0 {
		t.Fatal("no change was announced")
	}

	// Another service on the same database (another process) serves the
	// same cache without reading.
	other := NewSubscriptionUsageService(SubscriptionUsageOptions{
		Store: f.db, Readers: []SubscriptionUsageReader{&fakeUsageReader{provider: "claude"}, &fakeUsageReader{provider: "codex"}},
		PollInterval: 5 * time.Minute, Now: func() time.Time { return f.now },
	})
	snap, err := other.Snapshot(context.Background())
	if err != nil || snap.Providers[0].State != UsageOK || snap.Providers[0].Windows[1].UsedPercent != 91.5 {
		t.Fatalf("other process: %+v %v", snap, err)
	}
}

func TestUsageRefreshIsDebouncedToOnceAMinute(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{Windows: claudeWindows(10)}, nil)
	f.svc.Refresh(context.Background())
	f.now = f.now.Add(30 * time.Second)
	f.svc.Refresh(context.Background())
	if f.claude.count() != 1 {
		t.Fatalf("reads = %d within a minute, want 1", f.claude.count())
	}
	f.now = f.now.Add(31 * time.Second)
	f.svc.Refresh(context.Background())
	if f.claude.count() != 2 {
		t.Fatalf("reads = %d after a minute, want 2", f.claude.count())
	}
}

func TestUsagePollsOnTheInterval(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{Windows: claudeWindows(10)}, nil)
	f.svc.Poll(context.Background())
	f.now = f.now.Add(4 * time.Minute)
	f.svc.Poll(context.Background())
	if f.claude.count() != 1 {
		t.Fatalf("reads = %d before the interval, want 1", f.claude.count())
	}
	f.now = f.now.Add(time.Minute)
	f.svc.Poll(context.Background())
	if f.claude.count() != 2 {
		t.Fatalf("reads = %d at the interval, want 2", f.claude.count())
	}
}

func TestAFailedReadKeepsTheLastReadingAsStale(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{Windows: claudeWindows(50)}, nil)
	f.svc.Refresh(context.Background())
	readAt := f.now

	f.now = f.now.Add(5 * time.Minute)
	f.claude.set(ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonEndpointChanged})
	f.svc.Poll(context.Background())

	claude := f.snapshot(t)["claude"]
	if claude.State != UsageStale || !claude.Stale || claude.Reason != UsageReasonEndpointChanged {
		t.Fatalf("claude = %+v", claude)
	}
	if len(claude.Windows) != 2 || claude.Windows[1].UsedPercent != 50 || !claude.AsOf.Equal(readAt) {
		t.Fatalf("the last reading was not kept: %+v", claude)
	}
}

func TestAFailureWithNothingToKeepIsUnavailableWithItsReason(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonNotSignedIn})
	f.codex.set(ProviderUsage{}, errors.New("anything else"))
	f.svc.Refresh(context.Background())
	got := f.snapshot(t)
	if got["claude"].State != UsageUnavailable || got["claude"].Reason != UsageReasonNotSignedIn || len(got["claude"].Windows) != 0 {
		t.Fatalf("claude = %+v", got["claude"])
	}
	if got["codex"].State != UsageUnavailable || got["codex"].Reason != UsageReasonError {
		t.Fatalf("codex = %+v", got["codex"])
	}
}

func TestSigningOutDropsTheLastReading(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{Windows: claudeWindows(50)}, nil)
	f.svc.Refresh(context.Background())
	f.now = f.now.Add(5 * time.Minute)
	f.claude.set(ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonNotSignedIn})
	f.svc.Poll(context.Background())
	if claude := f.snapshot(t)["claude"]; claude.State != UsageUnavailable || len(claude.Windows) != 0 {
		t.Fatalf("claude = %+v", claude)
	}
}

func TestAnOldReadingIsStale(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{Windows: claudeWindows(50)}, nil)
	f.svc.Refresh(context.Background())

	f.now = f.now.Add(10 * time.Minute)
	if claude := f.snapshot(t)["claude"]; claude.Stale {
		t.Fatalf("at two intervals: %+v", claude)
	}
	f.now = f.now.Add(time.Second)
	if claude := f.snapshot(t)["claude"]; !claude.Stale || claude.State != UsageStale {
		t.Fatalf("past two intervals: %+v", claude)
	}
}

func TestARateLimitWaitsAsTheProviderAsked(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonRateLimited, RetryAfter: 12 * time.Minute})
	f.svc.Poll(context.Background())
	f.claude.set(ProviderUsage{Windows: claudeWindows(10)}, nil)

	for _, step := range []time.Duration{5 * time.Minute, 5 * time.Minute} {
		f.now = f.now.Add(step)
		f.svc.Poll(context.Background())
		f.svc.Refresh(context.Background())
	}
	if f.claude.count() != 1 {
		t.Fatalf("reads = %d during the wait, want 1", f.claude.count())
	}
	f.now = f.now.Add(2 * time.Minute)
	f.svc.Poll(context.Background())
	if f.claude.count() != 2 {
		t.Fatalf("reads = %d after the wait, want 2", f.claude.count())
	}
}

func TestRepeatedFailuresBackOffUpToHalfAnHour(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{}, &UsageUnavailableError{Reason: UsageReasonUnreachable})
	var gaps []time.Duration
	last := f.now
	for f.now.Sub(last) < 2*time.Hour && len(gaps) < 6 {
		before := f.claude.count()
		f.svc.Poll(context.Background())
		if f.claude.count() > before {
			if before > 0 {
				gaps = append(gaps, f.now.Sub(last))
			}
			last = f.now
		}
		f.now = f.now.Add(time.Minute)
	}
	want := []time.Duration{10 * time.Minute, 20 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	if len(gaps) < len(want) {
		t.Fatalf("gaps = %v", gaps)
	}
	for i, w := range want {
		if gaps[i] != w {
			t.Fatalf("gaps = %v, want %v…", gaps, want)
		}
	}
}

// A restarted Legato goes on from the kept readings' schedule instead of
// asking every provider again at once.
func TestARestartedPollerWaitsOutTheKeptReadingsInterval(t *testing.T) {
	f := newUsageFixture(t)
	f.claude.set(ProviderUsage{Windows: claudeWindows(10)}, nil)
	f.codex.set(ProviderUsage{Windows: claudeWindows(10)}, nil)
	f.svc.Refresh(context.Background())

	restarted := &fakeUsageReader{provider: UsageProviderClaude, usage: ProviderUsage{Windows: claudeWindows(20)}}
	newCodex := &fakeUsageReader{provider: UsageProviderCodex, usage: ProviderUsage{Windows: claudeWindows(20)}}
	svc := NewSubscriptionUsageService(SubscriptionUsageOptions{
		Store: f.db, Readers: []SubscriptionUsageReader{restarted, newCodex},
		PollInterval: 5 * time.Minute, Now: func() time.Time { return f.now },
	})
	f.now = f.now.Add(2 * time.Minute)
	svc.Poll(context.Background())
	if restarted.count() != 0 {
		t.Fatalf("reads = %d two minutes after the kept reading", restarted.count())
	}
	f.now = f.now.Add(3 * time.Minute)
	svc.Poll(context.Background())
	if restarted.count() != 1 {
		t.Fatalf("reads = %d at the interval, want 1", restarted.count())
	}
}
