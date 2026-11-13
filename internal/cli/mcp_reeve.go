package cli

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/site"
)

// MCPReeve is what the Reeve's own tools need. They are offered only to a
// session of the Reeve, and refused to every other.
type MCPReeve struct {
	// Snapshot is the instance's snapshot (instance_status).
	Snapshot func(ctx context.Context) string
	// LogPath is this instance's log file (read_logs).
	LogPath string
	// APIBase is this instance's web API, as https://127.0.0.1:3080
	// (legato_api); "" when no web server runs.
	APIBase string
	// HTTP calls the API; nil uses a client that trusts the local server's
	// own certificate.
	HTTP *http.Client
	// ChangeSets puts the Reeve's changes to the person (propose_changes,
	// and a change made with legato_api).
	ChangeSets MCPChangeSets
	// Secrets asks the person for a secret (request_secret).
	Secrets MCPSecrets
	// Legato classes and runs legato subcommands on the host (the legato
	// tool).
	Legato MCPLegato
	// HostOperations reports whether host operations are on: host_shell,
	// edit_file and manage_service are offered only then.
	HostOperations func(ctx context.Context) bool
	// Peers messages other instances' Reeves (message_reeve).
	Peers MCPPeerReeves
	// Speech says the Reeve's lines out loud on this machine (speak).
	Speech MCPSpeech
}

// MCPSpeech is the Reeve's voice (service.ReeveSpeech): whether it is on,
// and a line put in its queue.
type MCPSpeech interface {
	Enabled(ctx context.Context) bool
	Say(ctx context.Context, line service.SpokenLine) error
}

// MCPPeerReeves messages other instances' Reeves
// (service.ReevePeerService).
type MCPPeerReeves interface {
	List(ctx context.Context) ([]service.Peer, error)
	Send(ctx context.Context, to, text string) (service.SendOutcome, error)
}

// MCPLegato classes and runs legato subcommands on the host.
type MCPLegato interface {
	Classify(args []string) (class, description string)
	Run(ctx context.Context, args []string) (int, string, error)
}

// LegatoCommand is a legato subcommand the Reeve may run, by its path
// (task list) and class (read, change or risky).
type LegatoCommand struct {
	Path        string
	Class       string
	Description string
}

// MCPLegatoCommands lists the commands the legato tool runs, for its
// description. An MCPLegato may implement it.
type MCPLegatoCommands interface {
	Commands() []LegatoCommand
}

// MCPSecrets asks the person for a secret (service.ReeveSecrets).
type MCPSecrets interface {
	Request(ctx context.Context, taskID, name, purpose string) (service.SecretOutcome, error)
}

// MCPChangeSets proposes a change set (service.ChangeSetService).
type MCPChangeSets interface {
	Propose(ctx context.Context, taskID, reason string, calls []service.ChangeCall) (service.ChangeSetOutcome, error)
}

// reeveSession reports whether the calling session is the Reeve's.
func (m *mcpTools) reeveSession() bool {
	return m.opts.Reeve != nil && m.opts.Store != nil && service.IsReeveTask(context.Background(), m.opts.Store, m.opts.TaskID)
}

func (m *mcpTools) reeveTools() []mcpserver.Tool {
	tools := []mcpserver.Tool{m.reeveDocs(), m.reeveStatus(), m.reeveLogs(), m.reeveAPI(), m.watchAgent(), m.unwatchAgent(), m.listWatches()}
	if m.opts.Reeve.ChangeSets != nil {
		tools = append(tools, m.proposeChanges(), m.restartLegato(), m.proposeConfigEdit())
	}
	if m.opts.Reeve.Secrets != nil {
		tools = append(tools, m.requestSecret())
	}
	if m.opts.Reeve.Peers != nil {
		tools = append(tools, m.messageReeve())
	}
	if m.opts.Reeve.Speech != nil && m.opts.Reeve.Speech.Enabled(context.Background()) {
		tools = append(tools, m.speak())
	}
	if m.opts.Reeve.Legato != nil {
		tools = append(tools, m.legatoCommand())
	}
	if m.opts.Reeve.ChangeSets != nil && m.opts.Reeve.HostOperations != nil && m.opts.Reeve.HostOperations(context.Background()) {
		tools = append(tools, m.hostShell(), m.editFile(), m.manageService())
	}
	return tools
}

