package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// ApprovalSourceReeveChangeSet is the approval of a change set the Reeve
// proposed.
const ApprovalSourceReeveChangeSet = "reeve_change_set"

// Kinds of call in a change set.
const (
	CallAPI        = "api"
	CallRestart    = "restart"
	CallConfigEdit = "config_edit"
	CallLegato     = "legato"
)

// OptionNoRestart approves a set but leaves out its optional restart.
const OptionNoRestart = "allow_no_restart"

var (
	// ErrNeverCall: a call the Reeve may never make, whatever is approved.
	ErrNeverCall = errors.New("the Reeve may never make this call")
	// ErrEmptyChangeSet: a set with no calls.
	ErrEmptyChangeSet = errors.New("a change set needs at least one call")
)

// ChangeCall is one call of a change set, as the Reeve proposes it.
type ChangeCall struct {
	Kind   string          `json:"kind"`
	Method string          `json:"method,omitempty"`
	Path   string          `json:"path,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
	// Edits are a config_edit's edits to config.yaml.
	Edits []ConfigEdit `json:"edits,omitempty"`
	// Args are a legato call's arguments: the subcommand and its flags,
	// run with no shell.
	Args []string `json:"args,omitempty"`
	// A host operation: a shell command, a file to write (its whole new
	// content), or a systemd unit with an action. AsRoot runs it as root.
	Command string `json:"command,omitempty"`
	File    string `json:"file,omitempty"`
	Content string `json:"content,omitempty"`
	Unit    string `json:"unit,omitempty"`
	Action  string `json:"action,omitempty"`
	System  bool   `json:"system,omitempty"`
	AsRoot  bool   `json:"as_root,omitempty"`
	// Optional marks a restart the person may untick: the change waits
	// for the next restart.
	Optional bool `json:"optional,omitempty"`
}

// ProposedCall is a call as the approval shows it and as Legato runs it:
// with its class, a plain description, its level, and for a setting its
// value before and after.
type ProposedCall struct {
	ChangeCall
	Class       string     `json:"class"`
	Description string     `json:"description"`
	Level       ReeveLevel `json:"level"`
	Setting     string     `json:"setting,omitempty"`
	Before      any        `json:"before,omitempty"`
	After       any        `json:"after,omitempty"`
	// AppliesAtRestart: a restart-mode setting, which takes effect when
	// Legato restarts.
	AppliesAtRestart bool `json:"applies_at_restart,omitempty"`
	// Unreached are the running agents a new-agents-mode setting does not
	// reach.
	Unreached []string `json:"unreached,omitempty"`
	// Diff is a config_edit's diff of config.yaml; Check what loading the
	// edited copy said ("loads", or the error).
	Diff  string `json:"diff,omitempty"`
	Check string `json:"check,omitempty"`
}

// ChangeSetDetails is what the approval of a set holds: everything Legato
// will run, so a signature covers exactly that.
type ChangeSetDetails struct {
	ID       string         `json:"id"`
	Reason   string         `json:"reason"`
	Instance string         `json:"instance"`
	Level    ReeveLevel     `json:"level"`
	Calls    []ProposedCall `json:"calls"`
}

// CallResult is what one call of a set did.
type CallResult struct {
	Call   int    `json:"call"`
	State  string `json:"state"` // ran, failed, skipped
	Status int    `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ChangeSetOutcome is what propose_changes answers.
type ChangeSetOutcome struct {
	ID         string `json:"id"`
	ApprovalID string `json:"approval_id"`
	// State is approved (Legato runs it now; results follow as a
	// message), denied, or pending (no decision within the wait).
	State string     `json:"state"`
	Level ReeveLevel `json:"level"`
	Note  string     `json:"note,omitempty"`
}

// RouteClassifier says a call's class and a plain description of it, from
// the route class table.
type RouteClassifier func(method, path string) (class, description string)

// CallRunner runs one API call with Legato's own authority, as the person
// would: its status and body.
type CallRunner func(ctx context.Context, method, path string, body []byte) (int, string, error)

// ChangeSetOptions wires a ChangeSetService.
type ChangeSetOptions struct {
	Store     *store.Store
	Approvals *ApprovalService
	Settings  *SettingsService
	Classify  RouteClassifier
	Run       CallRunner
	// ClassifyLegato classes a legato subcommand; RunLegato runs one on
	// the host (its exit code and output).
	ClassifyLegato func(args []string) (class, description string)
	RunLegato      func(ctx context.Context, args []string) (int, string, error)
	// HostExec runs a host operation's command; OutputDir is where the
	// whole output of one past the cap is kept (in the Reeve's folder).
	HostExec  HostExecutor
	OutputDir string
	// ConfigPath is config.yaml (config.ResolveConfigPath when nil).
	ConfigPath func() string
	// Restart restarts Legato, last in a set.
	Restart func()
	// Notify sends a message to a Reeve session ([legato] results, a
	// denial's note).
	Notify func(ctx context.Context, taskID, text string)
	// RunningAgents lists the task IDs of the agents running now, but the
	// Reeve's.
	RunningAgents func(ctx context.Context) []string
	Wait          time.Duration
	Now           func() time.Time
}

// ChangeSetService proposes the Reeve's change sets as approvals and runs
// the approved ones.
type ChangeSetService struct{ opts ChangeSetOptions }

func NewChangeSetService(opts ChangeSetOptions) *ChangeSetService {
	if opts.Wait == 0 {
		opts.Wait = 60 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &ChangeSetService{opts: opts}
}

// Register makes the approval service queue approved sets.
func (c *ChangeSetService) Register() {
	c.opts.Approvals.RegisterResolver(ApprovalSourceReeveChangeSet, changeSetResolver{c})
}

func (c *ChangeSetService) devicesEnrolled(ctx context.Context) bool {
	devices, err := c.opts.Store.ListApprovalDevices(ctx, store.DeviceActive)
	return err == nil && len(devices) > 0
}

// Describe works out what a set of calls is: each call's class,
// description and level, a setting's before and after, and the set's
// level. A call the Reeve may never make refuses the set.
func (c *ChangeSetService) Describe(ctx context.Context, reason string, calls []ChangeCall) (ChangeSetDetails, error) {
	if len(calls) == 0 {
		return ChangeSetDetails{}, ErrEmptyChangeSet
	}
	cfg := config.Defaults()
	if c.opts.Settings != nil {
		if effective, err := c.opts.Settings.Effective(ctx); err == nil {
			cfg = effective
		}
	}
	enrolled := c.devicesEnrolled(ctx)
	details := ChangeSetDetails{Reason: strings.TrimSpace(reason), Instance: instanceName()}
	var levels []ReeveLevel
	for i, call := range calls {
		p := ProposedCall{ChangeCall: call}
		switch call.Kind {
		case CallRestart:
			p.Class, p.Description = "risky", "Restart Legato"
			if call.Optional {
				p.Description = "Restart Legato (you can leave this out: the change then waits for the next restart)"
			}
			p.Level = ReeveLevelFor(cfg.Reeve, p.Class, "restart", enrolled)
		case CallHostShell, CallEditFile, CallManageService:
			if err := c.describeHost(cfg, enrolled, i, &p); err != nil {
				return details, err
			}
		case CallLegato:
			if c.opts.ClassifyLegato == nil {
				return details, fmt.Errorf("call %d: legato commands cannot be run here", i+1)
			}
			p.Class, p.Description = c.opts.ClassifyLegato(call.Args)
			if p.Class == "never" {
				return details, fmt.Errorf("call %d (legato %s): %w: %s", i+1, strings.Join(call.Args, " "), ErrNeverCall, p.Description)
			}
			p.Level = ReeveLevelFor(cfg.Reeve, p.Class, "legato "+strings.Join(call.Args, " "), enrolled)
		case CallConfigEdit:
			if len(call.Edits) == 0 {
				return details, fmt.Errorf("call %d: a config.yaml edit needs edits", i+1)
			}
			p.Class, p.Description = "risky", "Edit config.yaml"
			diff, loadErr, err := CheckConfigEdit(c.configPath(), call.Edits)
			if err != nil {
				return details, fmt.Errorf("call %d: %w", i+1, err)
			}
			if loadErr != "" {
				return details, fmt.Errorf("call %d: the edited config.yaml does not load: %s", i+1, loadErr)
			}
			p.Diff, p.Check = diff, "loads"
			p.Level = ReeveLevelFor(cfg.Reeve, p.Class, "config_edit", enrolled)
			if key := editsOwnRule(call.Edits); key != "" {
				// One of the Reeve's own rules: a signature, as from Settings.
				if !enrolled {
					return details, fmt.Errorf("call %d (%s): %w", i+1, key, ErrNoDeviceEnrolled)
				}
				p.Level = StrictestLevel(p.Level, LevelSigned)
			}
		case CallAPI, "":
			p.Kind = CallAPI
			p.Method = strings.ToUpper(strings.TrimSpace(call.Method))
			if p.Method == "" {
				p.Method = "GET"
			}
			if err := checkAPICall(p.Method, call.Path); err != nil {
				return details, fmt.Errorf("call %d: %w", i+1, err)
			}
			p.Class, p.Description = c.opts.Classify(p.Method, call.Path)
			if why := c.decidesApproval(ctx, p.Method, call.Path); why != "" {
				p.Class, p.Description = "never", why
			}
			if p.Class == "never" {
				return details, fmt.Errorf("call %d (%s %s): %w: %s", i+1, p.Method, call.Path, ErrNeverCall, p.Description)
			}
			p.Level = ReeveLevelFor(cfg.Reeve, p.Class, p.Method+" "+call.Path, enrolled)
			c.describeSetting(ctx, cfg, &p)
		default:
			return details, fmt.Errorf("call %d: unknown kind %q", i+1, call.Kind)
		}
		levels = append(levels, p.Level)
		details.Calls = append(details.Calls, p)
	}
	details.Level = StrictestLevel(levels...)
	return details, nil
}

// approvalProducers are the Attention producers whose actions decide an
// approval or act on approval devices.
var approvalProducers = map[string]bool{"approvals": true, "devices": true}

// decidesApproval says why an API call is never the Reeve's though its
// route is not: an Attention item's action on a pending approval or an
// approval device decides it as the approval routes do. An item that
// cannot be found is refused too, since its producer is unknown. "" for
// any other call.
func (c *ChangeSetService) decidesApproval(ctx context.Context, method, path string) string {
	rest, ok := strings.CutPrefix(path, "/api/attention/")
	if !ok || method != http.MethodPost {
		return ""
	}
	id, action, ok := strings.Cut(rest, "/actions/")
	if !ok || id == "" || action == "" {
		return ""
	}
	item, err := c.opts.Store.GetAttentionItem(ctx, id)
	if err != nil {
		return "Take the action of an Attention item that cannot be found"
	}
	if approvalProducers[item.Producer] {
		return "Decide an approval through its Attention item"
	}
	return ""
}

// describeSetting adds a setting's before and after to a call that changes
// one, and what its mode means.
func (c *ChangeSetService) describeSetting(ctx context.Context, cfg *config.Config, p *ProposedCall) {
	key, ok := strings.CutPrefix(p.Path, "/api/config/settings/")
	if !ok || c.opts.Settings == nil {
		return
	}
	key, _ = url.PathUnescape(key)
	setting, found := config.LookupSetting(key)
	if !found {
		return
	}
	p.Setting = key
	if current, err := c.opts.Settings.Get(ctx, key); err == nil {
		p.Before = current.Value
	}
	switch p.Method {
	case "PUT":
		var body struct {
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(p.Body, &body) == nil {
			if v, err := config.DecodeSetting(setting, body.Value); err == nil {
				p.After = v
			}
		}
	case "DELETE":
		p.After, _ = config.SettingValue(config.Defaults(), key)
	}
	switch setting.Mode {
	case config.ModeRestart:
		p.AppliesAtRestart = true
	case config.ModeNewAgents:
		if c.opts.RunningAgents != nil {
			p.Unreached = c.opts.RunningAgents(ctx)
		}
	}
}

func (c *ChangeSetService) configPath() string {
	if c.opts.ConfigPath != nil {
		return c.opts.ConfigPath()
	}
	return config.ResolveConfigPath()
}

// optionalRestart reports whether a set's restart may be left out.
func optionalRestart(calls []ProposedCall) bool {
	for _, call := range calls {
		if call.Kind == CallRestart && call.Optional {
			return true
		}
	}
	return false
}

// Propose makes a change set of calls for a Reeve session and puts it to
// the person as one approval at its strictest level, then waits for the
// decision (60 seconds). A signed level with no device enrolled refuses
// the set.
func (c *ChangeSetService) Propose(ctx context.Context, taskID, reason string, calls []ChangeCall) (ChangeSetOutcome, error) {
	details, err := c.Describe(ctx, reason, calls)
	if err != nil {
		return ChangeSetOutcome{}, err
	}
	if (details.Level == LevelSigned || details.Level == LevelVerified) && !c.devicesEnrolled(ctx) {
		return ChangeSetOutcome{}, ErrNoDeviceEnrolled
	}
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	details.ID = "cs-" + hex.EncodeToString(b)
	raw, _ := json.Marshal(details.Calls)
	if err := c.opts.Store.InsertReeveChangeSet(ctx, store.ReeveChangeSet{ID: details.ID, TaskID: taskID, Reason: details.Reason, Level: string(details.Level), Calls: string(raw), Waiting: true}); err != nil {
		return ChangeSetOutcome{}, err
	}
	title := details.Reason
	if title == "" {
		title = fmt.Sprintf("%d changes from the Reeve", len(details.Calls))
	}
	options := []ApprovalOption{{ID: "allow", Label: "Approve", Kind: ApprovalAllow}}
	if optionalRestart(details.Calls) {
		options = append(options, ApprovalOption{ID: OptionNoRestart, Label: "Approve without the restart", Kind: ApprovalAllow,
			Description: "The change is made; it takes effect at the next restart."})
	}
	options = append(options, ApprovalOption{ID: "deny", Label: "Deny", Kind: ApprovalDeny})
	approval, _, err := c.opts.Approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceReeveChangeSet, SourceKey: details.ID, TaskID: taskID,
		Title: title, Summary: changeSetSummary(details), Details: details,
		Signed: details.Level == LevelSigned || details.Level == LevelVerified, Verified: details.Level == LevelVerified,
		Options: options,
	})
	if err != nil {
		return ChangeSetOutcome{}, err
	}
	_ = c.opts.Store.SetReeveChangeSetApproval(ctx, details.ID, approval.ID)
	out := ChangeSetOutcome{ID: details.ID, ApprovalID: approval.ID, State: "pending", Level: details.Level}
	decided, err := c.opts.Approvals.Wait(ctx, approval.ID, c.opts.Wait)
	_ = c.opts.Store.SetReeveChangeSetWaiting(context.Background(), details.ID, false)
	if err != nil || decided == nil {
		return out, nil
	}
	switch decided.Status {
	case store.ApprovalApproved:
		out.State = "approved"
	case store.ApprovalDenied, store.ApprovalDismissed:
		out.State = "denied"
		if decided.Decision != nil {
			out.Note = decided.Decision.Feedback
		}
	}
	return out, nil
}

func changeSetSummary(d ChangeSetDetails) string {
	var parts []string
	for _, call := range d.Calls {
		parts = append(parts, call.Description)
	}
	return strings.Join(parts, "; ")
}

type changeSetResolver struct{ c *ChangeSetService }

func (r changeSetResolver) Resolve(ctx context.Context, approval Approval, decision ApprovalDecision) error {
	set, err := r.c.opts.Store.GetReeveChangeSet(ctx, approval.SourceKey)
	if err != nil {
		return err
	}
	if decision.Decision == ApprovalAllow {
		to := store.ChangeSetQueued
		if decision.Option == OptionNoRestart {
			to = store.ChangeSetQueuedNoRestart
		}
		_, err := r.c.opts.Store.MoveReeveChangeSet(ctx, set.ID, store.ChangeSetPending, to)
		return err
	}
	if moved, err := r.c.opts.Store.MoveReeveChangeSet(ctx, set.ID, store.ChangeSetPending, store.ChangeSetDenied); err != nil || !moved {
		return err
	}
	// The tool reports a denial it was waiting for; a later one is told.
	if current, err := r.c.opts.Store.GetReeveChangeSet(ctx, set.ID); err == nil && !current.Waiting && r.c.opts.Notify != nil {
		note := strings.TrimSpace(decision.Feedback)
		if note == "" {
			note = "(no note)"
		}
		r.c.opts.Notify(ctx, set.TaskID, fmt.Sprintf("[legato] The person denied change set %s (%s). Their note: %s\nDo not propose the same change again in this session unless they ask.", set.ID, set.Reason, note))
	}
	return nil
}

// RunQueued runs the approved sets waiting to run, each once (a set is
// claimed by moving it to running), from what its approval holds.
func (c *ChangeSetService) RunQueued(ctx context.Context) int {
	sets, err := c.opts.Store.ListReeveChangeSets(ctx, "")
	if err != nil {
		return 0
	}
	ran := 0
	for _, set := range sets {
		if set.State != store.ChangeSetQueued && set.State != store.ChangeSetQueuedNoRestart {
			continue
		}
		if claimed, err := c.opts.Store.MoveReeveChangeSet(ctx, set.ID, set.State, store.ChangeSetRunning); err != nil || !claimed {
			continue
		}
		c.run(ctx, set, set.State == store.ChangeSetQueuedNoRestart)
		ran++
	}
	return ran
}

// run runs a claimed set's calls in order, a restart last, stopping at the
// first failure, and tells the Reeve.
func (c *ChangeSetService) run(ctx context.Context, set store.ReeveChangeSet, noRestart bool) {
	var details ChangeSetDetails
	var decision *ApprovalDecision
	if approval, err := c.opts.Approvals.Get(ctx, set.ApprovalID); err == nil {
		_ = json.Unmarshal(approval.Details, &details)
		decision = approval.Decision
	}
	cfg := config.Defaults()
	if c.opts.Settings != nil {
		if effective, err := c.opts.Settings.Effective(ctx); err == nil {
			cfg = effective
		}
	}
	calls := details.Calls
	order := make([]int, len(calls))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return calls[order[a]].Kind != CallRestart && calls[order[b]].Kind == CallRestart
	})
	results := make([]CallResult, len(calls))
	failed, restart := false, false
	for _, i := range order {
		call := calls[i]
		results[i] = CallResult{Call: i + 1}
		switch {
		case failed:
			results[i].State = "skipped"
		case call.Kind == CallRestart && noRestart:
			results[i].State, results[i].Detail = "skipped", "left out by the person: the change waits for the next restart"
		case call.Kind == CallRestart:
			results[i].State = "ran"
			restart = true
		case isHostCall(call.Kind):
			res, _, output := c.runHost(ctx, cfg, set, i+1, call)
			results[i] = res
			failed = res.State == "failed"
			c.auditHost(ctx, set, decision, call, res, output)
		case call.Kind == CallLegato:
			exit, output, err := c.opts.RunLegato(ctx, call.Args)
			results[i].Status = exit
			switch {
			case err != nil:
				results[i].State, results[i].Detail, failed = "failed", err.Error(), true
			case exit != 0:
				results[i].State, results[i].Detail, failed = "failed", clip(strings.TrimSpace(output), 500), true
			default:
				results[i].State, results[i].Detail = "ran", clip(strings.TrimSpace(output), 300)
			}
		case call.Kind == CallConfigEdit:
			if backup, err := WriteConfigEdit(c.configPath(), call.Edits); err != nil {
				results[i].State, results[i].Detail, failed = "failed", err.Error(), true
			} else {
				results[i].State, results[i].Detail = "ran", "config.yaml written; the old file is "+backup
			}
		case c.decidesApproval(ctx, strings.ToUpper(call.Method), call.Path) != "":
			// Checked again as it runs: never decide an approval.
			results[i].State, results[i].Detail, failed = "failed", c.decidesApproval(ctx, strings.ToUpper(call.Method), call.Path), true
		default:
			status, body, err := c.opts.Run(ctx, call.Method, call.Path, call.Body)
			results[i].Status = status
			switch {
			case err != nil:
				results[i].State, results[i].Detail, failed = "failed", err.Error(), true
			case status >= 400:
				results[i].State, results[i].Detail, failed = "failed", clip(strings.TrimSpace(body), 500), true
			case status >= 300:
				// A redirect is not followed: the call did not happen.
				results[i].State, results[i].Detail, failed = "failed", "the route answered with a redirect", true
			default:
				results[i].State, results[i].Detail = "ran", clip(strings.TrimSpace(body), 300)
			}
		}
	}
	state := store.ChangeSetDone
	if failed {
		state = store.ChangeSetFailed
	} else if restart {
		state = store.ChangeSetRestarting
	}
	raw, _ := json.Marshal(results)
	if err := c.opts.Store.FinishReeveChangeSet(ctx, set.ID, state, string(raw)); err != nil {
		log.Printf("reeve: change set %s: %v", set.ID, err)
	}
	if c.opts.Notify != nil {
		c.opts.Notify(ctx, set.TaskID, ChangeSetReport(set.ID, set.Reason, calls, results, restart && !failed))
	}
	if restart && !failed && c.opts.Restart != nil {
		c.opts.Restart()
	}
}

