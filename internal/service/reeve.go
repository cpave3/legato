package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// The Reeve: the agent of this Legato itself (docs/ai/reeve.md). Each
// session is a hidden task of kind reeve, always in Legato's sandbox,
// working in the Reeve's own folder, with an API token scoped to reading.

var (
	// ErrReeveRunning: the instance already has a running (or suspended)
	// session, and only one runs at a time.
	ErrReeveRunning = errors.New("the Reeve already has a session running; end it first, or add to it")
	// ErrReeveNotEnded: a session must be ended before it is deleted.
	ErrReeveNotEnded = errors.New("end the Reeve's session before deleting it")
	// ErrReeveSandboxed: a Reeve session runs only in Legato's sandbox.
	ErrReeveSandboxed = errors.New("the Reeve runs only in Legato's sandbox")
)

// ReeveTokenPrefix starts every Reeve API token.
const ReeveTokenPrefix = "lgr_"

// ReeveDefaults are the settings a session starts with (reeve.*).
type ReeveDefaults struct {
	Agent      string
	Model      string
	LaunchArgs []string
}

// ReeveStart is how a session starts: an override of the agent tool and
// model for this session (the Reeve's own button), and a brief (Ask the
// Reeve).
type ReeveStart struct {
	Agent     string `json:"agent,omitempty"`
	Model     string `json:"model,omitempty"`
	Brief     string `json:"brief,omitempty"`
	StartedBy string `json:"-"`
}

// ReeveSpawner starts the agent of a session; the agent service is one.
type ReeveSpawner interface {
	SpawnAgent(ctx context.Context, taskID string, width, height int, opts ...AgentSpawnOptions) error
	KillAgent(ctx context.Context, taskID string) error
}

// ReeveServiceOptions wires a ReeveService.
type ReeveServiceOptions struct {
	Store    *store.Store
	Agents   ReeveSpawner
	Folder   string
	Defaults func() ReeveDefaults
	// Snapshot is the instance's snapshot, put in the launch prompt and
	// sent to a resumed session first.
	Snapshot func(ctx context.Context) string
	// Engram is the Reeve's own engram for a session; nil, or an error
	// (ErrEngramsOff), starts the session without one.
	Engram func(ctx context.Context) (ReeveEngram, error)
	// Approvals expires a session's pending approvals when it ends (not
	// when it is suspended).
	Approvals interface {
		ExpireTask(ctx context.Context, taskID string) error
	}
	// Changed is told when a session starts, ends, is suspended or is
	// resumed.
	Changed func(taskID string)
}

// ReeveService starts, ends, resumes and deletes the Reeve's sessions.
type ReeveService struct {
	opts ReeveServiceOptions
	mu   sync.Mutex
	// When each session's agent was last launched: a message to one still
	// starting waits for its native endpoint.
	launchedMu sync.Mutex
	launched   map[string]time.Time
}

// reeveBindWait is how long after a launch a message waits for the Reeve's
// native endpoint, as the agent service waits for its launch messages.
const reeveBindWait = 30 * time.Second

// reeveEndpoints is the part of the agent service that says whether an
// agent's native endpoint is bound.
type reeveEndpoints interface {
	HasChimeraEndpoint(ctx context.Context, taskID string) bool
}

func NewReeveService(opts ReeveServiceOptions) *ReeveService {
	if opts.Defaults == nil {
		opts.Defaults = func() ReeveDefaults { return ReeveDefaults{} }
	}
	return &ReeveService{opts: opts}
}

// ReeveFolder is the Reeve's folder: $XDG_STATE_HOME/legato/reeve, else
// ~/.local/state/legato/reeve. It is outside any repository and the data
// folder, which the sandbox refuses to show.
func ReeveFolder(getenv func(string) string) string {
	if state := getenv("XDG_STATE_HOME"); state != "" {
		return filepath.Join(state, "legato", "reeve")
	}
	home := getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".local", "state", "legato", "reeve")
}

// WorkDir is where a session works, inside the Reeve's folder.
func (s *ReeveService) WorkDir() string { return filepath.Join(s.opts.Folder, "work") }

// ReeveOverview is the Reeve's current session and its history.
type ReeveOverview struct {
	Current *store.ReeveSession  `json:"current,omitempty"`
	History []store.ReeveSession `json:"history"`
	Folder  string               `json:"folder"`
	// Instance is this instance's name (its host name) and Role whether it
	// is a main, a worker, both, or standalone.
	Instance string `json:"instance"`
	Role     string `json:"role"`
	// ToApprove is how many approvals the Reeve's sessions wait on.
	ToApprove int `json:"to_approve"`
	// Engram is the engram the current session's agent runs as, "" when
	// it runs without one.
	Engram string `json:"engram,omitempty"`
}

