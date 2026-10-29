package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/libretto"
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
	// ImageDir is where Legato keeps its copies of the image files an agent
	// shows. Without it the tools turn image files down.
	ImageDir string
	// VideoDir is the same for video files. Without it show_video turns
	// every video down.
	VideoDir string
	// Libretto says what the libretto installed on this machine can do,
	// which show_studio asks before it opens a studio. Nil asks the command
	// on PATH.
	Libretto LibrettoProbe
	// Credentials is the credential broker. Without it, or with no
	// credentials configured, request_access is not offered.
	Credentials MCPCredentials
}

// LibrettoProbe asks the installed libretto what it can do.
type LibrettoProbe interface {
	Probe(ctx context.Context) (libretto.Info, error)
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

- When your answer is mainly data (figures, a status, a comparison of options, a ranking, progress, a small chart), prefer a card to a Markdown table or a list of numbers. show_components is the simplest way: you name ready-made parts and Legato draws them to match the app. When those parts cannot say it well, call show_card and design the card yourself.
- When you have an image file to show (a screenshot you took, a chart you rendered, a photo, a design mock-up), show the image; do not describe it. The image and image_grid components take the file's path and need nothing else: show_components puts them in the conversation, and show_artifact takes the same components for a gallery in the side panel. In HTML or Markdown of your own, list the files in images and write legato-image:<name> where an image's address goes.
- When your answer is long or has structure (an architecture, an explanation that needs a diagram, a design, a plan, a report, anything longer than about a screen), prefer an artifact to a long reply: call show_artifact and it opens beside the conversation, where the person can keep it open. Call it again with the same key to revise it.
- Custom HTML is yours to use in full when it gives the person a better experience: your own layout and CSS, inline SVG, JavaScript for interaction, a diagram or chart library. An architecture or an explanation is a page the person explores, not a document they scroll: let a Mermaid diagram carry the structure, style it as your own, and show the detail of a part when they click it. Never draw boxes with arrows typed as text. Markdown is for plain prose.
- When a walk-through is better watched than read (how a change works, a tour of code the person does not know, what you built and why) and the libretto command is installed, make a narrated video. libretto --help describes a storyboard and libretto schema gives every field; libretto voices lists the voices, of which the kokoro and piper ones are local and free. Check the storyboard with libretto validate, which also estimates its length, and fix what it warns about; render it with --no-captions (with a paid voice, check a free libretto render --silent first); look at the .contact.png beside the video, and at one moment full size with libretto frame; then call show_video with the manifest render wrote. The person can pause the video and ask you about what is on screen; such a question comes with the time and what the video showed then.
- When the person wants to present something in their own voice (demo a feature, walk through their own work) and the libretto command is installed, open a studio with show_studio and plan it with them there. Write a first plan at once and call show_studio with it: the brief in their words, and scenes that each have notes and a shows line saying what the picture will be, with no pictures drawn yet. Shape it as you talk, and in the studio they can rewrite, move, add and remove scenes themselves. Draw the pictures only when they agree the plan, which a [legato studio] message tells you. Until then, when they say to add a part, drop one, or spend longer on something, change the storyboard file straight away, and the studio beside the conversation shows the change within a couple of seconds. Call show_studio again with scene to turn it to the part you are discussing. While they record, a message that starts [legato studio] tells you of each take: review it at once with libretto transcribe and libretto review, and bring that scene in line with what they said, without talking over them. They can leave you comments on a scene there, and remove a scene: both are written into the storyboard file as comments, so read it again before each edit, do what they ask, and delete each comment when it is done. The notes are talking points, two to four short lines starting with "- " that name the details to get across; they are not a script, and you never write the person sentences to read. They record each scene in the studio, speaking in their own words. When they ask you to line it up, read what they said in each take with libretto transcribe and edit the storyboard to follow them. Expect them to leave the plan, and treat that as direction, not as a mistake: add a picture for what they went into (a terminal for commands they named, a diagram, the code they mentioned), drop what they skipped, and put a marker before each word where something should happen. Do not ask for another take because they went off script. Then render it and call show_video.
- After you show something, say in one sentence what it is, and do not repeat its content.
- A short, plain answer stays plain text.

Also yours to use without being asked:
- task_context: read the task you are working on, with its notes, when you need the brief or what earlier work left behind.
- add_task_note: record a finding or a decision that should outlast this session.
- list_peers, message_peer, broadcast_peers: see and coordinate with other agents working in the same Git worktree.
- session_info: which session, task and working directory you are in.
- request_access: when the task needs a credential Legato holds (an API key, a login), ask for exactly the scope you need, with your reason, before you need it. If the answer is pending, carry on with other work, or stop and end your turn when you are blocked; a [legato approvals] message tells you the decision. Never look for the secret another way.`

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
		ui: service.NewAgentUIService(opts.Store, nil, service.AgentUIOptions{
			ResourceDomains: opts.ResourceDomains, ImageDir: opts.ImageDir, VideoDir: opts.VideoDir,
		}),
	}
	server.Instructions = MCPGuidance
	if opts.Credentials != nil {
		server.Instructions += CredentialGuidance(opts.Credentials.Credentials())
	}
	server.Tools = tools.all()
	return server
}

// MCPToolNames returns the name of every tool the server can offer, for an
// agent tool that must be told which calls to run without asking.
func MCPToolNames() []string {
	// The tools are only named here, never called, so they need nothing real
	// behind them; the peers are present so that the peer tools are counted.
	tools := (&mcpTools{opts: MCPOptions{Agents: noPeers{}, Credentials: noCredentials{}}}).all()
	names := make([]string, 0, len(tools)+1)
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	// request_access is offered only with credentials; it is named always.
	if !listHasName(names, "request_access") {
		names = append(names, "request_access")
	}
	return names
}

func listHasName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
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
		m.showArtifact(), m.showCard(), m.showComponents(), m.showVideo(), m.showStudio(),
		m.sessionInfo(), m.taskContext(), m.addTaskNote(),
	}
	if m.opts.Agents != nil {
		tools = append(tools, m.listPeers(), m.messagePeer(), m.broadcastPeers())
	}
	if m.opts.Credentials != nil && len(m.opts.Credentials.Credentials()) > 0 {
		tools = append(tools, m.requestAccess())
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

type showStudioInput struct {
	Storyboard string `json:"storyboard" jsonschema:"the storyboard file, as an absolute path. Its scenes have notes, what each should get across, where a scene for a voice has narration"`
	Title      string `json:"title" jsonschema:"a short name for what is being recorded, shown on the studio's tab"`
	Key        string `json:"key,omitempty" jsonschema:"a name of your own for this studio; calling again with the same key shows the same studio, not another"`
	Scene      string `json:"scene,omitempty" jsonschema:"the id of a scene to put in front of the person: the studio turns to it. Use it when the conversation moves to one part of the plan"`
}

func (m *mcpTools) showStudio() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "show_studio",
		Title: "Open a studio for the person to record in",
		Description: "Opens a libretto studio in a panel beside the conversation: the plan of a video the person will present in their own voice, and the place they record it, one scene at a time. " +
			"Use it without being asked when the person wants to present something themselves: demo a feature, walk through their own work, narrate a video in their own voice. " +
			"Open it early, with a first plan, and agree the plan with them before anything is drawn. A plan is a storyboard with a brief (what they want to cover, in their words) and scenes that each have notes and a shows line: " +
			"a few words on what the scene's picture will be, such as \"A terminal running restack on a three-branch stack\". A scene with shows and no picture is planned: the studio draws a placeholder, and render waits. " +
			"Draw no pictures at this stage, because they will change the plan: the studio's Plan view lets them rewrite a scene's points and its shows line, move and remove scenes, rename chapters and add a scene from one line, all of which land in the storyboard file. " +
			"When they write or change the brief in the studio you are told, and draft the scenes from it. When they agree the plan you are told, and only then give each planned scene its picture (keep its shows line), after which they record. " +
			"The notes are talking points, two to four short lines that each start with \"- \": the details to get across, not sentences to read out. Never write the person a script, and give such a scene no narration. " +
			"Once the plan is agreed, draw each scene's picture in full (a terminal's commands, a diagram, the code) and leave out actions that wait for a marker; those come after the take. List the names they will say (products, commands, people) in the storyboard's vocabulary, so the transcript spells them right. libretto --help and libretto schema give the format; check with libretto validate. " +
			"The storyboard file is the one copy, and the studio reads it again every couple of seconds. So when the person says to add a part, drop one, reorder, or go deeper on something, change the file there and then: they see it change, with what is new marked. " +
			"Call this tool again with scene to turn the studio to the scene you are talking about. " +
			"The person can also write to you in the studio. A scene's comments in the storyboard are what they want changed there, and the storyboard's own comments say what they did, such as removing a scene. " +
			"Read the file again before every edit, do what each comment asks, and delete the comment when it is done; libretto validate lists the ones still waiting. " +
			"While they record, Legato tells you of each take in a message that starts [legato studio]. Act on it without being asked, and keep out of the conversation while they present: " +
			"read the take with libretto transcribe, write what you make of it with libretto review (where they covered each talking point, and each stretch that left the plan, with a note to them), " +
			"and bring that scene's narration, markers and cuts in line with what they said. The studio shows your review over their transcript and asks them what to do with what left the plan; " +
			"a scene is added, removed or split only when they have decided, which reaches you as a comment on the scene. " +
			"If they turned those messages off they will ask you to make the video from their takes instead. Either way, read each take with libretto transcribe, then edit the storyboard to follow them: " +
			"they will have left the plan, which is direction and not a mistake. Add a picture for what they talked about (a terminal for commands they named, a diagram, the code), remove what they skipped, " +
			"cut a take into more than one scene where the subject changes, leave out a false start or a stumble with the take's skip, and put a marker before each word where something should happen. " +
			"Then render with --no-captions and show the result with show_video. " +
			"The studio needs libretto on this machine; when it is missing or too old the tool says so, and you tell the person.",
		ReadOnly: true,
	}, func(ctx context.Context, in showStudioInput) (mcpserver.Result, error) {
		storyboard := strings.TrimSpace(in.Storyboard)
		if storyboard == "" {
			return mcpserver.Result{}, fmt.Errorf("show_studio needs storyboard: the path of the storyboard file to record")
		}
		if !filepath.IsAbs(storyboard) {
			return mcpserver.Result{}, fmt.Errorf("storyboard %q must be an absolute path", storyboard)
		}
		if info, err := os.Stat(storyboard); err != nil || !info.Mode().IsRegular() {
			return mcpserver.Result{}, fmt.Errorf("there is no storyboard file at %s", storyboard)
		}
		// Asked before anything is shown: a panel that can only say libretto
		// is missing is no use to the person, and the agent is the one who
		// can tell them what to do about it.
		probe := m.opts.Libretto
		if probe == nil {
			probe = libretto.Runner{}
		}
		if _, err := probe.Probe(ctx); err != nil {
			return mcpserver.Result{}, fmt.Errorf("the studio cannot open. %s Tell the person this; you cannot fix it for them", libretto.Explain(err))
		}
		return m.show(ctx, service.AgentUIShowInput{
			Placement: service.AgentUIPanel, Format: service.AgentUIFormatStudio, Title: in.Title, Key: in.Key,
			Content: service.AgentUIStudioContent(storyboard, in.Scene),
		}, fmt.Sprintf("Opened the studio %q in Legato's side panel, for the person to record in", strings.TrimSpace(in.Title)))
	})
}

type showArtifactInput struct {
	Title      string                      `json:"title" jsonschema:"a short name for the artifact, shown on its tab"`
	Content    string                      `json:"content,omitempty" jsonschema:"the whole artifact: an HTML document or fragment, or Markdown. Leave it out when you pass components"`
	Format     string                      `json:"format,omitempty" jsonschema:"html (default) or markdown, for content"`
	Components []service.UIComponent       `json:"components,omitempty" jsonschema:"in place of content: the artifact as Legato's own components, top to bottom, as show_components takes them"`
	Images     []service.AgentUIImageInput `json:"images,omitempty" jsonschema:"image files the content uses; write legato-image:<name> in the content where an image's address goes"`
	Key        string                      `json:"key,omitempty" jsonschema:"a name of your own for this artifact; showing again under the same key replaces it as a new revision instead of adding another"`
}

func (m *mcpTools) showArtifact() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "show_artifact",
		Title: "Show an artifact",
		Description: "Shows a page of your own making in a panel beside the conversation in Legato, where the person can read it while you carry on. " +
			"Use it without being asked for something worth more than a reply: an architecture, an explanation with diagrams, a design, a report, a comparison, a set of screenshots. " +
			"Make it a page the person explores, not a document: a short summary at the top, a diagram that carries the structure (Mermaid for the layout, or SVG), " +
			"your own styling, and scripts for tabs, sections that open and the detail of a part shown when the person clicks it. " +
			"The panel is narrow (about 500px, and a phone is narrower): use one column and short node labels, and keep the detail for what opens on a click. " +
			"format markdown is for plain prose only; a ```mermaid fence in it is drawn as a diagram, and ![what it shows](legato-image:<name>) is an image from images. " +
			"When ready-made parts say it well (a gallery of image files, figures, a table), pass components in place of content: the same components show_components takes, and no HTML to write. " +
			m.htmlGuide(),
		ReadOnly: true,
	}, func(ctx context.Context, in showArtifactInput) (mcpserver.Result, error) {
		format := strings.TrimSpace(in.Format)
		hasContent := strings.TrimSpace(in.Content) != ""
		switch {
		case len(in.Components) > 0 && hasContent:
			return mcpserver.Result{}, fmt.Errorf("show_artifact takes content or components, not both")
		case len(in.Components) > 0 && format != "" && format != service.AgentUIFormatComponents:
			return mcpserver.Result{}, fmt.Errorf("format %s is for content; leave format out when you pass components", format)
		case len(in.Components) > 0:
			format = service.AgentUIFormatComponents
		case !hasContent:
			return mcpserver.Result{}, fmt.Errorf("show_artifact needs content (HTML or Markdown) or components")
		case format == "":
			format = service.AgentUIFormatHTML
		}
		return m.show(ctx, service.AgentUIShowInput{
			Placement: service.AgentUIPanel, Format: format, Title: in.Title, Key: in.Key,
			Content: in.Content, Components: in.Components, Images: in.Images,
		}, fmt.Sprintf("Shown in Legato's side panel as %q", strings.TrimSpace(in.Title)))
	})
}