// ChangeSetReport is the message that tells the Reeve what each call did.
func ChangeSetReport(id, reason string, calls []ProposedCall, results []CallResult, restarting bool) string {
	var b strings.Builder
	ran, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch r.State {
		case "ran":
			ran++
		case "failed":
			failed++
		default:
			skipped++
		}
	}
	fmt.Fprintf(&b, "[legato] Change set %s (%s): %d of %d ran", id, reason, ran, len(results))
	if failed > 0 {
		fmt.Fprintf(&b, ", %d failed", failed)
	}
	if skipped > 0 {
		fmt.Fprintf(&b, ", %d skipped", skipped)
	}
	b.WriteString(".\n")
	for i, r := range results {
		desc := ""
		if i < len(calls) {
			desc = calls[i].Description
		}
		fmt.Fprintf(&b, "%d. %s: %s", r.Call, desc, r.State)
		if r.Status != 0 {
			fmt.Fprintf(&b, " (%d)", r.Status)
		}
		// A failure says why. A host call's output is reported when it
		// ran too: it is what the Reeve asked for. Other calls' output is
		// not (a legato command's may hold a token).
		if r.Detail != "" && (r.State == "failed" || (i < len(calls) && isHostCall(calls[i].Kind))) {
			fmt.Fprintf(&b, ": %s", r.Detail)
		}
		b.WriteString("\n")
	}
	if restarting {
		b.WriteString("Legato restarts now; you will be told when it is back.\n")
	}
	return b.String()
}