// Overview is the current session and every session, newest first.
func (s *ReeveService) Overview(ctx context.Context) (ReeveOverview, error) {
	current, err := s.opts.Store.CurrentReeveSession(ctx)
	if err != nil {
		return ReeveOverview{}, err
	}
	history, err := s.opts.Store.ListReeveSessions(ctx)
	if err != nil {
		return ReeveOverview{}, err
	}
	overview := ReeveOverview{Current: current, History: history, Folder: s.opts.Folder, Instance: instanceName(), Role: s.instanceRole(ctx)}
	if current != nil {
		if agent, err := s.opts.Store.GetAgentSessionByTaskID(ctx, current.TaskID); err == nil {
			overview.Engram = agent.Engram
		}
	}
	for _, h := range history {
		if pending, err := s.opts.Store.ListApprovals(ctx, store.ApprovalFilter{Status: store.ApprovalPending, TaskID: h.TaskID}); err == nil {
			overview.ToApprove += len(pending)
		}
	}
	return overview, nil
}

// instanceName is this instance's name: its host name.
func instanceName() string {
	name, err := os.Hostname()
	if err != nil {
		return "this Legato"
	}
	return name
}

func (s *ReeveService) instanceRole(ctx context.Context) string {
	return InstanceRole(ctx, s.opts.Store)
}

// InstanceRole is whether this instance is a main (it has workers), a
// worker (mains are paired with it), both, or standalone.
func InstanceRole(ctx context.Context, db *store.Store) string {
	workers, _ := db.ListWorkers(ctx)
	mains, _ := db.ListMainIntegrationTokens(ctx)
	switch {
	case len(workers) > 0 && len(mains) > 0:
		return "main and worker"
	case len(workers) > 0:
		return "main"
	case len(mains) > 0:
		return "worker"
	}
	return "standalone"
}

// Current is the running or suspended session, or nil.
func (s *ReeveService) Current(ctx context.Context) (*store.ReeveSession, error) {
	return s.opts.Store.CurrentReeveSession(ctx)
}

// Start starts a new session. It is refused while one runs.
func (s *ReeveService) Start(ctx context.Context, in ReeveStart) (*store.ReeveSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked(ctx, in)
}

func (s *ReeveService) startLocked(ctx context.Context, in ReeveStart) (*store.ReeveSession, error) {
	if current, err := s.opts.Store.CurrentReeveSession(ctx); err != nil {
		return nil, err
	} else if current != nil {
		return nil, ErrReeveRunning
	}
	defaults := s.opts.Defaults()
	agent, model := firstNonEmpty(in.Agent, defaults.Agent), firstNonEmpty(in.Model, defaults.Model)
	taskID, err := s.opts.Store.CreateReeveTask(ctx, "The Reeve")
	if err != nil {
		return nil, err
	}
	token, hash := newReeveToken()
	args, _ := json.Marshal(nonNilStrings(defaults.LaunchArgs))
	row := store.ReeveSession{TaskID: taskID, State: store.ReeveRunning, AgentKind: agent, Model: model, LaunchArgs: string(args),
		APIToken: token, APITokenHash: hash, StartedBy: in.StartedBy}
	if err := s.opts.Store.InsertReeveSession(ctx, row); err != nil {
		return nil, err
	}
	if err := s.spawn(ctx, row, in.Brief, false); err != nil {
		_ = s.opts.Store.SetReeveSessionState(ctx, taskID, store.ReeveEnded)
		return nil, err
	}
	s.changed(taskID)
	return s.opts.Store.GetReeveSession(ctx, taskID)
}

