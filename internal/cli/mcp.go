package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// MCPOptions says which Legato session an MCP server acts for and what it
// acts through. `legato mcp serve` runs inside an agent's own process tree, so
// the session is the one whose pane it was started in.
type MCPOptions struct {
	Store *store.Store
	// Agents reaches the other agents of the session's worktree. Without it
	// the peer tools are not offered.
	Agents MCPPeers
	// TaskID and ExecutionID identify the calling agent's session. With no
	// task ID the server is not inside a Legato session and offers no tools.
	TaskID      string
	ExecutionID string
	// Version is Legato's own, reported to the client as the server's.
	Version string
	// ResourceDomains are the hosts an agent's HTML may load from.
	ResourceDomains []string
}

// MCPPeers is the part of the agent service the peer tools use.
type MCPPeers interface {
	ListPeers(ctx context.Context, taskID, executionID, directory string) ([]service.AgentPeer, error)
	SendPeerMessage(ctx context.Context, senderTaskID, recipientID, message string) error
	BroadcastToPeers(ctx context.Context, taskID, executionID, directory, message string) (service.AgentBroadcastResult, error)
}

// MCPGuidance tells an agent that Legato's tools exist and when to use them
// without being asked. It is the server's own instructions, which reach the
// model of an agent tool that reads them (Claude Code does), and the text
// Legato adds to the launch prompt of one that does not (Codex, Chimera). It
// names each tool by its own name: every agent tool prefixes MCP tool names
// in its own way.
const MCPGuidance = `You are running inside Legato, the app the person uses to work with you, and you have tools from the MCP server named "legato" that act on your own Legato session. Legato found the session by itself, so none of them takes a session or task ID. Your agent tool may show their names with a prefix of its own, or keep them out of your tool list until you look them up by name.

Use these tools without being asked. They are how you give the person something better than a wall of text:

- When your answer is mainly data (figures, a status, a comparison of options, a ranking, progress, a small chart), prefer a card to a Markdown table or a list of numbers: call show_components with the parts of the card and Legato draws it in the conversation. Call show_card with HTML of your own design when those parts are not enough.
- When your answer is long or has structure (an explanation that needs a diagram, a design, a plan, a report, anything longer than about a screen), prefer an artifact to a long reply: call show_artifact and it opens beside the conversation, where the person can keep it open. Call it again with the same key to revise it.
- After you show something, say in one sentence what it is, and do not repeat its content.
- A short, plain answer stays plain text.

Also yours to use without being asked:
- task_context: read the task you are working on, with its notes, when you need the brief or what earlier work left behind.
- add_task_note: record a finding or a decision that should outlast this session.
- list_peers, message_peer, broadcast_peers: see and coordinate with other agents working in the same Git worktree.
- session_info: which session, task and working directory you are in.`

const mcpOutsideSession = "This server acts on a Legato agent session, and it was not started inside one (LEGATO_TASK_ID is not set), so it offers no tools."

// MCPServerOptions returns the MCP server Legato offers to the agent of one
// session: its name, its instructions and its tools.
func MCPServerOptions(opts MCPOptions) mcpserver.Options {
	server := mcpserver.Options{Name: "legato", Title: "Legato", Version: opts.Version, Instructions: mcpOutsideSession}
	opts.TaskID = strings.TrimSpace(opts.TaskID)
	if opts.TaskID == "" {
		return server
	}
	tools := &mcpTools{
		opts: opts,
		ui:   service.NewAgentUIService(opts.Store, nil, service.AgentUIOptions{ResourceDomains: opts.ResourceDomains}),
	}
	server.Instructions = MCPGuidance
	server.Tools = tools.all()
	return server
}

// MCPToolNames returns the name of every tool the server can offer, for an
// agent tool that must be told which calls to run without asking.
func MCPToolNames() []string {
	// The tools are only named here, never called, so they need nothing real
	// behind them; the peers are present so that the peer tools are counted.
	tools := (&mcpTools{opts: MCPOptions{Agents: noPeers{}}}).all()
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	return names
}