// ChangeSetsOf lists a session's sets, for the conversation's compact
// lines.
func (c *ChangeSetService) ChangeSetsOf(ctx context.Context, taskID string) ([]store.ReeveChangeSet, error) {
	return c.opts.Store.ListReeveChangeSets(ctx, taskID)
}

// ReportRestarts tells the Reeve that Legato is back, for each set that
// ended in a restart before this process started (since): the restart's
// time and version, who asked and why, each step's result, the settings
// now in effect, or the rollback and its error. Each set is reported once.
func (c *ChangeSetService) ReportRestarts(ctx context.Context, since time.Time, version string) int {
	sets, err := c.opts.Store.ListReeveChangeSets(ctx, "")
	if err != nil {
		return 0
	}
	reported := 0
	for _, set := range sets {
		if set.State != store.ChangeSetRestarting {
			continue
		}
		updated, err := time.Parse("2006-01-02T15:04:05.000Z", set.UpdatedAt)
		if err != nil || !updated.Before(since) {
			continue
		}
		var record *store.RestartRecord
		if records, err := c.opts.Store.ListRestartRecords(ctx, 1); err == nil && len(records) == 1 {
			record = &records[0]
		}
		final := store.ChangeSetDone
		if record != nil && record.State == store.RestartRolledBack {
			final = store.ChangeSetFailed
		}
		if moved, err := c.opts.Store.MoveReeveChangeSet(ctx, set.ID, store.ChangeSetRestarting, final); err != nil || !moved {
			continue
		}
		reported++
		if c.opts.Notify != nil {
			c.opts.Notify(ctx, set.TaskID, c.backMessage(ctx, set, record, since, version))
		}
	}
	return reported
}

