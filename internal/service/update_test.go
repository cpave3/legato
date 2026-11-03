package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/selfupdate"
	"github.com/cpave3/legato/internal/engine/store"
)

// fakeReleases is GitHub's latest release, and records what was installed.
type fakeReleases struct {
	mu         sync.Mutex
	latest     selfupdate.Release
	latestErr  error
	installErr error
	checks     int
	installed  []string // versions, in order
}

func (f *fakeReleases) Latest(context.Context) (selfupdate.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return f.latest, f.latestErr
}

func (f *fakeReleases) Install(_ context.Context, rel selfupdate.Release, _ string, _ func(context.Context, string) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.installErr != nil {
		return f.installErr
	}
	f.installed = append(f.installed, rel.Version)
	return nil
}

func (f *fakeReleases) installs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.installed...)
}

func (f *fakeReleases) checkCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks
}

func release(v string) selfupdate.Release {
	return selfupdate.Release{Version: v, URL: "https://github.com/cpave3/legato/releases/tag/" + v}
}

// updaterFixture is an UpdateService running v1.2.0, with agents that are
// busy while busy is set, in a process that restarts when restarts is set.
type updaterFixture struct {
	svc       *UpdateService
	db        *store.Store
	releases  *fakeReleases
	mu        sync.Mutex
	busy      bool
	restarts  bool
	restarted int
}

func newUpdater(t *testing.T, current string, opts ...func(*UpdateOptions)) *updaterFixture {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &updaterFixture{releases: &fakeReleases{latest: release("v1.3.0")}, restarts: true, db: db}
	o := UpdateOptions{
		Store: db, Releases: f.releases, Current: current, Executable: "/opt/legato",
		Busy: func(context.Context) (bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.busy, nil
		},
		Restart: func() bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.restarted++
			return f.restarts
		},
		FirstCheckAfter: time.Millisecond, CheckEvery: time.Hour,
		QuietPoll: time.Millisecond, MaxWait: time.Hour,
	}
	for _, opt := range opts {
		opt(&o)
	}
	f.svc = NewUpdateService(o)
	return f
}

func (f *updaterFixture) setBusy(busy bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.busy = busy
}

func (f *updaterFixture) restartCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restarted
}

