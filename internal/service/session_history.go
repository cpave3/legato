package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/transcript"
)

// SessionHistoryOptions configures the session history.
type SessionHistoryOptions struct {
	// Keep is how long an entry is kept after it was last used
	// (agents.history.keep); 0 keeps entries until they are forgotten.
	Keep time.Duration
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// ToolConfigured says whether an agent tool can be started here; nil
	// is every tool.
	ToolConfigured func(kind string) bool
	// Away says whether a task is away on a worker (on a main instance);
	// nil is never.
	Away func(ctx context.Context, taskID string) bool
	// AwayHistoryURL is where an away task's conversations are listed: its
	// worker's session history; nil or "" is nowhere known.
	AwayHistoryURL func(ctx context.Context, taskID string) string
	// Restorer starts a conversation again; restores are refused without.
	Restorer ConversationRestorer
	// ReeveResume resumes a Reeve session by its task, as
	// ReeveService.Resume does; Reeve restores are refused without.
	ReeveResume func(ctx context.Context, taskID string) error
	// Transcripts finds and reads the agent tools' transcripts; without
	// it their state stays unknown.
	Transcripts *transcript.Reader
	// NewTask makes the task a conversation is restored into, and returns
	// it with a function that removes it again; restores into a new task
	// are refused without. BoardNewTask is the board's.
	NewTask NewTaskMaker
}

// NewTaskMaker makes a task titled title: ephemeral for an ephemeral
// original, else a card in the workspace (when it still exists) and the
// board's first column.
type NewTaskMaker func(ctx context.Context, title string, ephemeral bool, workspaceID *int) (taskID string, remove func(context.Context) error, err error)

// BoardNewTask makes new tasks on the board.
func BoardNewTask(board BoardService, db *store.Store) NewTaskMaker {
	return func(ctx context.Context, title string, ephemeral bool, workspaceID *int) (string, func(context.Context) error, error) {
		if ephemeral {
			id, err := db.CreateEphemeralTask(ctx, title)
			return id, func(ctx context.Context) error { return db.DeleteTask(ctx, id) }, err
		}
		columns, err := board.ListColumns(ctx)
		if err != nil {
			return "", nil, err
		}
		if len(columns) == 0 {
			return "", nil, errors.New("the board has no columns")
		}
		if workspaceID != nil {
			if _, err := db.GetWorkspace(ctx, *workspaceID); err != nil {
				workspaceID = nil
			}
		}
		card, err := board.CreateTask(ctx, title, "", columns[0].Name, "", workspaceID)
		if err != nil {
			return "", nil, err
		}
		return card.ID, func(ctx context.Context) error { return board.DeleteTask(ctx, card.ID) }, nil
	}
}

// SessionHistoryService is the session history: the conversations Legato's
// agents had on this instance, kept with their runs to restore them.
type SessionHistoryService struct {
	store *store.Store
	opts  SessionHistoryOptions

	restoring sync.Map // conversation ID → struct{}: one restore of each at a time
}

// NewSessionHistoryService returns the history kept in db.
func NewSessionHistoryService(db *store.Store, opts SessionHistoryOptions) *SessionHistoryService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &SessionHistoryService{store: db, opts: opts}
}

// SessionHistorySweepEvery is how often the history is swept.
const SessionHistorySweepEvery = 24 * time.Hour

// Sweep removes the entries last used longer ago than Keep, never one with
// a running or interrupted run, and returns how many it removed. With Keep
// 0 it removes none.
func (h *SessionHistoryService) Sweep(ctx context.Context) (int, error) {
	if h.opts.Keep <= 0 {
		return 0, nil
	}
	return h.store.SweepAgentConversations(ctx, h.opts.Now().Add(-h.opts.Keep))
}

// The most entries one page of the history holds, and how many unless asked.
const (
	SessionPageDefault = 50
	SessionPageMost    = 200
)