func (c *ChangeSetService) backMessage(ctx context.Context, set store.ReeveChangeSet, record *store.RestartRecord, since time.Time, version string) string {
	var calls []ProposedCall
	_ = json.Unmarshal([]byte(set.Calls), &calls)
	var results []CallResult
	_ = json.Unmarshal([]byte(set.Results), &results)
	var b strings.Builder
	fmt.Fprintf(&b, "[legato] Legato is back: running %s since %s.\n", version, since.UTC().Format("15:04:05 UTC"))
	fmt.Fprintf(&b, "The restart was asked for by you (the Reeve), in change set %s: %s.\n", set.ID, set.Reason)
	if record != nil && record.State == store.RestartRolledBack {
		fmt.Fprintf(&b, "The restart failed and was rolled back: %s\nThe configuration from before is in effect again.\n", record.Failure)
	}
	for i, r := range results {
		desc := ""
		if i < len(calls) {
			desc = calls[i].Description
		}
		state := r.State
		if state == "ran" {
			state = "applied"
		}
		if record != nil && record.State == store.RestartRolledBack && r.State == "ran" {
			state = "rolled back"
		}
		fmt.Fprintf(&b, "%d. %s: %s\n", r.Call, desc, state)
	}
	if record == nil || record.State != store.RestartRolledBack {
		var now []string
		for _, call := range calls {
			if call.Setting != "" && call.AppliesAtRestart {
				now = append(now, call.Setting)
			}
		}
		if c.opts.Settings != nil {
			if pending, err := c.opts.Settings.PendingRestart(ctx); err == nil {
				for _, key := range pending {
					now = append(now, key)
				}
			}
		}
		if len(now) > 0 {
			fmt.Fprintf(&b, "Now in effect: %s.\n", strings.Join(dedupe(now), ", "))
		}
	}
	return b.String()
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// ImportOutcome is what an import proposed: the change set's approval, and
// the keys of the file that no API changes (file-only ones), left out.
type ImportOutcome struct {
	ID         string   `json:"id"`
	ApprovalID string   `json:"approval_id"`
	Level      string   `json:"level"`
	Calls      int      `json:"calls"`
	Skipped    []string `json:"skipped,omitempty"`
}

// ProposeImport turns a file shaped like config.yaml (legato config
// export) into one change set for the person to approve: a call setting
// each registered setting it gives, and one saving each list entry. It
// does not wait for the decision.
func (c *ChangeSetService) ProposeImport(ctx context.Context, data []byte, by string) (ImportOutcome, error) {
	imported, err := config.LoadBytes(data)
	if err != nil {
		return ImportOutcome{}, fmt.Errorf("the file does not load: %w", err)
	}
	var calls []ChangeCall
	var skipped []string
	for _, key := range imported.PinnedSettingsAndOthers() {
		setting, ok := config.LookupSetting(key)
		if !ok {
			if config.IsFileOnly(key) {
				skipped = append(skipped, key)
			}
			continue
		}
		value, err := config.SettingValue(imported, setting.Key)
		if err != nil {
			continue
		}
		body, _ := json.Marshal(map[string]any{"value": value})
		calls = append(calls, ChangeCall{Kind: CallAPI, Method: "PUT", Path: "/api/config/settings/" + url.PathEscape(setting.Key), Body: body})
	}
	for _, l := range config.EntryListDefs() {
		for _, name := range imported.FileEntries(l.Key) {
			value, ok, err := config.EntryValue(imported, l.Key, name)
			if err != nil || !ok {
				continue
			}
			body, _ := json.Marshal(map[string]any{"value": value})
			calls = append(calls, ChangeCall{Kind: CallAPI, Method: "PUT", Path: "/api/config/lists/" + url.PathEscape(l.Key) + "/" + url.PathEscape(name), Body: body})
		}
	}
	if len(calls) == 0 {
		return ImportOutcome{Skipped: skipped}, errors.New("the file holds no setting that can be imported")
	}
	waitless := *c
	waitless.opts.Wait = time.Millisecond
	out, err := waitless.Propose(ctx, "", "Import settings ("+by+")", calls)
	if err != nil {
		return ImportOutcome{Skipped: skipped}, err
	}
	return ImportOutcome{ID: out.ID, ApprovalID: out.ApprovalID, Level: string(out.Level), Calls: len(calls), Skipped: skipped}, nil
}

// apiMethods are the methods a change set's API call may use.
var apiMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// checkAPICall refuses a method or path that is not a plain route of
// Legato's API: an unknown method, a path that does not parse, has spaces
// or control characters, or is not in its clean form (no //, . or ..).
func checkAPICall(method, target string) error {
	if !apiMethods[method] {
		return fmt.Errorf("%q is not a method a change may use", method)
	}
	for _, r := range target {
		if r <= ' ' || r == 0x7f {
			return fmt.Errorf("%q has a space or a control character", target)
		}
	}
	u, err := url.ParseRequestURI(target)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" {
		return fmt.Errorf("%q is not a path of Legato's API", target)
	}
	if !strings.HasPrefix(u.Path, "/api/") || path.Clean(u.Path) != u.Path {
		return fmt.Errorf("%q is not a route of Legato's API in its plain form", target)
	}
	return nil
}

// editsOwnRule names the first of the Reeve's own rules that edits touch:
// the rule's key, a parent of it (replacing the whole object) or a part of
// it; "" for none.
func editsOwnRule(edits []ConfigEdit) string {
	var rules []string
	for _, s := range config.Settings() {
		if s.OwnRule {
			rules = append(rules, s.Key)
		}
	}
	for _, l := range config.EntryListDefs() {
		if l.OwnRule {
			rules = append(rules, l.Key)
		}
	}
	for _, e := range edits {
		key := strings.TrimSpace(e.Key)
		for _, rule := range rules {
			if key == rule || strings.HasPrefix(rule, key+".") || strings.HasPrefix(key, rule+".") {
				return rule
			}
		}
	}
	return ""
}
