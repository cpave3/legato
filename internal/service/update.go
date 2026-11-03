package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/selfupdate"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/version"
)

// How this Legato takes a new release.
const (
	// UpdateModeAuto installs a new release once no agent is in the middle
	// of a turn, and restarts on it.
	UpdateModeAuto = "auto"
	// UpdateModeAsk says a new release is out, and installs it when the
	// person asks.
	UpdateModeAsk = "ask"
	// UpdateModeOff never looks for one.
	UpdateModeOff = "off"
)

// Where an update stands.
const (
	UpdateUnchecked  = "unchecked"
	UpdateUpToDate   = "up_to_date"
	UpdateAvailable  = "available"
	UpdateWaiting    = "waiting"    // for the agents to finish their turns
	UpdateInstalling = "installing" //
	UpdateRestarting = "restarting" // on the new binary
	UpdateInstalled  = "installed"  // and waiting for Legato to be restarted
	UpdateFailed     = "failed"
)

var (
	// ErrNotARelease is a Legato built from source, which never updates
	// itself.
	ErrNotARelease = errors.New("this Legato is built from source, so it does not update itself")
	// ErrUnknownUpdateMode is a mode that is not auto, ask or off.
	ErrUnknownUpdateMode = errors.New("not an update mode")
	// ErrNoUpdate is an update asked for when none is available.
	ErrNoUpdate = errors.New("no newer release is available")
)

const updateModeKey = "updates.mode"

// UpdateReleases finds and installs releases (selfupdate.Client).
type UpdateReleases interface {
	Latest(ctx context.Context) (selfupdate.Release, error)
	Install(ctx context.Context, rel selfupdate.Release, exe string, verify func(ctx context.Context, binary string) error) error
}

// UpdateOptions are what an UpdateService works with.
type UpdateOptions struct {
	Store    TintStore // keeps the mode
	Releases UpdateReleases
	Bus      *events.Bus // told of each change (EventUpdateChanged); may be nil
	// Current is this binary's version (version.String()).
	Current string
	// Executable is the running binary, which an update replaces.
	Executable string
	// Busy reports whether an agent is in the middle of a turn; an
	// automatic update waits until none is.
	Busy func(ctx context.Context) (bool, error)
	// Restart starts this Legato again on the binary just installed, after
	// a clean shutdown, and reports whether it will. When it cannot (no
	// service manager to start it again), the update waits for the person
	// to restart Legato.
	Restart func() bool
	// Verify checks a downloaded binary before it is installed; nil runs it
	// and asks its version.
	Verify func(want string) func(ctx context.Context, binary string) error

	// Tools are command-line tools kept current beside Legato (st), under
	// the same mode: checked with Legato, installed at once in auto mode
	// (nothing holds them open), and on the person's word in ask mode.
	Tools []*ToolUpdater

	FirstCheckAfter time.Duration // after Run starts
	CheckEvery      time.Duration
	QuietPoll       time.Duration // how often an automatic update asks Busy
	MaxWait         time.Duration // for the agents, before updating anyway
}