func (s *ReeveService) spawn(ctx context.Context, row store.ReeveSession, brief string, resumed bool) error {
	if err := os.MkdirAll(s.WorkDir(), 0o700); err != nil {
		return fmt.Errorf("making the Reeve's folder: %w", err)
	}
	snapshot := ""
	if s.opts.Snapshot != nil {
		snapshot = s.opts.Snapshot(ctx)
	}
	reeveEngram, noEngram := s.engram(ctx)
	launch := ReeveLaunchPrompt(noEngram, snapshot, brief)
	first := ""
	switch {
	case resumed:
		first = ReeveResumeMessage(snapshot)
		if strings.TrimSpace(brief) != "" {
			first += "\n\n" + strings.TrimSpace(brief)
		}
	case strings.TrimSpace(brief) != "":
		first = "[legato] Look into the brief at the end of your instructions, and tell me what you find."
	}
	opt := AgentSpawnOptions{
		AgentKind: row.AgentKind, Model: row.Model, WorkingDir: s.WorkDir(), Ephemeral: true, Prompt: first, LaunchPrompt: launch,
		Resources: []Resource{{Name: "The Reeve's folder", Paths: []ResourcePath{{Path: s.opts.Folder, Access: "write"}}, Configured: true}},
		ExtraEnv:  map[string]string{"LEGATO_REEVE": "1"},
	}
	if reeveEngram.Name != "" {
		opt.Engram, opt.EngramHome, opt.EngramSeed = reeveEngram.Name, reeveEngram.Home, reeveEngram.Seed
		opt.EngramMissing = reeveNoEngram
	}
	_ = json.Unmarshal([]byte(row.LaunchArgs), &opt.LaunchArgs)
	if resumed {
		opt.SessionPolicy = "resume"
	}
	if err := s.opts.Agents.SpawnAgent(ctx, row.TaskID, 0, 0, opt); err != nil {
		return err
	}
	s.launchedMu.Lock()
	if s.launched == nil {
		s.launched = map[string]time.Time{}
	}
	s.launched[row.TaskID] = time.Now()
	s.launchedMu.Unlock()
	return nil
}