// noPeers stands in for the agent service where only tool names are wanted.
type noPeers struct{}

func (noPeers) ListPeers(context.Context, string, string, string) ([]service.AgentPeer, error) {
	return nil, nil
}

func (noPeers) SendPeerMessage(context.Context, string, string, string) error { return nil }

func (noPeers) BroadcastToPeers(context.Context, string, string, string, string) (service.AgentBroadcastResult, error) {
	return service.AgentBroadcastResult{}, nil
}

// mcpTools holds what the tools of one session's server share.
type mcpTools struct {
	opts MCPOptions
	ui   *service.AgentUIService
}

func (m *mcpTools) all() []mcpserver.Tool {
	tools := []mcpserver.Tool{
		m.showArtifact(), m.showCard(), m.showComponents(),
		m.sessionInfo(), m.taskContext(), m.addTaskNote(),
	}
	if m.opts.Agents != nil {
		tools = append(tools, m.listPeers(), m.messagePeer(), m.broadcastPeers())
	}
	return tools
}

// session returns the calling agent's running session. It is looked up on
// each call rather than once at start: an agent tool starts its MCP servers
// while Legato may still be recording the session it launched.
func (m *mcpTools) session(ctx context.Context) (*store.AgentSession, error) {
	session, err := m.opts.Store.GetAgentSessionByTaskID(ctx, m.opts.TaskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("Legato has no running agent session for task %s, so there is nowhere to act", m.opts.TaskID)
	}
	if err != nil {
		return nil, err
	}
	if m.opts.ExecutionID != "" && session.ExecutionID != "" && session.ExecutionID != m.opts.ExecutionID {
		return nil, fmt.Errorf("execution %s does not own the running agent session of task %s", m.opts.ExecutionID, m.opts.TaskID)
	}
	return session, nil
}

// show stores what an agent asked to show and tells running Legato instances.
func (m *mcpTools) show(ctx context.Context, in service.AgentUIShowInput, shownAs string) (mcpserver.Result, error) {
	if _, err := m.session(ctx); err != nil {
		return mcpserver.Result{}, err
	}
	in.TaskID, in.ExecutionID = m.opts.TaskID, m.opts.ExecutionID
	item, err := m.ui.Show(ctx, in)
	if err != nil {
		return mcpserver.Result{}, err
	}
	ipc.Broadcast(ipc.Message{
		Type: "agent_ui_changed", TaskID: item.TaskID, ItemID: item.ID,
		Placement: item.Placement, Revision: item.Revision, Kind: "shown",
	})
	return mcpserver.Result{
		// The marker is how Legato's own conversation view finds the item
		// this call made; the rest is for the model. There is no structured
		// content: a client that has it gives the model that instead of this.
		Text: fmt.Sprintf("%s [legato-ui:%s:%d]. The person can see it in Legato now. Say in one sentence what you showed; do not repeat its content.", shownAs, item.ID, item.Revision),
	}, nil
}

type showArtifactInput struct {
	Title   string `json:"title" jsonschema:"a short name for the artifact, shown on its tab"`
	Content string `json:"content" jsonschema:"the whole artifact: an HTML document or fragment, or Markdown"`
	Format  string `json:"format,omitempty" jsonschema:"html (default) or markdown"`
	Key     string `json:"key,omitempty" jsonschema:"a name of your own for this artifact; showing again under the same key replaces it as a new revision instead of adding another"`
}