// ErrBadSessionQuery is a list query the history cannot read, such as a
// cursor it did not give.
var ErrBadSessionQuery = errors.New("bad session history query")

// ErrSessionInUse is a conversation whose agent is running or
// interrupted: it is still its task's, so it cannot be forgotten.
var ErrSessionInUse = errors.New("the conversation is running or interrupted")

// SessionQuery narrows the history. Q is words to search for; State is
// running, interrupted, ended or away.
type SessionQuery struct {
	Q         string
	AgentKind string
	State     string
	TaskID    string
	Repo      string
	Since     time.Time
	Until     time.Time
	Cursor    string
	Limit     int
}

// SessionEntry is one conversation of the session history, with how it
// stands and whether it can be restored.
type SessionEntry struct {
	ID                int64  `json:"id"`
	AgentKind         string `json:"agent_kind"`
	ProviderSessionID string `json:"provider_session_id"`
	TaskID            string `json:"task_id"`
	TaskTitle         string `json:"task_title"`
	TaskExists        bool   `json:"task_exists"`
	TaskArchived      bool   `json:"task_archived"`
	Slot              string `json:"slot"`
	Ephemeral         bool   `json:"ephemeral"`
	Reeve             bool   `json:"reeve"`
	CWD               string `json:"cwd"`
	Repo              string `json:"repo,omitempty"`
	Branch            string `json:"branch,omitempty"`
	// State is running, interrupted, ended (with EndReason) or away.
	State         string     `json:"state"`
	EndReason     string     `json:"end_reason,omitempty"`
	FirstMessage  string     `json:"first_message,omitempty"`
	LastMessage   string     `json:"last_message,omitempty"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	FirstSeenAt   time.Time  `json:"first_seen_at"`
	LastSeenAt    time.Time  `json:"last_seen_at"`
	RunCount      int        `json:"run_count"`
	// Source is recorded, or recovered (found by folder and time).
	Source          string `json:"source"`
	TranscriptState string `json:"transcript_state"`
	// Restorable says whether it can be restored, NewTaskOnly that only
	// into a new task; Reason says why not, or why only so.
	Restorable    bool   `json:"restorable"`
	NewTaskOnly   bool   `json:"new_task_only,omitempty"`
	Reason        string `json:"reason,omitempty"`
	CanStartFresh bool   `json:"can_start_fresh"`
	// AwayHistoryURL is, on a main, the session history of its away task
	// on the worker, where it is restored.
	AwayHistoryURL string `json:"away_history_url,omitempty"`
}

// SessionPage is a page of the history; NextCursor asks for the next one,
// "" when there is none.
type SessionPage struct {
	Sessions   []SessionEntry `json:"sessions"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// SessionRun is one run of a conversation.
type SessionRun struct {
	ExecutionID string     `json:"execution_id"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	EndReason   string     `json:"end_reason,omitempty"`
	Sandboxed   *bool      `json:"sandboxed,omitempty"`
	Engram      string     `json:"engram,omitempty"`
	Resources   []string   `json:"resources"`
}

// SessionDetail is an entry with its runs, the oldest first.
type SessionDetail struct {
	SessionEntry
	TranscriptPath string       `json:"transcript_path,omitempty"`
	Runs           []SessionRun `json:"runs"`
	// Preview is the newest messages of its transcript, when it is there.
	Preview []transcript.Message `json:"preview,omitempty"`
}

// transcriptCheckEvery is how long a look for a transcript stands.
const transcriptCheckEvery = time.Hour

// sessionPreview is how many of a transcript's newest messages a detail shows.
const sessionPreview = 10

// checkTranscript looks for an entry's transcript when it has not been
// looked for within the hour, and fills the messages the entry lacks from
// it. A running conversation's is not looked for: its tool may not have
// written it yet.
func (h *SessionHistoryService) checkTranscript(ctx context.Context, row *store.AgentConversationRow) {
	if h.opts.Transcripts == nil || row.Live || (row.AgentKind != transcript.ClaudeCode && row.AgentKind != transcript.Codex) {
		return
	}
	now := h.opts.Now()
	if row.TranscriptCheckedAt != nil && now.Sub(*row.TranscriptCheckedAt) < transcriptCheckEvery {
		return
	}
	path, err := h.opts.Transcripts.Find(row.AgentKind, row.StorageDir, row.CWD, row.ProviderSessionID)
	state := store.TranscriptPresent
	var first, last string
	var lastAt *time.Time
	switch {
	case errors.Is(err, transcript.ErrNotFound):
		state, path = store.TranscriptMissing, ""
	case err != nil:
		return
	case row.FirstMessage == "" || row.LastMessage == "":
		if t, err := transcript.Read(row.AgentKind, path, 0); err == nil {
			first = firstOperatorText(t.FirstUser)
			last = cleanConversationText(t.LastAssistant)
			if last != "" && !t.EndedAt.IsZero() {
				lastAt = &t.EndedAt
			}
		}
	}
	if err := h.store.RecordTranscriptCheck(ctx, row.ID, path, state, now, first, last, lastAt); err != nil {
		return
	}
	row.TranscriptPath, row.TranscriptState, row.TranscriptCheckedAt = path, state, &now
	if row.FirstMessage == "" {
		row.FirstMessage = first
	}
	if row.LastMessage == "" {
		row.LastMessage, row.LastMessageAt = last, lastAt
	}
}

// firstOperatorText is the first of some user messages that is the
// operator's own, else the first, cleaned.
func firstOperatorText(messages []string) string {
	for _, text := range messages {
		if IsOperatorText(text) {
			return cleanConversationText(text)
		}
	}
	if len(messages) > 0 {
		return cleanConversationText(messages[0])
	}
	return ""
}

// List returns a page of the history: the best matches for Q first, or
// without Q the most recently used.
func (h *SessionHistoryService) List(ctx context.Context, q SessionQuery) (SessionPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = SessionPageDefault
	}
	limit = min(limit, SessionPageMost)
	offset, err := decodeSessionCursor(q.Cursor)
	if err != nil {
		return SessionPage{}, err
	}
	filter := store.ConversationFilter{
		AgentKind: q.AgentKind, TaskID: q.TaskID, Repo: q.Repo, Since: q.Since, Until: q.Until,
		Limit: limit + 1, Offset: offset,
	}
	switch q.State {
	case "", "away":
	case store.ConversationRunning, store.ConversationInterrupted, store.ConversationEnded:
		filter.State = q.State
	default:
		return SessionPage{}, fmt.Errorf("%w: state %q is not running, interrupted, ended or away", ErrBadSessionQuery, q.State)
	}
	if strings.TrimSpace(q.Q) != "" {
		filter.Match = sessionSearch(q.Q)
		if filter.Match == "" {
			return SessionPage{Sessions: []SessionEntry{}}, nil
		}
	}
	rows, err := h.store.ListAgentConversations(ctx, filter)
	if err != nil {
		return SessionPage{}, err
	}
	page := SessionPage{Sessions: make([]SessionEntry, 0, len(rows))}
	if len(rows) > limit {
		rows = rows[:limit]
		page.NextCursor = encodeSessionCursor(offset + limit)
	}
	for _, row := range rows {
		h.checkTranscript(ctx, &row)
		entry := h.entry(ctx, row)
		// An away task is the main's to tell: the worker holds its agent.
		if q.State == "away" && entry.State != "away" || q.State != "" && q.State != "away" && entry.State == "away" {
			continue
		}
		page.Sessions = append(page.Sessions, entry)
	}
	return page, nil
}

// Get returns an entry with its runs; store.ErrNotFound when there is none.
func (h *SessionHistoryService) Get(ctx context.Context, id int64) (*SessionDetail, error) {
	row, err := h.store.GetAgentConversation(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("session %d: %w", id, store.ErrNotFound)
	}
	runs, err := h.store.ListConversationRuns(ctx, id)
	if err != nil {
		return nil, err
	}
	h.checkTranscript(ctx, row)
	detail := &SessionDetail{SessionEntry: h.entry(ctx, *row), TranscriptPath: row.TranscriptPath, Runs: make([]SessionRun, len(runs))}
	if row.TranscriptState == store.TranscriptPresent && row.TranscriptPath != "" {
		if t, err := transcript.Read(row.AgentKind, row.TranscriptPath, sessionPreview); err == nil {
			for i := range t.Recent {
				t.Recent[i].Text = cleanConversationText(t.Recent[i].Text)
			}
			detail.Preview = t.Recent
		}
	}
	for i, run := range runs {
		out := SessionRun{ExecutionID: run.ExecutionID, StartedAt: run.StartedAt, EndedAt: run.EndedAt, Sandboxed: run.Sandboxed}
		if run.EndReason != nil {
			out.EndReason = *run.EndReason
		}
		if name, _, err := h.store.ExecutionEngram(ctx, run.ExecutionID); err == nil {
			out.Engram = name
		}
		out.Resources, _ = h.store.ListExecutionResources(ctx, run.ExecutionID)
		if out.Resources == nil {
			out.Resources = []string{}
		}
		detail.Runs[i] = out
	}
	return detail, nil
}

// Forget removes an entry, its runs and its search row. The tool's own
// transcript and the task's saved conversation are left as they are.
func (h *SessionHistoryService) Forget(ctx context.Context, id int64) error {
	err := h.store.ForgetAgentConversation(ctx, id)
	if errors.Is(err, store.ErrConflict) {
		return ErrSessionInUse
	}
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("session %d: %w", id, store.ErrNotFound)
	}
	return err
}

// entry is a row as the history shows it.
func (h *SessionHistoryService) entry(ctx context.Context, row store.AgentConversationRow) SessionEntry {
	e := SessionEntry{
		ID: row.ID, AgentKind: row.AgentKind, ProviderSessionID: row.ProviderSessionID,
		TaskID: row.TaskID, TaskTitle: row.TaskTitle, TaskExists: row.TaskExists, TaskArchived: row.TaskArchived,
		Slot: row.Slot, Ephemeral: row.Ephemeral, Reeve: row.Reeve, CWD: row.CWD, Repo: row.Repo, Branch: row.Branch,
		FirstMessage: row.FirstMessage, LastMessage: row.LastMessage, LastMessageAt: row.LastMessageAt,
		FirstSeenAt: row.FirstSeenAt, LastSeenAt: row.LastSeenAt, RunCount: row.RunCount,
		Source: row.Source, TranscriptState: row.TranscriptState,
	}
	switch {
	case row.Live:
		e.State = store.ConversationRunning
	case row.LastEndReason == store.RunInterrupted:
		e.State = store.ConversationInterrupted
	default:
		e.State, e.EndReason = store.ConversationEnded, row.LastEndReason
	}
	if row.TaskExists && h.opts.Away != nil && h.opts.Away(ctx, row.TaskID) {
		e.State, e.EndReason = "away", ""
		if h.opts.AwayHistoryURL != nil {
			e.AwayHistoryURL = h.opts.AwayHistoryURL(ctx, row.TaskID)
		}
	}
	h.restorability(&e)
	return e
}

// restorability says whether an entry can be restored, and if not why:
// the checks a restore makes before it starts anything, in its order.
func (h *SessionHistoryService) restorability(e *SessionEntry) {
	refuse := func(reason string, fresh bool) {
		e.Restorable, e.Reason, e.CanStartFresh = false, reason, fresh
	}
	// Chimera keeps its sessions in a store Legato cannot look into, so
	// whether one can be resumed is found out only by trying: a fresh start
	// is offered beside the restore.
	defer func() {
		if e.Restorable && e.AgentKind == "chimera" {
			e.CanStartFresh = true
		}
	}()
	switch {
	case e.State == store.ConversationRunning:
		refuse("it is running", false)
		return
	case e.State == "away":
		refuse("its task is away on a worker", false)
		return
	case h.opts.ToolConfigured != nil && !h.opts.ToolConfigured(e.AgentKind):
		refuse(e.AgentKind+" is not configured", false)
		return
	}
	if e.CWD != "" {
		if info, err := os.Stat(e.CWD); err != nil || !info.IsDir() {
			refuse("its folder "+e.CWD+" is gone", true)
			return
		}
	}
	if e.TranscriptState == store.TranscriptMissing {
		refuse("its transcript is gone", true)
		return
	}
	e.Restorable = true
	if e.Reeve {
		return
	}
	switch {
	case !e.TaskExists:
		e.NewTaskOnly, e.Reason = true, "its task is gone, so it can be restored into a new task only"
	case e.TaskArchived:
		e.NewTaskOnly, e.Reason = true, "its task is archived, so it can be restored into a new task only"
	case IsNamedSlot(e.Slot):
		e.NewTaskOnly, e.Reason = true, "Legato starts that slot's agents itself, so it can be restored into a new task only"
	}
}

// sessionSearch makes words typed into the history's search an FTS5
// expression: each a quoted phrase matched as a prefix, all of them
// required, so nothing typed is read as FTS5 syntax. "" when no word is
// left.
func sessionSearch(q string) string {
	var terms []string
	for _, word := range strings.Fields(q) {
		word = strings.Map(func(r rune) rune {
			if r == '"' || r == '*' || r < ' ' {
				return -1
			}
			return r
		}, word)
		if strings.Trim(word, "()^:+-.,") == "" {
			continue
		}
		terms = append(terms, `"`+word+`"*`)
	}
	return strings.Join(terms, " ")
}

func encodeSessionCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o" + strconv.Itoa(offset)))
}

func decodeSessionCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) < 2 || raw[0] != 'o' {
		return 0, fmt.Errorf("%w: cursor %q", ErrBadSessionQuery, cursor)
	}
	offset, err := strconv.Atoi(string(raw[1:]))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("%w: cursor %q", ErrBadSessionQuery, cursor)
	}
	return offset, nil
}

// Where a restore starts the conversation.
const (
	RestoreIntoTask    = "task"
	RestoreIntoNewTask = "new_task"
)

// RestoreRefusal is a restore refused before anything was started, and why.
type RestoreRefusal struct {
	Reason string `json:"error"`
	// NewTaskPossible says the conversation could be restored into a new
	// task; CanStartFresh that a fresh start could take its place.
	NewTaskPossible bool `json:"new_task_possible,omitempty"`
	CanStartFresh   bool `json:"can_start_fresh,omitempty"`
}

func (r *RestoreRefusal) Error() string { return "cannot restore: " + r.Reason }

// Restore starts a conversation of the history again, into target: its
// own task (RestoreIntoTask) or a new one (RestoreIntoNewTask). A Reeve
// conversation is resumed as the Reeve's. A refusal is a *RestoreRefusal.
func (h *SessionHistoryService) Restore(ctx context.Context, id int64, target string) (string, error) {
	if target != RestoreIntoTask && target != RestoreIntoNewTask {
		return "", fmt.Errorf("%w: target %q is not task or new_task", ErrBadSessionQuery, target)
	}
	if _, busy := h.restoring.LoadOrStore(id, struct{}{}); busy {
		return "", &RestoreRefusal{Reason: "it is already being restored"}
	}
	defer h.restoring.Delete(id)

	detail, err := h.Get(ctx, id)
	if err != nil {
		return "", err
	}
	e := detail.SessionEntry
	if !e.Restorable {
		return "", &RestoreRefusal{Reason: e.Reason, CanStartFresh: e.CanStartFresh}
	}
	if e.Reeve {
		if target == RestoreIntoNewTask {
			return "", &RestoreRefusal{Reason: "a Reeve conversation is restored as the Reeve's, not into a new task"}
		}
		if h.opts.ReeveResume == nil {
			return "", &RestoreRefusal{Reason: "the Reeve is not available here"}
		}
		return e.TaskID, h.opts.ReeveResume(ctx, e.TaskID)
	}
	if target == RestoreIntoTask && e.NewTaskOnly {
		return "", &RestoreRefusal{Reason: e.Reason, NewTaskPossible: true}
	}
	if h.opts.Restorer == nil {
		return "", &RestoreRefusal{Reason: "agents cannot be started here"}
	}
	r := ConversationRestore{
		ConversationID: id, TaskID: e.TaskID, AgentKind: e.AgentKind, ProviderSessionID: e.ProviderSessionID, CWD: e.CWD,
	}
	row, err := h.store.GetAgentConversation(ctx, id)
	if err != nil {
		return "", err
	}
	if row != nil {
		r.StorageDir = row.StorageDir
	}
	if n := len(detail.Runs); n > 0 {
		latest := detail.Runs[n-1]
		r.ExecutionID, r.Sandboxed, r.LastRanAt = latest.ExecutionID, latest.Sandboxed, latest.StartedAt
		if latest.EndedAt != nil {
			r.LastRanAt = *latest.EndedAt
		}
	}
	if target == RestoreIntoNewTask {
		return h.restoreIntoNewTask(ctx, r, e, row)
	}
	return e.TaskID, h.opts.Restorer.RestoreConversation(ctx, r)
}

// restoreIntoNewTask makes a task of the original's kind, gives it the
// conversation and starts it there. When the start fails the task goes
// again and the conversation goes back to its old task.
func (h *SessionHistoryService) restoreIntoNewTask(ctx context.Context, r ConversationRestore, e SessionEntry, row *store.AgentConversationRow) (string, error) {
	if h.opts.NewTask == nil {
		return "", &RestoreRefusal{Reason: "restoring into a new task is not available here"}
	}
	var workspaceID *int
	if row != nil {
		workspaceID = row.WorkspaceID
	}
	taskID, remove, err := h.opts.NewTask(ctx, e.TaskTitle, e.Ephemeral, workspaceID)
	if err != nil {
		return "", fmt.Errorf("making the new task: %w", err)
	}
	undo := func() {
		_ = h.store.MoveAgentConversation(context.WithoutCancel(ctx), r.ConversationID, e.TaskID, e.Slot)
		_ = remove(context.WithoutCancel(ctx))
	}
	if err := h.store.MoveAgentConversation(ctx, r.ConversationID, taskID, store.SlotMain); err != nil {
		undo()
		return "", err
	}
	r.TaskID = taskID
	if err := h.opts.Restorer.RestoreConversation(ctx, r); err != nil {
		undo()
		return "", err
	}
	return taskID, nil
}

// StartFresh starts a new conversation in place of one of the history that
// cannot be restored (its folder or transcript gone, its tool unable to
// resume it): on its task or a new one, in workingDir, else its old folder
// while that exists, set up as its latest run, with a [legato handover]
// message carrying what the history knows of it. It returns the task.
func (h *SessionHistoryService) StartFresh(ctx context.Context, id int64, target, workingDir string) (string, error) {
	if target != RestoreIntoTask && target != RestoreIntoNewTask {
		return "", fmt.Errorf("%w: target %q is not task or new_task", ErrBadSessionQuery, target)
	}
	if _, busy := h.restoring.LoadOrStore(id, struct{}{}); busy {
		return "", &RestoreRefusal{Reason: "it is already being restored"}
	}
	defer h.restoring.Delete(id)
	detail, err := h.Get(ctx, id)
	if err != nil {
		return "", err
	}
	e := detail.SessionEntry
	switch {
	case e.State == store.ConversationRunning:
		return "", &RestoreRefusal{Reason: "it is running"}
	case e.State == "away":
		return "", &RestoreRefusal{Reason: "its task is away on a worker"}
	case e.Reeve:
		return "", &RestoreRefusal{Reason: "start a new Reeve session instead"}
	case h.opts.ToolConfigured != nil && !h.opts.ToolConfigured(e.AgentKind):
		return "", &RestoreRefusal{Reason: e.AgentKind + " is not configured"}
	}
	folder := strings.TrimSpace(workingDir)
	if folder == "" {
		folder = e.CWD
	}
	if info, err := os.Stat(folder); folder == "" || err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: choose a folder to start in: %q is not one", ErrBadSessionQuery, folder)
	}
	if h.opts.Restorer == nil {
		return "", &RestoreRefusal{Reason: "agents cannot be started here"}
	}
	newTaskOnly := !e.TaskExists || e.TaskArchived || IsNamedSlot(e.Slot)
	if target == RestoreIntoTask && newTaskOnly {
		return "", &RestoreRefusal{Reason: "its task cannot take it: start fresh in a new task", NewTaskPossible: true}
	}
	f := FreshStart{AgentKind: e.AgentKind, WorkingDir: folder, Handover: handoverNote(detail)}
	if n := len(detail.Runs); n > 0 {
		f.ExecutionID, f.Sandboxed = detail.Runs[n-1].ExecutionID, detail.Runs[n-1].Sandboxed
	}
	if target == RestoreIntoTask {
		f.TaskID = e.TaskID
		return f.TaskID, h.opts.Restorer.StartFresh(ctx, f)
	}
	if h.opts.NewTask == nil {
		return "", &RestoreRefusal{Reason: "starting in a new task is not available here"}
	}
	var workspaceID *int
	if row, err := h.store.GetAgentConversation(ctx, id); err == nil && row != nil {
		workspaceID = row.WorkspaceID
	}
	taskID, remove, err := h.opts.NewTask(ctx, e.TaskTitle, e.Ephemeral, workspaceID)
	if err != nil {
		return "", fmt.Errorf("making the new task: %w", err)
	}
	f.TaskID = taskID
	if err := h.opts.Restorer.StartFresh(ctx, f); err != nil {
		_ = remove(context.WithoutCancel(ctx))
		return "", err
	}
	return taskID, nil
}

// handoverNote is the first message of a fresh start: what the history
// knows of the conversation it replaces. The messages are kept as they
// were clipped.
func handoverNote(d *SessionDetail) string {
	var b strings.Builder
	b.WriteString("[legato handover] You are taking over from an earlier agent of this task, whose conversation could not be restored. What Legato knows of it:\n")
	line := func(label, value string) {
		if strings.TrimSpace(value) != "" {
			fmt.Fprintf(&b, "- %s: %s\n", label, value)
		}
	}
	line("Task", d.TaskTitle)
	line("Folder", d.CWD)
	if d.Repo != "" {
		where := d.Repo
		if d.Branch != "" {
			where += ", branch " + d.Branch
		}
		line("Repository", where)
	}
	if n := len(d.Runs); n > 0 {
		last := d.Runs[n-1]
		at := last.StartedAt
		if last.EndedAt != nil {
			at = *last.EndedAt
		}
		ended := last.EndReason
		if ended == "" {
			ended = "unknown"
		}
		line("Last ran", at.Local().Format("2 Jan 2006 at 15:04")+"; it ended: "+ended)
	}
	line("Its first prompt", d.FirstMessage)
	line("Its last message", d.LastMessage)
	if d.TranscriptState == store.TranscriptPresent {
		line("Its transcript (read only)", d.TranscriptPath)
	}
	b.WriteString("Files, branches and other agents may have changed since. Check the state before you go on.")
	return b.String()
}