func TestABuildFromSourceNeverUpdates(t *testing.T) {
	f := newUpdater(t, "dev")
	ctx := context.Background()

	if _, err := f.svc.Check(ctx); !errors.Is(err, ErrNotARelease) {
		t.Errorf("Check err = %v, want ErrNotARelease", err)
	}
	if err := f.svc.Apply(ctx); !errors.Is(err, ErrNotARelease) {
		t.Errorf("Apply err = %v, want ErrNotARelease", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	f.svc.Run(runCtx)
	if f.releases.checkCount() != 0 || len(f.releases.installs()) != 0 {
		t.Errorf("asked GitHub %d times, installed %v", f.releases.checkCount(), f.releases.installs())
	}
	if st := f.svc.Status(ctx); st.Release || st.Version != "dev" {
		t.Errorf("status = %+v", st)
	}
}

func TestCheckFindsANewerRelease(t *testing.T) {
	f := newUpdater(t, "v1.2.0")

	st, err := f.svc.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !st.Available || st.Latest != "v1.3.0" || st.LatestURL != "https://github.com/cpave3/legato/releases/tag/v1.3.0" || st.State != UpdateAvailable {
		t.Errorf("status = %+v", st)
	}
	if st.CheckedAt.IsZero() {
		t.Error("no time of the check")
	}
	if len(f.releases.installs()) != 0 {
		t.Error("checking installed")
	}
}

func TestCheckFindsNothingNewer(t *testing.T) {
	for _, latest := range []string{"v1.2.0", "v1.1.9"} {
		f := newUpdater(t, "v1.2.0")
		f.releases.latest = release(latest)

		st, err := f.svc.Check(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if st.Available || st.State != UpdateUpToDate {
			t.Errorf("latest %s: status = %+v", latest, st)
		}
	}
}

func TestAFailedCheckIsReported(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	f.releases.latestErr = errors.New("GitHub answered 403")

	if _, err := f.svc.Check(context.Background()); err == nil {
		t.Fatal("no error")
	}
	if st := f.svc.Status(context.Background()); st.Error != "GitHub answered 403" {
		t.Errorf("status = %+v", st)
	}
}

func TestUpdatesAskByDefaultAndKeepTheirMode(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	ctx := context.Background()

	if st := f.svc.Status(ctx); st.Mode != UpdateModeAsk {
		t.Errorf("mode = %q, want ask", st.Mode)
	}
	if err := f.svc.SetMode(ctx, "auto"); err != nil {
		t.Fatal(err)
	}
	if st := f.svc.Status(ctx); st.Mode != UpdateModeAuto {
		t.Errorf("mode = %q, want auto", st.Mode)
	}
	if err := f.svc.SetMode(ctx, "sometimes"); !errors.Is(err, ErrUnknownUpdateMode) {
		t.Errorf("err = %v, want ErrUnknownUpdateMode", err)
	}
}

func TestApplyInstallsTheNewestReleaseAndRestarts(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	ctx := context.Background()

	if err := f.svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}

	if got := f.releases.installs(); len(got) != 1 || got[0] != "v1.3.0" {
		t.Errorf("installed %v, want v1.3.0", got)
	}
	if f.restartCount() != 1 {
		t.Errorf("restarted %d times, want once", f.restartCount())
	}
	if st := f.svc.Status(ctx); st.State != UpdateRestarting {
		t.Errorf("state = %q, want restarting", st.State)
	}
}

func TestAnUpdateWaitsForARestartWhereLegatoCannotRestartItself(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	f.restarts = false
	ctx := context.Background()

	if err := f.svc.Apply(ctx); err != nil {
		t.Fatal(err)
	}

	st := f.svc.Status(ctx)
	if st.State != UpdateInstalled || st.Available {
		t.Errorf("status = %+v, want installed and nothing more to install", st)
	}
	if _, err := f.svc.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if st := f.svc.Status(ctx); st.State != UpdateInstalled || st.Available {
		t.Errorf("after another check: %+v", st)
	}
	if err := f.svc.Apply(ctx); !errors.Is(err, ErrNoUpdate) {
		t.Errorf("second Apply err = %v, want ErrNoUpdate", err)
	}
}

func TestAFailedInstallIsReported(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	f.releases.installErr = errors.New("legato-linux-amd64 of v1.3.0 does not match its checksum")
	ctx := context.Background()

	if err := f.svc.Apply(ctx); err == nil {
		t.Fatal("no error")
	}

	st := f.svc.Status(ctx)
	if st.State != UpdateFailed || st.Error != "legato-linux-amd64 of v1.3.0 does not match its checksum" || !st.Available {
		t.Errorf("status = %+v", st)
	}
	if f.restartCount() != 0 {
		t.Error("restarted after a failed install")
	}
}

func TestApplyWithNothingNewer(t *testing.T) {
	f := newUpdater(t, "v1.3.0")

	if err := f.svc.Apply(context.Background()); !errors.Is(err, ErrNoUpdate) {
		t.Errorf("err = %v, want ErrNoUpdate", err)
	}
	if len(f.releases.installs()) != 0 {
		t.Error("installed")
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func running(t *testing.T, f *updaterFixture) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.svc.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func TestInAskModeAnUpdateWaitsForThePerson(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	running(t, f)

	eventually(t, "the update to be found", func() bool { return f.svc.Status(context.Background()).Available })
	time.Sleep(20 * time.Millisecond)

	if got := f.releases.installs(); len(got) != 0 {
		t.Errorf("installed %v without being asked", got)
	}
}

func TestInAutoModeAnUpdateWaitsForTheAgentsToFinishTheirTurns(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	if err := f.svc.SetMode(context.Background(), UpdateModeAuto); err != nil {
		t.Fatal(err)
	}
	f.setBusy(true)
	running(t, f)

	eventually(t, "the update to wait", func() bool { return f.svc.Status(context.Background()).State == UpdateWaiting })
	time.Sleep(20 * time.Millisecond)
	if got := f.releases.installs(); len(got) != 0 {
		t.Fatalf("installed %v while an agent was busy", got)
	}

	f.setBusy(false)
	eventually(t, "the install", func() bool { return len(f.releases.installs()) == 1 })
	eventually(t, "the restart", func() bool { return f.restartCount() == 1 })
}

func TestInAutoModeAnUpdateStopsWaitingForAgentsInTheEnd(t *testing.T) {
	f := newUpdater(t, "v1.2.0", func(o *UpdateOptions) { o.MaxWait = 20 * time.Millisecond })
	if err := f.svc.SetMode(context.Background(), UpdateModeAuto); err != nil {
		t.Fatal(err)
	}
	f.setBusy(true)
	running(t, f)

	eventually(t, "the install", func() bool { return len(f.releases.installs()) == 1 })
}

func TestSwitchingToAutoInstallsAWaitingUpdate(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	running(t, f)
	eventually(t, "the update to be found", func() bool { return f.svc.Status(context.Background()).Available })

	if err := f.svc.SetMode(context.Background(), UpdateModeAuto); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the install", func() bool { return len(f.releases.installs()) == 1 })
}

func TestWithUpdatesOffNothingIsChecked(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	if err := f.svc.SetMode(context.Background(), UpdateModeOff); err != nil {
		t.Fatal(err)
	}
	running(t, f)
	time.Sleep(30 * time.Millisecond)

	if n := f.releases.checkCount(); n != 0 {
		t.Errorf("asked GitHub %d times", n)
	}
}