func (m *mcpTools) showArtifact() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "show_artifact",
		Title: "Show an artifact",
		Description: "Shows a page of your own making in a panel beside the conversation in Legato, where the person can read it while you carry on. " +
			"Use it without being asked for something worth more than a reply: an explanation with diagrams, a design, a report, a comparison. " +
			m.htmlGuide(),
		ReadOnly: true,
	}, func(ctx context.Context, in showArtifactInput) (mcpserver.Result, error) {
		format := strings.TrimSpace(in.Format)
		if format == "" {
			format = service.AgentUIFormatHTML
		}
		return m.show(ctx, service.AgentUIShowInput{
			Placement: service.AgentUIPanel, Format: format, Title: in.Title, Key: in.Key, Content: in.Content,
		}, fmt.Sprintf("Shown in Legato's side panel as %q", strings.TrimSpace(in.Title)))
	})
}

type showCardInput struct {
	HTML         string `json:"html" jsonschema:"the card: an HTML fragment with its own inline style, sized by its content"`
	FallbackText string `json:"fallback_text" jsonschema:"the same information as plain text, for where the card cannot be drawn"`
	Title        string `json:"title,omitempty" jsonschema:"a short name for the card"`
	Key          string `json:"key,omitempty" jsonschema:"a name of your own for this card; showing again under the same key keeps one card and adds a revision"`
}

func (m *mcpTools) showCard() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "show_card",
		Title: "Show a card",
		Description: "Shows a card of your own design in the conversation in Legato, in place of text. Use it without being asked when an answer is better seen than read: " +
			"a weather report, a status, a comparison, a small chart. Write the card as HTML; for plain figures, tables and lists show_components costs less. " +
			m.htmlGuide(),
		ReadOnly: true,
	}, func(ctx context.Context, in showCardInput) (mcpserver.Result, error) {
		return m.show(ctx, service.AgentUIShowInput{
			Placement: service.AgentUIInline, Format: service.AgentUIFormatHTML,
			Title: in.Title, Key: in.Key, Content: in.HTML, FallbackText: in.FallbackText,
		}, "Shown as a card in the conversation")
	})
}

type showComponentsInput struct {
	Components   []service.UIComponent `json:"components" jsonschema:"the card's components, top to bottom"`
	FallbackText string                `json:"fallback_text" jsonschema:"the same information as plain text, for where the card cannot be drawn"`
	Title        string                `json:"title,omitempty" jsonschema:"a short name for the card"`
	Key          string                `json:"key,omitempty" jsonschema:"a name of your own for this card; showing again under the same key keeps one card and adds a revision"`
}

func (m *mcpTools) showComponents() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "show_components",
		Title: "Show a component card",
		Description: "Shows a card in the conversation in Legato, built from Legato's own components instead of HTML: you name what to show and Legato draws it to match the app. " +
			"Use it without being asked when an answer is better seen than read, and prefer it to show_card for figures, tables, lists, progress and simple bar charts.\n\n" + service.UIComponentGuide,
		ReadOnly: true,
	}, func(ctx context.Context, in showComponentsInput) (mcpserver.Result, error) {
		return m.show(ctx, service.AgentUIShowInput{
			Placement: service.AgentUIInline, Format: service.AgentUIFormatComponents,
			Title: in.Title, Key: in.Key, Components: in.Components, FallbackText: in.FallbackText,
		}, "Shown as a card in the conversation")
	})
}

// mcpSessionInfo is what an agent can learn about its own Legato session.
type mcpSessionInfo struct {
	TaskID       string `json:"task_id"`
	Title        string `json:"title"`
	AgentKind    string `json:"agent_kind"`
	Role         string `json:"role,omitempty"`
	ParentTaskID string `json:"parent_task_id,omitempty"`
	ExecutionID  string `json:"execution_id,omitempty"`
	WorkingDir   string `json:"working_dir,omitempty"`
}