type showVideoInput struct {
	Manifest string `json:"manifest" jsonschema:"the .manifest.json file libretto render wrote beside the video, as an absolute path"`
	Video    string `json:"video,omitempty" jsonschema:"the video file, as an absolute path. Leave it out for the video the manifest names, in the manifest's own folder"`
	Title    string `json:"title" jsonschema:"a short name for the video, shown on its tab"`
	Key      string `json:"key,omitempty" jsonschema:"a name of your own for this video; showing a new render under the same key replaces it as a new revision instead of adding another"`
}

// mcpVideoManifest is the one field of a manifest the tool itself reads: the
// name of the video file beside it.
type mcpVideoManifest struct {
	Video string `json:"video"`
}

func (m *mcpTools) showVideo() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "show_video",
		Title: "Show a narrated video",
		Description: "Shows a narrated video you rendered with libretto in a panel beside the conversation in Legato, with its chapters and captions. " +
			"The person can pause it and ask you about that moment: the question then arrives with the time, the sentence being said and what was on screen, such as the file and the highlighted lines. " +
			"Use it without being asked when a walk-through is better watched than read: how a change works, a tour of code, what you built and why. " +
			"To make one: write a storyboard (run libretto --help and libretto schema for the format), check it with libretto validate, " +
			"then render with a voice and --no-captions, because Legato draws the captions itself. A local voice is free; with a paid one, check a libretto render --silent first. " +
			"Look at the .contact.png it writes before you show the video. " +
			"Pass the .manifest.json that render wrote beside the video. " +
			"To change a video after a question, edit the storyboard, render again and call this tool with the same key: unchanged scenes come from the cache.",
		ReadOnly: true,
	}, func(ctx context.Context, in showVideoInput) (mcpserver.Result, error) {
		manifestPath := strings.TrimSpace(in.Manifest)
		if manifestPath == "" {
			return mcpserver.Result{}, fmt.Errorf("show_video needs manifest: the .manifest.json file libretto render wrote beside the video")
		}
		info, err := os.Stat(manifestPath)
		if err != nil || !info.Mode().IsRegular() {
			return mcpserver.Result{}, fmt.Errorf("there is no manifest file at %s", manifestPath)
		}
		if info.Size() > service.AgentUIMaxContentBytes {
			return mcpserver.Result{}, fmt.Errorf("%s is %d bytes; a manifest may be %d at most", manifestPath, info.Size(), service.AgentUIMaxContentBytes)
		}
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			return mcpserver.Result{}, fmt.Errorf("%s cannot be read", manifestPath)
		}
		video := strings.TrimSpace(in.Video)
		if video == "" {
			var manifest mcpVideoManifest
			if err := json.Unmarshal(content, &manifest); err != nil || strings.TrimSpace(manifest.Video) == "" {
				return mcpserver.Result{}, fmt.Errorf("%s names no video file; pass video, or pass the .manifest.json that libretto render wrote", manifestPath)
			}
			video = manifest.Video
			if !filepath.IsAbs(video) {
				video = filepath.Join(filepath.Dir(manifestPath), video)
			}
		}
		return m.show(ctx, service.AgentUIShowInput{
			Placement: service.AgentUIPanel, Format: service.AgentUIFormatVideo, Title: in.Title, Key: in.Key,
			Content: string(content), Video: video,
		}, fmt.Sprintf("Shown in Legato's side panel as the video %q", strings.TrimSpace(in.Title)))
	})
}