// isReeve reports whether the calling session is the Reeve's, whether or
// not the Reeve's tools are wired here.
func (m *mcpTools) isReeve() bool {
	return m.opts.Store != nil && service.IsReeveTask(context.Background(), m.opts.Store, m.opts.TaskID)
}

// mustBeReeve refuses a call from any session but the Reeve's: the tools
// are listed only to it, and checked again on each call.
func (m *mcpTools) mustBeReeve(ctx context.Context) error {
	if _, err := m.session(ctx); err != nil {
		return err
	}
	if !service.IsReeveTask(ctx, m.opts.Store, m.opts.TaskID) {
		return fmt.Errorf("this tool is the Reeve's, and this session is not")
	}
	return nil
}

type reeveDocsInput struct {
	Action string `json:"action" jsonschema:"list (every page), search (pages with every word of query) or read (one page by path)"`
	Query  string `json:"query,omitempty" jsonschema:"for search: the words to find"`
	Path   string `json:"path,omitempty" jsonschema:"for read: the page's path, as using/reeve"`
}

func (m *mcpTools) reeveDocs() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "docs", Title: "Read Legato's user manual",
		Description: "Lists, searches and reads Legato's user manual (the pages of its documentation site, built into this Legato). Read the page about something before you explain or change it.",
		ReadOnly:    true,
	}, func(ctx context.Context, in reeveDocsInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		switch in.Action {
		case "list", "":
			var b strings.Builder
			for _, p := range site.Pages() {
				fmt.Fprintf(&b, "- %s: %s", p.Path, p.Title)
				if p.Description != "" {
					fmt.Fprintf(&b, " — %s", p.Description)
				}
				b.WriteString("\n")
			}
			return mcpserver.Result{Text: b.String()}, nil
		case "search":
			hits := site.Search(in.Query, 10)
			if len(hits) == 0 {
				return mcpserver.Result{Text: "No page holds every word of " + in.Query + "."}, nil
			}
			var b strings.Builder
			for _, h := range hits {
				fmt.Fprintf(&b, "## %s (%s)\n", h.Title, h.Path)
				for _, line := range h.Lines {
					fmt.Fprintf(&b, "> %s\n", clipLine(line, 300))
				}
				b.WriteString("\n")
			}
			return mcpserver.Result{Text: b.String()}, nil
		case "read":
			text, ok := site.Read(in.Path)
			if !ok {
				return mcpserver.Result{}, fmt.Errorf("no page %q: list the pages first", in.Path)
			}
			return mcpserver.Result{Text: text}, nil
		}
		return mcpserver.Result{}, fmt.Errorf("action %q: use list, search or read", in.Action)
	})
}

func (m *mcpTools) reeveStatus() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "instance_status", Title: "Read this instance's status",
		Description: "Returns a fresh snapshot of this Legato instance: version and updates, how it runs, its workers and mains, open Attention items, running agents and recent errors in its log.",
		ReadOnly:    true,
	}, func(ctx context.Context, _ struct{}) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		if m.opts.Reeve.Snapshot == nil {
			return mcpserver.Result{}, fmt.Errorf("this Legato cannot take a snapshot of itself here")
		}
		return mcpserver.Result{Text: m.opts.Reeve.Snapshot(ctx)}, nil
	})
}

type reeveLogsInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"a regular expression; only lines that match it are returned"`
	Lines  int    `json:"lines,omitempty" jsonschema:"how many of the most recent lines, 100 when left out, 1000 at most"`
}

func (m *mcpTools) reeveLogs() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "read_logs", Title: "Read this instance's log",
		Description: "Returns the most recent lines of this Legato instance's log file, oldest first, filtered by a regular expression when given.",
		ReadOnly:    true,
	}, func(ctx context.Context, in reeveLogsInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		n := in.Lines
		if n <= 0 {
			n = 100
		}
		if n > 1000 {
			n = 1000
		}
		lines, err := service.ReadLogLines(m.opts.Reeve.LogPath, in.Filter, n)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if len(lines) == 0 {
			return mcpserver.Result{Text: "No lines match."}, nil
		}
		return mcpserver.Result{Text: strings.Join(lines, "\n")}, nil
	})
}

type reeveAPIInput struct {
	Method string         `json:"method" jsonschema:"GET, POST, PUT, PATCH or DELETE"`
	Path   string         `json:"path" jsonschema:"the route, starting /api/, with its query string"`
	Body   map[string]any `json:"body,omitempty" jsonschema:"the JSON body, for a route that takes one"`
	Reason string         `json:"reason,omitempty" jsonschema:"for a change: one line saying why, shown to the operator with it"`
}

func (m *mcpTools) reeveAPI() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "legato_api", Title: "Call Legato's API",
		Description: "Calls a route of this Legato's API with your own token and returns its status and body. Read routes answer at once; a change or risky route answers 403 needs_approval, and a route you may never call answers 403 never.",
	}, func(ctx context.Context, in reeveAPIInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		if !strings.HasPrefix(in.Path, "/api/") {
			return mcpserver.Result{}, fmt.Errorf("path %q: give a route starting /api/", in.Path)
		}
		method := strings.ToUpper(strings.TrimSpace(in.Method))
		if method == "" {
			method = http.MethodGet
		}
		raw := rawBody(in.Body)
		status, out, err := m.callAPI(ctx, method, in.Path, raw)
		if err != nil {
			return mcpserver.Result{}, err
		}
		// A change is not made with the Reeve's token: it becomes a change
		// set of one, for the person to approve.
		if status == http.StatusForbidden && strings.Contains(string(out), "needs_approval") && m.opts.Reeve.ChangeSets != nil {
			return m.propose(ctx, in.Reason, []service.ChangeCall{{Kind: service.CallAPI, Method: method, Path: in.Path, Body: raw}})
		}
		return mcpserver.Result{Text: fmt.Sprintf("%s %s → %d\n\n%s", method, in.Path, status, out)}, nil
	})
}

// callAPI calls a route of this Legato's API with the session's own token
// and answers its status and body (cut at 256 KiB).
func (m *mcpTools) callAPI(ctx context.Context, method, path string, raw json.RawMessage) (int, []byte, error) {
	if m.opts.Reeve.APIBase == "" {
		return 0, nil, fmt.Errorf("this Legato runs no web server, so its API cannot be called")
	}
	session, err := m.opts.Store.GetReeveSession(ctx, m.opts.TaskID)
	if err != nil || session.APIToken == "" {
		return 0, nil, fmt.Errorf("this session has no API token: it has ended")
	}
	var body io.Reader
	if len(raw) > 0 {
		body = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(m.opts.Reeve.APIBase, "/")+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+session.APIToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := m.opts.Reeve.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	_ = m.opts.Store.TouchReeveSession(ctx, m.opts.TaskID)
	return resp.StatusCode, out, nil
}

type watchAgentInput struct {
	TaskID string   `json:"task_id" jsonschema:"the task whose agent to watch"`
	Events []string `json:"events,omitempty" jsonschema:"what to be told of: waiting (a question or permission prompt), idle, background (only background work runs), error, exited, note (a task note added), pr (a pull request opened or changed); all when left out"`
}

type unwatchAgentInput struct {
	TaskID string `json:"task_id" jsonschema:"the task whose agent to stop watching"`
}

func (m *mcpTools) watchAgent() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "watch_agent", Title: "Watch an agent",
		Description: "Be told when an agent waits for the operator, goes idle, works only in the background, errors, exits, adds a task note or has a pull request opened or changed. A [legato agent] message arrives between your turns with what changed and the agent's last message; do not poll it. Watching asks no approval, outlives a restart, and ends when the agent exits. Agents you start or message through a change set are watched already, while reeve.watch.started is on.",
	}, func(ctx context.Context, in watchAgentInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		raw, _ := json.Marshal(map[string]any{"task_id": in.TaskID, "events": in.Events})
		if len(in.Events) == 0 {
			raw, _ = json.Marshal(map[string]any{"task_id": in.TaskID})
		}
		status, out, err := m.callAPI(ctx, http.MethodPost, "/api/reeve/watches", raw)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if status != http.StatusCreated {
			return mcpserver.Result{}, fmt.Errorf("watching %s: %d %s", in.TaskID, status, strings.TrimSpace(string(out)))
		}
		var watch service.ReeveWatchInfo
		_ = json.Unmarshal(out, &watch)
		return mcpserver.Result{Text: fmt.Sprintf("Watching %s for %s. Its agent is %s now.", in.TaskID, strings.Join(watch.Events, ", "), firstLine(watch.State, "not started yet"))}, nil
	})
}

func (m *mcpTools) unwatchAgent() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "unwatch_agent", Title: "Stop watching an agent",
		Description: "Stop being told what an agent does.",
	}, func(ctx context.Context, in unwatchAgentInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		status, out, err := m.callAPI(ctx, http.MethodDelete, "/api/reeve/watches/"+url.PathEscape(in.TaskID), nil)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if status != http.StatusOK {
			return mcpserver.Result{}, fmt.Errorf("unwatching %s: %d %s", in.TaskID, status, strings.TrimSpace(string(out)))
		}
		return mcpserver.Result{Text: in.TaskID + " is no longer watched."}, nil
	})
}

func (m *mcpTools) listWatches() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "list_watches", Title: "List the agents you watch",
		Description: "Lists the agents you watch, what you are told of for each, and how each stands.",
	}, func(ctx context.Context, _ struct{}) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		status, out, err := m.callAPI(ctx, http.MethodGet, "/api/reeve/watches", nil)
		if err != nil {
			return mcpserver.Result{}, err
		}
		var list struct {
			Watches []service.ReeveWatchInfo `json:"watches"`
		}
		if status != http.StatusOK || json.Unmarshal(out, &list) != nil {
			return mcpserver.Result{}, fmt.Errorf("listing watches: %d %s", status, strings.TrimSpace(string(out)))
		}
		if len(list.Watches) == 0 {
			return mcpserver.Result{Text: "You watch no agents."}, nil
		}
		var b strings.Builder
		for _, w := range list.Watches {
			fmt.Fprintf(&b, "- %s (%s): %s; told of %s\n", w.TaskID, w.Title, firstLine(w.State, "not started yet"), strings.Join(w.Events, ", "))
		}
		return mcpserver.Result{Text: b.String()}, nil
	})
}

func clipLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type proposeChangesInput struct {
	Reason string         `json:"reason" jsonschema:"one line saying why, the approval's title"`
	Calls  []proposedCall `json:"calls" jsonschema:"the calls, in order: {kind: api, method, path, body} for an API route, {kind: restart} for a restart of Legato (always run last)"`
}

type proposedCall struct {
	Kind   string         `json:"kind" jsonschema:"api or restart"`
	Method string         `json:"method,omitempty" jsonschema:"for api: GET, POST, PUT, PATCH or DELETE"`
	Path   string         `json:"path,omitempty" jsonschema:"for api: the route, starting /api/"`
	Body   map[string]any `json:"body,omitempty" jsonschema:"for api: the JSON body"`
}

// rawBody is a call's body as JSON, nil for none.
func rawBody(body map[string]any) json.RawMessage {
	if len(body) == 0 {
		return nil
	}
	raw, _ := json.Marshal(body)
	return raw
}

func (m *mcpTools) proposeChanges() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "propose_changes", Title: "Propose a change set",
		Description: "Puts changes to the operator as one approval: a one-line reason and an ordered list of calls. Legato runs exactly these calls once they are approved, in order, a restart last, stopping at the first failure, and tells you each call's result. " +
			"It waits up to a minute for the decision, then answers pending. A set the operator lets run without asking (auto) is approved at once. A call you may never make refuses the whole set.",
	}, func(ctx context.Context, in proposeChangesInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		calls := make([]service.ChangeCall, len(in.Calls))
		for i, c := range in.Calls {
			calls[i] = service.ChangeCall{Kind: c.Kind, Method: c.Method, Path: c.Path, Body: rawBody(c.Body)}
		}
		return m.propose(ctx, in.Reason, calls)
	})
}

func (m *mcpTools) propose(ctx context.Context, reason string, calls []service.ChangeCall) (mcpserver.Result, error) {
	if strings.TrimSpace(reason) == "" && len(calls) == 1 {
		reason = strings.TrimSpace(calls[0].Method + " " + calls[0].Path)
	}
	out, err := m.opts.Reeve.ChangeSets.Propose(ctx, m.opts.TaskID, reason, calls)
	if err != nil {
		return mcpserver.Result{}, err
	}
	_ = m.opts.Store.TouchReeveSession(ctx, m.opts.TaskID)
	var text string
	switch out.State {
	case "approved":
		text = fmt.Sprintf("Change set %s was approved. Legato is running it now; a [legato] message will tell you each call's result.", out.ID)
		if out.Level == service.LevelAuto {
			text = fmt.Sprintf("Change set %s runs now with no approval asked (the operator set these calls to auto). A [legato] message will tell you each call's result: read it before you propose the next step.", out.ID)
		}
	case "denied":
		text = fmt.Sprintf("Change set %s was denied. The operator's note: %s\nDo not propose the same change again in this session unless they ask.", out.ID, noteOrNone(out.Note))
	default:
		text = fmt.Sprintf("Change set %s waits for the operator's approval (%s, approval %s). A [legato] message will tell you the decision and the results; carry on meanwhile, or end your turn.", out.ID, out.Level, out.ApprovalID)
	}
	return mcpserver.Result{Text: text, Data: out}, nil
}

func noteOrNone(note string) string {
	if strings.TrimSpace(note) == "" {
		return "(no note)"
	}
	return note
}

type restartLegatoInput struct {
	Reason string `json:"reason" jsonschema:"one line saying why Legato should restart"`
}

func (m *mcpTools) restartLegato() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "restart_legato", Title: "Restart Legato",
		Description: "Proposes a restart of every Legato process of this instance, as a change set for the operator to approve. Agents keep running. A restart whose start fails is rolled back. When Legato is back, a [legato] message says so.",
	}, func(ctx context.Context, in restartLegatoInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			reason = "restart Legato"
		}
		return m.propose(ctx, reason, []service.ChangeCall{{Kind: service.CallRestart}})
	})
}

type configEditInput struct {
	Reason string         `json:"reason" jsonschema:"one line saying why"`
	Set    map[string]any `json:"set,omitempty" jsonschema:"keys to give a value, dotted as in config.yaml (web.port, agents.sandbox.enabled), with their values. A key reaches into a list by an entry's index or field: credentials[name=github].policy[0].operation; [+] adds an entry at the end: credentials[name=github].policy[+] with the new rule as its value"`
	Remove []string       `json:"remove,omitempty" jsonschema:"dotted keys to take out of config.yaml; a list entry too, as credentials[name=github].policy[2]"`
}

func (m *mcpTools) proposeConfigEdit() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "propose_config_edit", Title: "Propose an edit to config.yaml",
		Description: "Proposes an edit to this instance's config.yaml (for a file-only setting, or one config.yaml pins): Legato checks the edited copy loads, shows the operator a diff, and writes it with its comments kept and a backup, then restarts. The operator may leave the restart out; the edit then waits for the next one.",
	}, func(ctx context.Context, in configEditInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		var edits []service.ConfigEdit
		keys := make([]string, 0, len(in.Set))
		for key := range in.Set {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			edits = append(edits, service.ConfigEdit{Key: key, Value: in.Set[key]})
		}
		for _, key := range in.Remove {
			edits = append(edits, service.ConfigEdit{Key: key, Remove: true})
		}
		if len(edits) == 0 {
			return mcpserver.Result{}, fmt.Errorf("give keys to set or remove")
		}
		return m.propose(ctx, in.Reason, []service.ChangeCall{{Kind: service.CallConfigEdit, Edits: edits}, {Kind: service.CallRestart, Optional: true}})
	})
}

type requestSecretInput struct {
	Name    string `json:"name" jsonschema:"the name to keep the secret under, as elevenlabs-key"`
	Purpose string `json:"purpose" jsonschema:"what it is for, shown to the operator"`
}

func (m *mcpTools) requestSecret() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "request_secret", Title: "Ask the operator for a secret",
		Description: "Asks the operator to paste a secret (an API key, a token) into a form in Legato, which keeps it in its secret store. You get its name to use (legato://<name>, in a setting or a credential), never its value. Never ask for a secret in the conversation.",
	}, func(ctx context.Context, in requestSecretInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		out, err := m.opts.Reeve.Secrets.Request(ctx, m.opts.TaskID, in.Name, in.Purpose)
		if err != nil {
			return mcpserver.Result{}, err
		}
		var text string
		switch out.State {
		case "stored":
			text = "The operator stored the secret. Use it by its name: " + out.Ref
		case "declined":
			text = "The operator declined to give the secret."
		default:
			text = "The request waits in the operator's approval tray (" + out.ApprovalID + "). A [legato] message does not follow: call request_secret again later, or ask the operator."
		}
		return mcpserver.Result{Text: text, Data: out}, nil
	})
}

type messageReeveInput struct {
	To   string `json:"to,omitempty" jsonschema:"the instance, by its name or instance ID; leave out to list them"`
	Text string `json:"text,omitempty" jsonschema:"the message"`
}

func (m *mcpTools) messageReeve() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "message_reeve", Title: "Message another instance's Reeve",
		Description: "Sends a message to the Reeve of another Legato instance this one is paired with: its workers, its main, or an instance paired in Settings › Reeve. Its answer comes back as a [from the Reeve on <name>] message. Call it with no arguments to list the instances you can reach. Ask the other Reeve, never tell it to change something without its person's approval: it proposes its own changes there.",
	}, func(ctx context.Context, in messageReeveInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		if strings.TrimSpace(in.To) == "" {
			peers, err := m.opts.Reeve.Peers.List(ctx)
			if err != nil {
				return mcpserver.Result{}, err
			}
			if len(peers) == 0 {
				return mcpserver.Result{Text: "No other instance is paired with this one."}, nil
			}
			var b strings.Builder
			b.WriteString("Instances whose Reeve you can message:\n")
			for _, p := range peers {
				fmt.Fprintf(&b, "- %s (%s, %s)\n", p.Name, p.Via, p.InstanceID)
			}
			return mcpserver.Result{Text: b.String(), Data: peers}, nil
		}
		if strings.TrimSpace(in.Text) == "" {
			return mcpserver.Result{}, fmt.Errorf("give the text of the message")
		}
		out, err := m.opts.Reeve.Peers.Send(ctx, in.To, in.Text)
		if err != nil {
			return mcpserver.Result{}, err
		}
		var text string
		switch out.State {
		case "queued":
			text = "The Reeve on " + out.To.Name + " cannot be reached from here; the message goes with its next message to you."
		case "waiting":
			text = "Delivered. No Reeve session runs on " + out.To.Name + ", and its person is asked first; its answer comes later."
		default:
			text = "Delivered to the Reeve on " + out.To.Name + "; its answer comes as a message."
		}
		for _, r := range out.Replies {
			text += "\n\nA reply that waited for you: " + r
		}
		return mcpserver.Result{Text: text, Data: out}, nil
	})
}

type messageOwnReeveInput struct {
	Text string `json:"text" jsonschema:"the message: what you ask or report, in a few sentences; put long detail in a task note and point to it"`
}

// messageOwnReeve is message_reeve for every session but the Reeve's: a
// message to this instance's own Reeve, queued and told between its turns
// (service.MessageReeve).
func (m *mcpTools) messageOwnReeve() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "message_reeve", Title: "Message this Legato's Reeve",
		Description: "Sends a message to this Legato's Reeve: the agent that runs this Legato instance and may have started you. " +
			"Use it when the Reeve asked you to report to it (say what you finished, the pull requests, what is left), or to ask it something you need answered. " +
			"It is told between its own turns, within a minute or so, and a suspended Reeve is woken for it. Its answer, if any, comes as a message in this conversation; do not wait idle for one unless you are blocked. " +
			"Record findings that should last in a task note as well (add_task_note): the Reeve and the operator read those.",
	}, func(ctx context.Context, in messageOwnReeveInput) (mcpserver.Result, error) {
		if _, err := m.session(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		out, err := service.MessageReeve(ctx, m.opts.Store, m.opts.TaskID, in.Text, time.Now().UTC())
		if err != nil {
			return mcpserver.Result{}, err
		}
		text := "Queued for the Reeve; it is told between its turns."
		if out.State == service.ReeveMessageWaiting {
			text = "No Reeve session runs now: your message waits for the next one. If the operator needs to know now, say so in your reply or a task note."
		}
		return mcpserver.Result{Text: text, Data: out}, nil
	})
}

type speakInput struct {
	Text string `json:"text" jsonschema:"what to say: one to three short sentences, the outcome first, written to be heard"`
}

func (m *mcpTools) speak() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "speak", Title: "Say something out loud",
		Description: "Says a short line out loud to the operator, in your voice, on the device they are listening on. It returns at once, before the line is heard; lines are spoken in order, one at a time. " +
			"Write for the ear: one to three short sentences, the outcome first, in plain spoken words. Never say code, file paths, IDs, URLs, Markdown, tables or lists, and never read your written reply aloud: " +
			"speech is the short version, and the screen has the detail; the conversation shows what you said, so do not write it again. Over about 400 characters is refused, so say less. " +
			"Answer a message that starts with [legato voice] with it: that message was spoken. Otherwise speak only when the operator asks.",
	}, func(ctx context.Context, in speakInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		if !m.opts.Reeve.Speech.Enabled(ctx) {
			return mcpserver.Result{}, fmt.Errorf("your voice is off (reeve.voice.enabled): reply in writing")
		}
		if err := m.opts.Reeve.Speech.Say(ctx, service.SpokenLine{TaskID: m.opts.TaskID, ExecutionID: m.opts.ExecutionID, Text: in.Text}); err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Result{Text: "queued"}, nil
	})
}

type legatoCommandInput struct {
	Args   []string `json:"args" jsonschema:"the subcommand and its arguments as a list of words, without legato: [\"config\",\"show\"]; a single string is split as a shell would"`
	Reason string   `json:"reason,omitempty" jsonschema:"for a change: one line saying why"`
}

func (m *mcpTools) legatoCommand() mcpserver.Tool {
	description := "Runs a legato subcommand on this instance's host, with no shell: for what the CLI does and the API does not (integration tokens, Argus connect and disconnect, columns sync, hooks install). A command that only reads runs at once; one that changes something becomes a change set for the operator to approve. Some commands are never run: a refusal says what to use instead. " +
		`args is a list of words, as ["config","show"].`
	if lister, ok := m.opts.Reeve.Legato.(MCPLegatoCommands); ok {
		description += "\n\nThe commands you may run, by group, reads apart from changes (\"asks\": a change set for the operator). Mind the difference between a read and a change of a similar name: task notes reads a task's notes, task note writes one.\n" + legatoCommandList(lister.Commands())
	}
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "legato", Title: "Run a legato command on the host",
		Description: description,
		Normalize: func(args map[string]any) {
			if line, ok := args["args"].(string); ok {
				words := splitShellWords(line)
				list := make([]any, len(words))
				for i, w := range words {
					list[i] = w
				}
				args["args"] = list
			}
		},
	}, func(ctx context.Context, in legatoCommandInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		class, description := m.opts.Reeve.Legato.Classify(in.Args)
		switch class {
		case "never":
			return mcpserver.Result{}, fmt.Errorf("refused: %s", description)
		case "read":
			exit, output, err := m.opts.Reeve.Legato.Run(ctx, in.Args)
			if err != nil {
				return mcpserver.Result{}, err
			}
			return mcpserver.Result{Text: fmt.Sprintf("legato %s → exit %d\n\n%s", strings.Join(in.Args, " "), exit, output)}, nil
		}
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			reason = description
		}
		if m.opts.Reeve.ChangeSets == nil {
			return mcpserver.Result{}, fmt.Errorf("%s needs the operator's approval, and change sets are not available here", description)
		}
		return m.propose(ctx, reason, []service.ChangeCall{{Kind: service.CallLegato, Args: in.Args}})
	})
}

type hostShellInput struct {
	Command string `json:"command" jsonschema:"the command, run with sh -c, word for word as the operator will see it"`
	AsRoot  bool   `json:"as_root,omitempty" jsonschema:"run it as root (with sudo); then it always needs the operator's signature with a fingerprint or PIN"`
	Reason  string `json:"reason" jsonschema:"one line saying why"`
}

func (m *mcpTools) hostShell() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "host_shell", Title: "Run a command on the host",
		Description: "Proposes a shell command on this machine, as Legato's user in its home folder, with a small fixed environment and no way to gain privileges (sudo fails) unless as_root. A last resort: use Legato's own tools first. It runs once the operator approves; a [legato] message gives its exit code and output (cut at 64 KB, the rest in a file you can read).",
	}, func(ctx context.Context, in hostShellInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		return m.propose(ctx, firstLine(in.Reason, in.Command), []service.ChangeCall{{Kind: service.CallHostShell, Command: in.Command, AsRoot: in.AsRoot}})
	})
}

type editFileInput struct {
	Path    string `json:"path" jsonschema:"the file, as an absolute path"`
	Content string `json:"content" jsonschema:"the file's whole new content"`
	Reason  string `json:"reason" jsonschema:"one line saying why"`
}

func (m *mcpTools) editFile() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "edit_file", Title: "Write a file on the host",
		Description: "Proposes writing a file outside Legato, as Legato's user: the operator sees a diff, and once approved the file is written atomically, the old one kept beside it as a backup.",
	}, func(ctx context.Context, in editFileInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		return m.propose(ctx, firstLine(in.Reason, "write "+in.Path), []service.ChangeCall{{Kind: service.CallEditFile, File: in.Path, Content: in.Content}})
	})
}

type manageServiceInput struct {
	Unit   string `json:"unit" jsonschema:"the systemd unit, as legato.service"`
	Action string `json:"action" jsonschema:"start, stop, restart, status, or logs (its recent log)"`
	System bool   `json:"system,omitempty" jsonschema:"a system unit rather than one of Legato's user's"`
	AsRoot bool   `json:"as_root,omitempty" jsonschema:"run it as root, as a system unit needs to start or stop"`
	Reason string `json:"reason" jsonschema:"one line saying why"`
}

func (m *mcpTools) manageService() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "manage_service", Title: "Manage a systemd service",
		Description: "Proposes starting, stopping or restarting a systemd unit (Legato's user's, or a system one), or reading its status or recent log, once the operator approves.",
	}, func(ctx context.Context, in manageServiceInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		return m.propose(ctx, firstLine(in.Reason, in.Action+" "+in.Unit), []service.ChangeCall{{Kind: service.CallManageService, Unit: in.Unit, Action: in.Action, System: in.System, AsRoot: in.AsRoot}})
	})
}

func firstLine(reason, fallback string) string {
	if strings.TrimSpace(reason) != "" {
		return strings.TrimSpace(reason)
	}
	return fallback
}

// legatoCommandList writes the commands the legato tool runs one group to a
// line, its reads apart from its changes: "task: reads list, show;
// changes (asks) create".
func legatoCommandList(commands []LegatoCommand) string {
	type group struct{ reads, changes []string }
	var order []string
	byGroup := map[string]*group{}
	for _, c := range commands {
		// A command of one word (version) is its own group.
		name, rest, ok := strings.Cut(c.Path, " ")
		if !ok {
			rest = name
		}
		g := byGroup[name]
		if g == nil {
			g = &group{}
			byGroup[name] = g
			order = append(order, name)
		}
		if c.Class == "read" {
			g.reads = append(g.reads, rest)
		} else {
			g.changes = append(g.changes, rest)
		}
	}
	var b strings.Builder
	for _, name := range order {
		g := byGroup[name]
		var parts []string
		if len(g.reads) > 0 {
			parts = append(parts, "reads "+strings.Join(g.reads, ", "))
		}
		if len(g.changes) > 0 {
			parts = append(parts, "changes (asks) "+strings.Join(g.changes, ", "))
		}
		fmt.Fprintf(&b, "- %s: %s\n", name, strings.Join(parts, "; "))
	}
	return b.String()
}

// splitShellWords splits a command line as a shell would split words:
// spaces between them, quotes and backslashes kept out. Nothing is
// expanded.
func splitShellWords(line string) []string {
	var words []string
	var word strings.Builder
	inWord := false
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}