func (m *mcpTools) sessionInfo() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:        "session_info",
		Title:       "Describe this session",
		Description: "Tells you which Legato session you are running in: its task ID and title, the agent tool, your role in a swarm if you have one, and the working directory.",
		ReadOnly:    true,
	}, func(ctx context.Context, _ struct{}) (mcpserver.Result, error) {
		session, err := m.session(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		info := mcpSessionInfo{TaskID: session.TaskID, AgentKind: session.AgentKind, Role: session.Role, ExecutionID: session.ExecutionID}
		if session.ParentTaskID != nil {
			info.ParentTaskID = *session.ParentTaskID
		}
		if info.ExecutionID != "" {
			if execution, err := m.opts.Store.GetExecution(ctx, info.ExecutionID); err == nil {
				info.Title, info.WorkingDir = execution.TaskTitle, execution.WorkingDir
			}
		}
		if info.Title == "" {
			if task, err := m.opts.Store.GetTask(ctx, session.TaskID); err == nil {
				info.Title = task.Title
			}
		}
		var text strings.Builder
		fmt.Fprintf(&text, "Task %s: %s\nAgent: %s", info.TaskID, info.Title, info.AgentKind)
		if info.Role != "" {
			fmt.Fprintf(&text, " (%s)", info.Role)
		}
		if info.ParentTaskID != "" {
			fmt.Fprintf(&text, "\nSwarm parent task: %s", info.ParentTaskID)
		}
		if info.WorkingDir != "" {
			fmt.Fprintf(&text, "\nWorking directory: %s", info.WorkingDir)
		}
		return mcpserver.Result{Text: text.String(), Data: info}, nil
	})
}

// workTask returns the task the calling agent is working on. A swarm
// worker's own identity is its sub-task, which has no description or notes:
// its task is the one the swarm belongs to.
func (m *mcpTools) workTask(ctx context.Context) (string, error) {
	session, err := m.session(ctx)
	if err != nil {
		return "", err
	}
	if session.ParentTaskID != nil && *session.ParentTaskID != "" {
		return *session.ParentTaskID, nil
	}
	return session.TaskID, nil
}

func (m *mcpTools) taskContext() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:        "task_context",
		Title:       "Read the task",
		Description: "Returns the Legato task you are working on as Markdown: its title, status, description, linked ticket and repository details, and the durable notes recorded on it. Read it when you need the brief or what earlier work left behind.",
		ReadOnly:    true,
	}, func(ctx context.Context, _ struct{}) (mcpserver.Result, error) {
		taskID, err := m.workTask(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		text, err := TaskShow(m.opts.Store, taskID, "full")
		if err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Result{Text: text}, nil
	})
}

type addTaskNoteInput struct {
	Body string `json:"body" jsonschema:"the note, as Markdown"`
}

func (m *mcpTools) addTaskNote() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "add_task_note",
		Title: "Add a task note",
		Description: "Records a durable Markdown note on the Legato task you are working on: a finding, a decision, what is left to do. " +
			"The note stays with the task after this session ends and is shown to the person and to later agents. It is kept in Legato only and is not sent to a ticket provider.",
	}, func(ctx context.Context, in addTaskNoteInput) (mcpserver.Result, error) {
		taskID, err := m.workTask(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		note, err := service.AddTaskNote(ctx, m.opts.Store, taskID, in.Body, "agent")
		if err != nil {
			return mcpserver.Result{}, err
		}
		ipc.Broadcast(ipc.Message{Type: "task_note", TaskID: taskID})
		return mcpserver.Result{
			Text: fmt.Sprintf("Note %s recorded on task %s.", note.ID, taskID),
			Data: map[string]any{"note_id": note.ID, "task_id": taskID},
		}, nil
	})
}

func (m *mcpTools) listPeers() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:        "list_peers",
		Title:       "List peer agents",
		Description: "Lists the other Legato agents running in the same Git worktree as you: their task ID, what they are working on, their agent tool and whether they are busy. Agents in other worktrees are not peers.",
		ReadOnly:    true,
	}, func(ctx context.Context, _ struct{}) (mcpserver.Result, error) {
		if _, err := m.session(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		peers, err := m.opts.Agents.ListPeers(ctx, m.opts.TaskID, m.opts.ExecutionID, "")
		if err != nil {
			return mcpserver.Result{}, err
		}
		if peers == nil {
			peers = []service.AgentPeer{}
		}
		var text strings.Builder
		if len(peers) == 0 {
			text.WriteString("No other agent is running in this worktree.")
		}
		for _, peer := range peers {
			activity := peer.Activity
			if activity == "" {
				activity = "idle"
			}
			fmt.Fprintf(&text, "%s (%s, %s): %s\n", peer.TaskID, peer.Kind, activity, peer.Title)
		}
		return mcpserver.Result{Text: strings.TrimRight(text.String(), "\n"), Data: map[string]any{"peers": peers}}, nil
	})
}