type showCardInput struct {
	HTML         string                      `json:"html" jsonschema:"the card: an HTML fragment with its own inline style, sized by its content"`
	FallbackText string                      `json:"fallback_text" jsonschema:"the same information as plain text, for where the card cannot be drawn"`
	Title        string                      `json:"title,omitempty" jsonschema:"a short name for the card"`
	Images       []service.AgentUIImageInput `json:"images,omitempty" jsonschema:"image files the card uses; write legato-image:<name> in the HTML where an image's address goes"`
	Key          string                      `json:"key,omitempty" jsonschema:"a name of your own for this card; showing again under the same key keeps one card and adds a revision"`
}

func (m *mcpTools) showCard() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "show_card",
		Title: "Show a card",
		Description: "Shows a card of your own design in the conversation in Legato, in place of text. Use it without being asked when an answer is better seen than read " +
			"and show_components does not have the parts for it: a custom layout, an illustration or a small diagram in SVG, a chart, something the person can click through. " +
			"Write the card as an HTML fragment sized by its content. " +
			m.htmlGuide(),
		ReadOnly: true,
	}, func(ctx context.Context, in showCardInput) (mcpserver.Result, error) {
		return m.show(ctx, service.AgentUIShowInput{
			Placement: service.AgentUIInline, Format: service.AgentUIFormatHTML,
			Title: in.Title, Key: in.Key, Content: in.HTML, FallbackText: in.FallbackText, Images: in.Images,
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
			"Use it without being asked when an answer is better seen than read. It is the simplest way to show figures, a status, tables, lists, progress and simple bar charts; " +
			"when these components cannot say it well, use show_card and design the card yourself.\n\n" + service.UIComponentGuide,
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

// mermaidHosts are the hosts the web UI's frame can load Mermaid from
// (MERMAID_SOURCES in web/src/lib/agentUi.ts). With none of them allowed the
// frame cannot draw a diagram, and an agent must not be told that it can.
var mermaidHosts = []string{"cdn.jsdelivr.net", "unpkg.com"}

// drawsDiagrams reports whether a frame that may load from these hosts can
// draw Mermaid diagrams.
func drawsDiagrams(domains []string) bool {
	for _, domain := range domains {
		for _, host := range mermaidHosts {
			if domain == host {
				return true
			}
		}
	}
	return false
}

// diagramGuide teaches Legato's way to draw a Mermaid diagram in the frame.
// An agent that loads and runs Mermaid itself gets a broken diagram inside a
// hidden tab, Mermaid's own look, and click handlers that do not find their
// node, since Mermaid's names for nodes change between releases. Legato's
// frame does those for it. The example was run in the frame as written.
const diagramGuide = "Legato draws Mermaid for you: write <pre class=\"mermaid\">…</pre> and do not load Mermaid yourself. " +
	"It is drawn in the app's theme, inside a hidden tab too. " +
	"Add :::blue, :::cyan, :::green, :::amber, :::red, :::pink, :::purple or :::gray to a node to color it, so kinds of thing can be told apart; classDef works for a style of your own. " +
	"Call legato.diagrams({ onNode(id, element) { … } }) once to make every node a button: id is the node's name in your source, and the chosen node gets aria-pressed=\"true\". " +
	"legato.selectNode(id) chooses a node from your own script, for a default choice or a link between parts. " +
	"Example: <pre class=\"mermaid\">flowchart TB; api[API]:::blue --> db[(Database)]:::cyan</pre><p id=\"about\">Select a part.</p>" +
	"<script>const about = { api: \"Checks the token and routes the call.\", db: \"One container for each domain.\" }; " +
	"legato.diagrams({ onNode: (id) => { document.getElementById(\"about\").textContent = about[id] } })</script> "

// imageGuide teaches how an image file from disk gets into a page. The frame
// cannot read the disk or reach Legato's API, so Legato copies the file and
// the page names it; what a click on an image does is Legato's, so an agent
// need not write a lightbox to make a screenshot readable.
const imageGuide = "To use an image file from disk (a screenshot, a chart you rendered), list it in images as {name, path} and write legato-image:<name> wherever its address goes: " +
	"<img src=\"legato-image:board\" alt=\"The board\">, or a CSS url(). In a script, legato.image(name) returns the address. Legato keeps its own copy of the file. " +
	"The person can click such an image to see it full size and step through the page's other images, so a small one is enough; put data-legato-zoom=\"off\" on an image that should not open. " +
	"A container with class=\"legato-image-grid\" lays its images, or its <figure>s with a <figcaption>, out as a grid of thumbnails. "

// htmlGuide tells an agent what the frame its HTML is drawn in allows. Each
// statement was checked in a browser: scripts, inline SVG and libraries from
// the allowed hosts work; storage, workers and the network do not. An agent
// told only what is forbidden writes boxes and text.
func (m *mcpTools) htmlGuide() string {
	guide := "HTML is drawn in a sandboxed frame with Legato's theme. Your own <style> and <script> run, and inline SVG and canvas work. " +
		"CSS variables such as --color-background-primary, --color-text-primary, --color-text-secondary, --color-border-primary, --font-sans and --font-mono match the app. "
	if len(m.opts.ResourceDomains) > 0 {
		guide += "Scripts, styles, fonts and images may load from: " + strings.Join(m.opts.ResourceDomains, ", ") +
			". Use that for a chart library (D3, Chart.js) at a pinned version. "
		if drawsDiagrams(m.opts.ResourceDomains) {
			guide += diagramGuide
		}
	} else {
		guide += "Nothing loads from the network, so keep every script, style and image inline. "
	}
	guide += imageGuide
	guide += "The frame has no storage (localStorage and cookies throw), no workers, and cannot fetch, submit forms or open windows, so put the data in the page. " +
		"Call legato.proposeMessage(text) from a script to put text in the person's message box for them to send, and legato.openLink(url) to offer a link."
	return guide
}