// UpdateStatus is where this Legato's updates stand.
type UpdateStatus struct {
	Version   string    `json:"version"`
	Release   bool      `json:"release"`
	Mode      string    `json:"mode"`
	State     string    `json:"state"`
	Available bool      `json:"available"`
	Latest    string    `json:"latest,omitempty"`
	LatestURL string    `json:"latest_url,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	Error     string    `json:"error,omitempty"`
	// Tools are the tools kept current beside Legato.
	Tools []ToolStatus `json:"tools"`
}

// UpdateService keeps a released Legato on the newest stable release:
// it looks for one now and then, and installs it when the person asks or,
// in auto mode, when no agent is in the middle of a turn.
type UpdateService struct {
	opts UpdateOptions

	mu        sync.Mutex
	state     string
	latest    selfupdate.Release
	checkedAt time.Time
	lastErr   string
	// installed is a release put in place that waits for a restart.
	installed string
	applying  bool
	// wake has Run check at once (the mode changed).
	wake chan struct{}
}

// NewUpdateService makes an UpdateService; Run starts its checks.
func NewUpdateService(opts UpdateOptions) *UpdateService {
	if opts.Verify == nil {
		opts.Verify = verifyLegatoBinary
	}
	s := &UpdateService{opts: opts, state: UpdateUnchecked, wake: make(chan struct{}, 1)}
	for _, tool := range opts.Tools {
		tool.setOnChange(s.changed)
	}
	return s
}

// release reports whether this Legato is a release, which updates itself.
func (s *UpdateService) release() bool { return version.IsStable(s.opts.Current) }

// newest is the newest version this Legato has: the one it runs, or one
// installed that waits for a restart. Callers hold mu.
func (s *UpdateService) newest() string {
	if s.installed != "" {
		return s.installed
	}
	return s.opts.Current
}

// available reports whether the latest release is newer than what this
// Legato has. Callers hold mu.
func (s *UpdateService) available() bool {
	return s.latest.Version != "" && version.Newer(s.latest.Version, s.newest())
}

// Status is where updates stand.
func (s *UpdateService) Status(ctx context.Context) UpdateStatus {
	mode := s.mode(ctx)
	tools := make([]ToolStatus, 0, len(s.opts.Tools))
	for _, tool := range s.opts.Tools {
		tools = append(tools, tool.Status())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return UpdateStatus{
		Version: s.opts.Current, Release: s.release(), Mode: mode, State: s.state,
		Available: s.available(), Latest: s.latest.Version, LatestURL: s.latest.URL,
		CheckedAt: s.checkedAt, Error: s.lastErr, Tools: tools,
	}
}

// mode is the saved mode, ask when none is saved or it cannot be read.
func (s *UpdateService) mode(ctx context.Context) string {
	mode, err := s.opts.Store.GetInstanceValue(ctx, updateModeKey)
	if err != nil || mode == "" {
		return UpdateModeAsk
	}
	return mode
}

// SetMode sets how this Legato takes a new release: auto, ask or off.
func (s *UpdateService) SetMode(ctx context.Context, mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case UpdateModeAuto, UpdateModeAsk, UpdateModeOff:
	default:
		return fmt.Errorf("%w: %q (use auto, ask or off)", ErrUnknownUpdateMode, mode)
	}
	if err := s.opts.Store.SetInstanceValue(ctx, updateModeKey, mode); err != nil {
		return err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	s.changed()
	return nil
}

// Check asks GitHub for the newest stable release.
func (s *UpdateService) Check(ctx context.Context) (UpdateStatus, error) {
	for _, tool := range s.opts.Tools {
		tool.Check(ctx)
	}
	if !s.release() {
		return s.Status(ctx), ErrNotARelease
	}
	_, err := s.check(ctx)
	return s.Status(ctx), err
}

// check reads the latest release and reports whether it is newer.
func (s *UpdateService) check(ctx context.Context) (bool, error) {
	rel, err := s.opts.Releases.Latest(ctx)
	s.mu.Lock()
	s.checkedAt = time.Now()
	if err != nil {
		s.lastErr = err.Error()
	} else {
		s.latest, s.lastErr = rel, ""
		if !s.applying && s.state != UpdateWaiting {
			switch {
			case s.available():
				s.state = UpdateAvailable
			case s.installed != "":
				s.state = UpdateInstalled
			default:
				s.state = UpdateUpToDate
			}
		}
	}
	available := s.available()
	s.mu.Unlock()
	s.changed()
	return available, err
}

// Apply installs the newest release and restarts on it, or, where this
// Legato cannot restart itself, leaves it for the next start.
func (s *UpdateService) Apply(ctx context.Context) error {
	if !s.release() {
		return ErrNotARelease
	}
	available, err := s.check(ctx)
	if err != nil {
		return err
	}
	if !available {
		return ErrNoUpdate
	}
	s.mu.Lock()
	if s.applying {
		s.mu.Unlock()
		return nil
	}
	s.applying, s.state = true, UpdateInstalling
	rel := s.latest
	s.mu.Unlock()
	s.changed()

	err = s.opts.Releases.Install(ctx, rel, s.opts.Executable, s.opts.Verify(rel.Version))
	s.mu.Lock()
	if err != nil {
		s.applying, s.state, s.lastErr = false, UpdateFailed, err.Error()
		s.mu.Unlock()
		s.changed()
		return err
	}
	s.installed, s.state = rel.Version, UpdateRestarting
	s.mu.Unlock()
	s.changed()

	restarting := s.opts.Restart != nil && s.opts.Restart()
	s.mu.Lock()
	s.applying = restarting
	if !restarting {
		s.state = UpdateInstalled
	}
	s.mu.Unlock()
	s.changed()
	return nil
}

// Run checks for releases until ctx ends: first after FirstCheckAfter,
// then every CheckEvery, and at once when the mode changes. In auto mode it
// installs a newer release once no agent is in the middle of a turn, or
// after MaxWait whatever the agents are doing.
func (s *UpdateService) Run(ctx context.Context) {
	if !s.release() && len(s.opts.Tools) == 0 {
		return
	}
	timer := time.NewTimer(s.opts.FirstCheckAfter)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-s.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer.Reset(s.opts.CheckEvery)
		mode := s.mode(ctx)
		if mode == UpdateModeOff {
			continue
		}
		s.keepToolsCurrent(ctx, mode)
		if !s.release() {
			continue
		}
		available, err := s.check(ctx)
		if err != nil || !available || mode != UpdateModeAuto {
			continue
		}
		if s.waitForQuiet(ctx) {
			if err := s.Apply(ctx); err != nil && ctx.Err() == nil {
				log.Printf("updates: installing: %v", err)
			}
		}
	}
}

// waitForQuiet waits until no agent is in the middle of a turn, or MaxWait
// has passed, and reports whether to install. It gives up when ctx ends or
// the mode is no longer auto.
func (s *UpdateService) waitForQuiet(ctx context.Context) bool {
	s.setState(UpdateWaiting)
	defer s.setState(UpdateAvailable)
	deadline := time.Now().Add(s.opts.MaxWait)
	for {
		busy, err := s.opts.Busy(ctx)
		if err == nil && !busy {
			return true
		}
		if time.Now().After(deadline) {
			log.Printf("updates: agents still busy after %s; installing anyway", s.opts.MaxWait)
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(s.opts.QuietPoll):
		}
		if s.mode(ctx) != UpdateModeAuto {
			return false
		}
	}
}

// keepToolsCurrent checks each tool and, in auto mode, installs a newer
// release at once: a tool's file is replaced by renaming, so a run of it
// that is under way is not disturbed and agents need not be waited for.
func (s *UpdateService) keepToolsCurrent(ctx context.Context, mode string) {
	for _, tool := range s.opts.Tools {
		if st := tool.Check(ctx); mode == UpdateModeAuto && st.Available {
			if err := tool.Apply(ctx); err != nil && ctx.Err() == nil {
				log.Printf("updates: %v", err)
			}
		}
	}
}

// ApplyTool installs a tool's newest release: ErrToolNotManaged for a tool
// the updater does not look after, ErrNoUpdate when nothing newer is out.
func (s *UpdateService) ApplyTool(ctx context.Context, name string) error {
	for _, tool := range s.opts.Tools {
		if tool.Name() == name {
			return tool.Apply(ctx)
		}
	}
	return ErrToolNotManaged
}

func (s *UpdateService) setState(state string) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
	s.changed()
}

func (s *UpdateService) changed() {
	if s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: events.EventUpdateChanged, At: time.Now()})
	}
}

// AgentsMidTurn reports whether a running agent is in the middle of a turn:
// working, or between the steps of one that will wake it again (a background
// shell or subagent). An agent waiting for the person is not.
func AgentsMidTurn(sessions []AgentSession) bool {
	for _, s := range sessions {
		if s.Status == "running" && (s.Activity == store.ActivityWorking || s.Activity == store.ActivityBackground) {
			return true
		}
	}
	return false
}