// awaitLaunch waits, for a session launched moments ago, until its native
// endpoint is bound: text sent before then would be typed into a tool still
// starting. Up to reeveBindWait after the launch.
func (s *ReeveService) awaitLaunch(ctx context.Context, taskID string) {
	endpoints, ok := s.opts.Agents.(reeveEndpoints)
	if !ok {
		return
	}
	s.launchedMu.Lock()
	at, launched := s.launched[taskID]
	s.launchedMu.Unlock()
	if !launched {
		return
	}
	deadline := at.Add(reeveBindWait)
	for time.Now().Before(deadline) && !endpoints.HasChimeraEndpoint(ctx, taskID) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// engram is the Reeve's engram for a session, or why it has none.
func (s *ReeveService) engram(ctx context.Context) (ReeveEngram, string) {
	if s.opts.Engram == nil {
		return ReeveEngram{}, "this Legato gives the Reeve no engram"
	}
	e, err := s.opts.Engram(ctx)
	if err != nil {
		return ReeveEngram{}, err.Error()
	}
	if e.Name == "" {
		return ReeveEngram{}, "this Legato gives the Reeve no engram"
	}
	return e, ""
}

// reeveEngramEnder ends the engram session a task's agent left kept (the
// agent service).
type reeveEngramEnder interface {
	EndTaskEngramSession(ctx context.Context, taskID string)
}

// End ends the running session: its agent stops, its token is revoked.
func (s *ReeveService) End(ctx context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.opts.Store.GetReeveSession(ctx, taskID)
	if err != nil {
		return err
	}
	if row.State == store.ReeveEnded {
		return nil
	}
	if row.State == store.ReeveRunning {
		if err := s.opts.Agents.KillAgent(ctx, taskID); err != nil && !errors.Is(err, store.ErrNotFound) && !strings.Contains(err.Error(), "no agent") {
			return err
		}
	}
	// A suspended session's agent stopped with its engram session kept:
	// ending the session ends that too.
	if row.State == store.ReeveSuspended {
		if ender, ok := s.opts.Agents.(reeveEngramEnder); ok {
			ender.EndTaskEngramSession(ctx, taskID)
		}
	}
	if err := s.opts.Store.SetReeveSessionState(ctx, taskID, store.ReeveEnded); err != nil {
		return err
	}
	// Its pending approvals expire with it; they do not come back on resume.
	if s.opts.Approvals != nil {
		if err := s.opts.Approvals.ExpireTask(ctx, taskID); err != nil {
			return err
		}
	}
	_ = s.opts.Store.ExpireReeveChangeSets(ctx, taskID)
	s.changed(taskID)
	return nil
}

// StopAgent is the person stopping an agent (the sidebar's or the TUI's
// kill): when it is the current Reeve session's, the session ends, as End
// does, and handled is true. Any other agent is left to the caller.
func (s *ReeveService) StopAgent(ctx context.Context, taskID string) (handled bool, err error) {
	current, err := s.opts.Store.CurrentReeveSession(ctx)
	if err != nil || current == nil || current.TaskID != taskID {
		return false, err
	}
	return true, s.End(ctx, taskID)
}

// Resume runs an ended (or suspended) session again with its conversation:
// a new token, the current defaults unless overridden. Refused while
// another session runs.
func (s *ReeveService) Resume(ctx context.Context, taskID string, in ReeveStart) (*store.ReeveSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumeLocked(ctx, taskID, in)
}

func (s *ReeveService) resumeLocked(ctx context.Context, taskID string, in ReeveStart) (*store.ReeveSession, error) {
	row, err := s.opts.Store.GetReeveSession(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if current, err := s.opts.Store.CurrentReeveSession(ctx); err != nil {
		return nil, err
	} else if current != nil && current.TaskID != taskID {
		return nil, ErrReeveRunning
	}
	if row.State == store.ReeveRunning {
		return row, nil
	}
	defaults := s.opts.Defaults()
	row.AgentKind = firstNonEmpty(in.Agent, row.AgentKind, defaults.Agent)
	row.Model = firstNonEmpty(in.Model, row.Model, defaults.Model)
	if row.State == store.ReeveEnded || row.APIToken == "" {
		row.APIToken, row.APITokenHash = newReeveToken()
	}
	if err := s.opts.Store.ResumeReeveSession(ctx, *row); err != nil {
		return nil, err
	}
	if err := s.spawn(ctx, *row, in.Brief, true); err != nil {
		_ = s.opts.Store.SetReeveSessionState(ctx, taskID, store.ReeveEnded)
		return nil, err
	}
	s.changed(taskID)
	return s.opts.Store.GetReeveSession(ctx, taskID)
}

// Delete removes an ended session and its task.
func (s *ReeveService) Delete(ctx context.Context, taskID string) error {
	row, err := s.opts.Store.GetReeveSession(ctx, taskID)
	if err != nil {
		return err
	}
	if row.State != store.ReeveEnded {
		return ErrReeveNotEnded
	}
	if err := s.opts.Store.DeleteReeveSession(ctx, taskID); err != nil {
		return err
	}
	s.changed(taskID)
	return nil
}

// ReeveIdentity is the session an API token belongs to.
type ReeveIdentity struct {
	TaskID string
}

// Authenticate finds the session a Reeve API token belongs to: a token of
// an ended session is revoked.
func (s *ReeveService) Authenticate(ctx context.Context, token string) (ReeveIdentity, error) {
	if !strings.HasPrefix(token, ReeveTokenPrefix) {
		return ReeveIdentity{}, ErrIntegrationUnauthorized
	}
	row, err := s.opts.Store.ReeveSessionByToken(ctx, hashReeveToken(token))
	if err != nil {
		return ReeveIdentity{}, ErrIntegrationUnauthorized
	}
	return ReeveIdentity{TaskID: row.TaskID}, nil
}

// IsReeveTask reports whether a task is a Reeve session's.
func IsReeveTask(ctx context.Context, db *store.Store, taskID string) bool {
	kind, err := db.TaskKind(ctx, taskID)
	return err == nil && kind == store.TaskKindReeve
}

func (s *ReeveService) changed(taskID string) {
	if s.opts.Changed != nil {
		s.opts.Changed(taskID)
	}
}

func newReeveToken() (token, hash string) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	token = ReeveTokenPrefix + hex.EncodeToString(b)
	return token, hashReeveToken(token)
}

func hashReeveToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// ReeveMessenger delivers a message to a running agent; the agent service
// is one.
type ReeveMessenger interface {
	QueueMessage(ctx context.Context, taskID, message string) error
}

// SuspendIdle suspends the running session when it has been idle (no
// activity since active_at) for longer than timeout at now: its agent
// stops; its token and its pending approvals stay. It reports how many it
// suspended.
func (s *ReeveService) SuspendIdle(ctx context.Context, now time.Time, timeout time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.opts.Store.CurrentReeveSession(ctx)
	if err != nil || current == nil || current.State != store.ReeveRunning {
		return 0, err
	}
	active, err := time.Parse("2006-01-02T15:04:05.000Z", current.ActiveAt)
	if err != nil || now.Sub(active) < timeout {
		return 0, nil
	}
	// Its engram session stays open for the resume.
	if err := s.opts.Agents.KillAgent(withEngramSessionKept(ctx), current.TaskID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}
	if err := s.opts.Store.SetReeveSessionState(ctx, current.TaskID, store.ReeveSuspended); err != nil {
		return 0, err
	}
	s.changed(current.TaskID)
	return 1, nil
}

// Touch records activity in a session, which puts off its idle suspend.
func (s *ReeveService) Touch(ctx context.Context, taskID string) {
	_ = s.opts.Store.TouchReeveSession(ctx, taskID)
}

// Message sends text to the Reeve: into the running session, resuming a
// suspended one with it, or starting a session with it as the brief. The
// session is read under the lock Start and Resume hold, so a message that
// comes while one runs waits for it, then goes to the session it started,
// once that can take it.
func (s *ReeveService) Message(ctx context.Context, text, by string) error {
	s.mu.Lock()
	current, err := s.opts.Store.CurrentReeveSession(ctx)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	switch {
	case current == nil:
		_, err = s.startLocked(ctx, ReeveStart{Brief: text, StartedBy: by})
		s.mu.Unlock()
		return err
	case current.State == store.ReeveSuspended:
		_, err = s.resumeLocked(ctx, current.TaskID, ReeveStart{Brief: text, StartedBy: by})
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	messenger, ok := s.opts.Agents.(ReeveMessenger)
	if !ok {
		return errors.New("the Reeve's agent cannot be sent a message here")
	}
	s.awaitLaunch(ctx, current.TaskID)
	s.Touch(ctx, current.TaskID)
	return messenger.QueueMessage(ctx, current.TaskID, text)
}

// Peer policies: what a peer's message does when no session runs.
const (
	PeerStart  = "start"
	PeerAsk    = "ask"
	PeerRefuse = "refuse"
)

// PeerMessage gives this instance's Reeve a message from another
// instance's, marked as such: into the running session (a suspended one
// resumes). With none running, policy decides: start a session (the
// default), keep it for a person to let the Reeve talk (ErrPeerAsk), or
// refuse it (ErrPeerRefused). A peer starts at most one session at a time.
func (s *ReeveService) PeerMessage(ctx context.Context, from PeerInfo, text, policy string) error {
	framed := PeerFrame(from, text)
	current, err := s.opts.Store.CurrentReeveSession(ctx)
	if err != nil {
		return err
	}
	if current != nil {
		return s.Message(ctx, framed, "peer:"+from.InstanceID)
	}
	switch policy {
	case PeerRefuse:
		return ErrPeerRefused
	case PeerAsk:
		if err := s.opts.Store.InsertReeveInbox(ctx, store.ReeveInboxMessage{InstanceID: from.InstanceID, Name: from.Name, Role: from.Role, Text: text}); err != nil {
			return err
		}
		s.changed("")
		return ErrPeerAsk
	}
	_, err = s.Start(ctx, ReeveStart{Brief: framed, StartedBy: "peer:" + from.InstanceID})
	return err
}

// LetPeerTalk starts a session with the messages one peer left while the
// person was asked.
func (s *ReeveService) LetPeerTalk(ctx context.Context, instanceID string) error {
	waiting, err := s.opts.Store.TakeReeveInbox(ctx, instanceID)
	if err != nil || len(waiting) == 0 {
		return err
	}
	var texts []string
	for _, m := range waiting {
		texts = append(texts, PeerFrame(PeerInfo{InstanceID: m.InstanceID, Name: m.Name, Role: m.Role}, m.Text))
	}
	return s.Message(ctx, strings.Join(texts, "\n\n"), "peer:"+instanceID)
}

// Open is the Reeve's session to go to: the running one, a suspended one
// resumed, or a new one started.
func (s *ReeveService) Open(ctx context.Context, by string) (string, error) {
	current, err := s.opts.Store.CurrentReeveSession(ctx)
	if err != nil {
		return "", err
	}
	switch {
	case current == nil:
		session, err := s.Start(ctx, ReeveStart{StartedBy: by})
		if err != nil {
			return "", err
		}
		return session.TaskID, nil
	case current.State == store.ReeveSuspended:
		if _, err := s.Resume(ctx, current.TaskID, ReeveStart{StartedBy: by}); err != nil {
			return "", err
		}
	}
	return current.TaskID, nil
}

// Ask gives the Reeve a brief (Message) and returns the task of the session
// that has it.
func (s *ReeveService) Ask(ctx context.Context, brief, by string) (string, error) {
	if err := s.Message(ctx, brief, by); err != nil {
		return "", err
	}
	current, err := s.opts.Store.CurrentReeveSession(ctx)
	if err != nil || current == nil {
		return "", errors.New("the Reeve's session did not start")
	}
	return current.TaskID, nil
}

// ResumeInterrupted resumes the Reeve's session when a crash took its agent
// (an InterruptedResumeRoute). The session is still marked running, so it
// is suspended first and resumed as one, with a fresh snapshot. Another
// task is not handled.
func (s *ReeveService) ResumeInterrupted(ctx context.Context, taskID string) (bool, error) {
	row, err := s.opts.Store.GetReeveSession(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if row.State == store.ReeveRunning {
		if err := s.opts.Store.SetReeveSessionState(ctx, taskID, store.ReeveSuspended); err != nil {
			return true, err
		}
	}
	_, err = s.Resume(ctx, taskID, ReeveStart{})
	return true, err
}