type messagePeerInput struct {
	To   string `json:"to" jsonschema:"the peer's task ID, from list_peers"`
	Text string `json:"text" jsonschema:"the message"`
}

func (m *mcpTools) messagePeer() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "message_peer",
		Title: "Message a peer agent",
		Description: "Sends a message to one other Legato agent, named by its task ID. An idle agent starts a turn on it; a busy one reads it at its next step. " +
			"Use it to coordinate: say which files you are about to change, or ask before touching theirs. Delivery is best effort.",
	}, func(ctx context.Context, in messagePeerInput) (mcpserver.Result, error) {
		if _, err := m.session(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		to, text := strings.TrimSpace(in.To), strings.TrimSpace(in.Text)
		if to == "" || text == "" {
			return mcpserver.Result{}, fmt.Errorf("message_peer needs both the peer's task ID and a message")
		}
		if err := m.opts.Agents.SendPeerMessage(ctx, m.opts.TaskID, to, text); err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Result{Text: "Message queued for " + to + ".", Data: map[string]any{"recipient_id": to, "queued": true}}, nil
	})
}

type broadcastPeersInput struct {
	Text string `json:"text" jsonschema:"the message"`
}

func (m *mcpTools) broadcastPeers() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:        "broadcast_peers",
		Title:       "Message every peer agent",
		Description: "Sends one message to every other Legato agent running in the same Git worktree as you, for example to announce the files you are working on. The result names who received it and who could not be reached.",
	}, func(ctx context.Context, in broadcastPeersInput) (mcpserver.Result, error) {
		if _, err := m.session(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		text := strings.TrimSpace(in.Text)
		if text == "" {
			return mcpserver.Result{}, fmt.Errorf("broadcast_peers needs a message")
		}
		result, err := m.opts.Agents.BroadcastToPeers(ctx, m.opts.TaskID, m.opts.ExecutionID, "", text)
		if err != nil {
			return mcpserver.Result{}, err
		}
		var out strings.Builder
		if len(result.Delivered) == 0 && len(result.Failed) == 0 {
			out.WriteString("No other agent is running in this worktree.")
		}
		if len(result.Delivered) > 0 {
			fmt.Fprintf(&out, "Delivered to: %s.", strings.Join(result.Delivered, ", "))
		}
		for _, failure := range result.Failed {
			fmt.Fprintf(&out, "\nNot delivered to %s: %s", failure.TaskID, failure.Error)
		}
		return mcpserver.Result{Text: strings.TrimSpace(out.String()), Data: result}, nil
	})
}

// htmlGuide tells an agent what the frame its HTML is drawn in allows.
func (m *mcpTools) htmlGuide() string {
	guide := "HTML is drawn in a sandboxed frame with Legato's theme: inline <style> and <script> work, and CSS variables such as " +
		"--color-background-primary, --color-text-primary, --color-text-secondary, --color-border-primary, --font-sans and --font-mono match the app. " +
		"The frame cannot fetch, submit forms or open windows. "
	if len(m.opts.ResourceDomains) > 0 {
		guide += "Scripts, styles, fonts and images may load from: " + strings.Join(m.opts.ResourceDomains, ", ") + ". "
	} else {
		guide += "Nothing loads from the network, so keep every script, style and image inline. "
	}
	guide += "Call legato.proposeMessage(text) from a script to put text in the person's message box for them to send, and legato.openLink(url) to offer a link."
	return guide
}
