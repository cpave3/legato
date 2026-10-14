# Agent tools (the Legato MCP server)

Every agent Legato launches gets a set of Legato's own tools, through an MCP server that Legato adds to the session at launch: `legato mcp serve`. With them an agent can show something in the web UI (an **artifact** in a side panel, or a **card** in the conversation in place of text), put image files from disk in what it shows, read the task it is working on, record a note on it, and message the other agents in its worktree. Nothing is installed by hand and nothing is written to the agent tool's own configuration.

The same server works for Claude Code, Codex and Chimera, and for any later agent tool that speaks MCP. It was built and verified against Claude Code 2.1.296, Codex 0.157.1 and a Chimera with `--mcp-config`.

## How it fits together

```
 agent session, in its tmux pane                                       Legato
 ┌────────────────────────────────────────┐                  ┌──────────────────────────┐
 │ Claude Code / Codex / Chimera          │                  │ SQLite store             │
 │   │ tools/call over stdio              │   writes a row   │  agent_ui_items          │
 │   ▼                                    │─────────────────▶│  agent_ui_item_revisions │
 │ legato mcp serve  (a child of the      │                  └────────────┬─────────────┘
 │ agent tool; finds its session from     │   IPC                         │ read
 │ the pane's environment)                │   agent_ui_changed            ▼
 └────────────────────────────────────────┘─────────────────▶ web server ── WebSocket ──▶ web UI
                                                              GET /api/ui-items/{id} ◀──── (JSON)
```

The server is a short-lived child of the agent tool, like the CLI commands the hooks run. It opens the store, writes, and broadcasts IPC to running Legato instances. It needs no web server: something shown while no browser is open is there when one opens.

Legato is not the MCP host. The agent tool is, and a terminal agent cannot draw UI. So the UI does not travel through the MCP client at all: Legato's own server hands it to Legato, and Legato's web UI draws it.

## The server

`legato mcp serve [--task <id>] [--execution-id <id>]` speaks MCP on stdin and stdout (diagnostics go to stderr). The flags win; otherwise the session is the one named by `LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID`, which Legato puts in every pane.

- **Outside a session** (no task ID) it offers no tools, and its instructions say why.
- **Inside one**, the tools are listed at once, and each call looks the session up again: the task must have a running agent session, and a given execution ID must be that session's. The lookup is per call, not at start, because an agent tool starts its MCP servers while Legato may still be recording the session it launched. A refused call returns a tool error and stores nothing.

| Tool | Arguments | What it does |
| --- | --- | --- |
| `show_artifact` | `title`, `key`, and either `content` with `format` (`html` default, or `markdown`) and `images`, or `components` | Stores an artifact and opens it in the side panel. |
| `show_card` | `html`, `fallback_text`, `title`, `images`, `key` | Stores an HTML card, drawn in the conversation at the call. |
| `show_components` | `components`, `fallback_text`, `title`, `key` | Stores a card built from Legato's own components (see below). |
| `show_video` | `manifest`, `title`, `video`, `key` | Stores a narrated video an agent rendered with libretto and opens it in the side panel (see Videos). |
| `show_studio` | `storyboard`, `title`, `key`, `scene` | Opens a libretto studio on a storyboard beside the conversation: the plan of a video the person will present, and where they record it (see Studios). Called again with `scene`, it turns the studio to that scene. It fails with what to tell the person when libretto is missing or too old. |
| `session_info` | none | Task ID and title, agent tool, swarm role, working directory. |
| `task_context` | none | The task as Markdown: description, linked ticket, repository details, durable notes. |
| `add_task_note` | `body` | Records a durable Markdown note (`task_notes`, source `agent`). |
| `list_peers` | none | Other agents in the same Git worktree. |
| `message_peer` | `to`, `text` | Queues a message to one peer. |
| `broadcast_peers` | `text` | Messages every peer; the result names who was and was not reached. |

A swarm worker's own identity is its sub-task, which has no description or notes, so `task_context` and `add_task_note` act on the swarm's parent task. The peer tools need tmux; without it the other tools still work and those three are not offered.

**Keys and revisions.** Showing again under a `key` the task already used in that placement replaces the item's content as a new revision instead of adding another item. An artifact and a card may share a key; they are different items.

**Results.** A display tool returns text only, with a marker the web UI reads: `Shown as a card in the conversation [legato-ui:ui-3e93f1138d:1]. …`. It returns no structured content on purpose: Claude Code hands its model a result's structured content *in place of* its text, which would hide both the marker and the guidance. The other tools return text and the same result as structured content.

**Schemas.** Tool input schemas are inferred from Go structs. Go slices and pointers infer as "this type or null"; `internal/engine/mcpserver` rewrites those to the type alone, because a tool's schema is passed on to model providers and several of those reject a property that names two types.

## How each agent gets the server

`agents.mcp.enabled` (default `true`) turns injection on for all three adapters, in `configuredAdapters`.

| Agent | How the server is added | Identity reaches the server | Tool names the model sees |
| --- | --- | --- | --- |
| Claude Code | A third plugin, `legato-tools` (`internal/engine/hooks/claudemod/tools/`), installed to `<data_dir>/claude-mod/legato-tools` and added to the pane's `CLAUDE_CODE_PLUGIN_DIRS`. Its `.mcp.json` starts `${LEGATO_BIN} mcp serve`. | Claude Code gives a plugin's MCP server the session's whole environment. | `mcp__plugin_legato-tools_legato__show_card` |
| Codex | Launch overrides: `-c mcp_servers.legato.command=…`, `….args=["mcp","serve"]`, `….env_vars=[…]`. `legato agent codex` passes every `-c` to the session's app server, which is the process that starts MCP servers. | Codex starts an MCP server with a short list of variables; `env_vars` adds the ones in `mcpServerEnv` (session IDs, `LEGATO_CONFIG`, `LEGATO_HOME`, the XDG directories, tmux). | `mcp__legato__show_card` (`legato.show_card` in the native panel) |
| Chimera | `--mcp-config '{"mcpServers":{"legato":{…}}}'`: servers for that session only, merged over the global config (see Chimera's `docs/mcp.md`). | The entry's `env` map (`LEGATO_TASK_ID: env:LEGATO_TASK_ID`) makes Chimera pass its whole environment. | `mcp_legato_show_x00005f_card` |

- The plugin is separate from the bridge mod, so the tools do not depend on `native_bridge`, and a change to it reloads no bridge. Because it is loaded from the pane's environment, a `claude` the person starts by hand in the pane gets the tools too.
- The overrides come before the person's `launch_args`, so an entry of their own for a server named `legato` wins.
- A Chimera from before the flag would refuse to start at all, so the adapter reads `chimera --help` once, at the first launch that needs it, and leaves an older Chimera alone. A `--mcp-config` in the person's own `launch_args` replaces Legato's: Chimera takes the flag once.

**No approval prompt.** Each agent tool is told to run these tools without asking:

- Claude Code: a `PreToolUse` hook in the plugin answers `allow` for `mcp__plugin_legato-tools_legato__.*` and nothing else. (The plugin's second hook is about `Read` and pasted images, not about these tools; see [Claude Code native integration](claude-code-native.md) § Legato's tools in a session.)
- Codex: the tools are annotated as not destructive and closed-world; Codex runs them without an approval request.
- Chimera: the entry's `trustedTools` lists every tool name (`cli.MCPToolNames()`).

## How an agent learns that the tools exist

An agent should reach for the tools by itself. One text says so, `cli.MCPGuidance`, and it reaches each agent tool's model by the route that tool supports:

| Agent | Route | Why |
| --- | --- | --- |
| Claude Code | The server's own MCP `instructions`. | Claude Code puts a server's instructions in the model's context for every session that loads the server, including one started by hand, after `/clear`, and on resume. |
| Codex | The launch prompt: `-c developer_instructions=…`. | Codex does not pass server instructions to its model, and it keeps MCP tools out of the model's tool list until they are looked up, so tool descriptions cannot do the job either. |
| Chimera | The launch prompt: the first turn (`--prompt`). | Chimera does not read server instructions. |

Adapters whose tool needs the launch prompt implement `service.ToolsPromptAdapter`. `AgentService.SpawnAgent` adds `ToolsPrompt()` to the prompt every launch injects, after the role or general prompt, so a plain Codex session that had no launch prompt now gets one. An adapter returns the text only when its session really gets the server: not when the setting is off, Chimera is too old, or the person's own `--mcp-config` replaced Legato's.

The guidance names tools by their own names, since each agent tool adds a prefix of its own, and it gives concrete triggers: prefer a card to a Markdown table or a list of numbers; prefer an artifact to a reply longer than about a screen; after showing something, say in one sentence what it is.

**Which form, and how far to go.** The guidance and the tool descriptions say two things together:

- `show_components` is the simplest way, and the right one when its ready-made parts say it well.
- Custom HTML is there to be used in full when it gives the person a better experience: the agent's own layout and CSS, inline SVG, JavaScript for interaction, a diagram or chart library. An architecture or an explanation is a page the person explores, not a document they scroll: a Mermaid diagram carries the structure, the agent styles it, and the detail of a part shows when the person clicks it. Markdown is for plain prose.

The second point exists because of what agents did without it, in two steps:

1. Told only what the frame forbids, an agent asked for an architecture wrote 11 KB of HTML with no SVG, no script and no library: boxes in a grid, with the connections typed as `↓` and `→`. The capability was there and unused. The descriptions of the two HTML tools therefore say what *works* in the frame (see The sandbox).
2. Told that Mermaid exists, the same kind of agent made a Markdown document with a flowchart in it: correct, and dull. `show_artifact` now asks for a page that is designed and can be explored, says that the panel is narrow (one column, short node labels, the detail in what opens on a click), and keeps `format: markdown` for plain prose. The HTML tools teach Legato's own way to draw a diagram (see Diagrams in a frame), with an example that was run in the frame; with no host allowed that serves Mermaid (`drawsDiagrams`), they do not mention it.

Checked after the second step: a Chimera agent, asked to "show me its architecture" with no hint about format, made an HTML page with a summary, four tabs, four Mermaid diagrams with named colors, and `legato.diagrams({ onNode })` showing a part's detail and source paths on a click. It loaded no library of its own. In the panel the three diagrams in hidden tabs drew at full size, and the downloaded file behaved the same. A Claude Code agent on its default model, given the same prompt, made a page with five tabs, five diagrams, a walk-through of the parts and a detail sheet, using `legato.diagrams`, `legato.selectNode` and `legato.proposeMessage`; all five diagrams drew. Before it showed the page it parsed its diagrams and ran its script against a stand-in for the helper, since an agent cannot see what it drew. That is one run with each agent; Codex was not run.

The loop used to get here (measure the output, name the gap, prove the target by hand, change the smallest thing, verify with a real agent) is the repository skill `.claude/skills/improve-agent-artifacts`, with the scripts it needs.

Checked with a question that names no tool ("compare these three search backends"): Codex, Chimera and Claude Code on its default model each answered with a component card. A small model (Claude Haiku) answered in text wherever the guidance was placed.

**About prompts and messages in general.** A launch-time system addendum has been reliable since it became a launch flag, not typed text: `--append-system-prompt` (Claude Code), `developer_instructions` (Codex, given to the app server by the launcher) and `--prompt` (Chimera). What the native bridges added is reliable delivery of *messages after launch* (the kickoff, peer notices): they are submitted over the session's endpoint instead of being typed. Server instructions go one step further for Claude Code, because they do not depend on Legato typing the launch command at all.

## UI items

`internal/engine/store/agent_ui.go`, migration `063_agent_ui_items.sql`.

- `agent_ui_items`: `id` (`ui-<10 hex>`), `task_id`, `execution_id`, `placement` (`panel` or `inline`), `item_key`, `revision`, timestamps. `(task_id, placement, item_key)` is unique when the key is set.
- `agent_ui_item_revisions`: `(item_id, revision)`, `format` (`html`, `markdown`, `components`, `video`, `studio`), `title`, `content`, `fallback_text`, and `images` (migration `064_agent_ui_item_images.sql`): the revision's files as JSON, `[{name, file}]`, empty when it has none. They are its image files, or for a `video` item its one video file, under the name `video`.

`task_id` is the agent's own identity, which is a sub-task ID for a swarm worker, so it has no foreign key: `Store.DeleteTask` removes a task's items and its workers', and `Store.DeleteSubtask` a worker's.

`service.AgentUIService` owns the rules (`internal/service/agent_ui.go`):

- Content is at most 1 MiB of valid UTF-8. An artifact needs a title; a card needs `fallback_text`, the same information as plain text.
- An artifact is HTML, Markdown, components, a video or a studio; a card is HTML or components.
- A task keeps 200 items. Past that, the items changed longest ago are removed, so a long session never reaches a point where nothing more can be shown. A card whose item is gone falls back to its text.
- An item keeps its newest 20 revisions.

## Component cards

`show_components` takes a list of components from a fixed catalog (`internal/service/agent_ui_components.go`, `UIComponentGuide`): `heading`, `text` (Markdown), `stat`, `stats`, `key_value`, `table`, `list`, `badges`, `progress`, `bar_chart`, `code`, `image`, `image_grid`, `divider` and `actions`. `show_artifact` takes the same list as `components`, in place of `content`, and the artifact is then drawn the same way in the side panel. Each is a flat object whose `type` says which fields apply; `tone` is `neutral`, `positive`, `warning`, `negative` or `info`. The service checks every component and names the one that is wrong (`components[1] (table): rows[0] has 1 cells; the table has 2 columns`), so the agent can correct the call.

The web UI draws them with its own elements (`AgentUIComponents`). Nothing the agent wrote runs; every value is text. A component card costs the agent far fewer tokens than HTML and always matches the app; HTML is for what the catalog cannot express.

## Image files

An agent can put an image file from disk in what it shows: a screenshot it took, a chart it rendered. There are two ways to name the file, and one way it is kept and served.

**Naming the file.**

| Where | How |
| --- | --- |
| Components | `{type: "image", path, alt, caption}` and `{type: "image_grid", items: [{path, alt, caption}]}` (1 to 24 images) take the file's path directly. This is the way that needs nothing else. |
| HTML and Markdown | `images: [{name, path}]` on `show_artifact` and `show_card` lists the files, and the content writes `legato-image:<name>` where an image's address goes: `<img src="legato-image:board">`, a CSS `url()`, `![The board](legato-image:board)`. A name is 1 to 64 letters, digits, dashes and underscores. |

- A path should be absolute. A relative one is taken from the working directory of the `legato mcp serve` process, which is usually the session's, and the error names the path that was tried.
- Content that refers to a name `images` does not list is turned down, so the agent corrects the call instead of showing a broken picture.
- What the agent wrote is stored as it wrote it. The names are resolved when the item is drawn.

**Keeping the file** (`internal/service/agent_ui_images.go`). When the tool is called, Legato copies each file to `<data_dir>/agent-ui-images/<task-id>/<sha256 of the content>.<ext>`, so the file can change or go afterwards. The folder is `0700` and a copy `0600`.

- Only content that is PNG, JPEG, GIF, WebP or BMP is taken, up to 25 MiB; the file's name does not count. SVG is refused, because it is a document: an agent writes SVG inline in its HTML.
- An item has at most 60 image files.
- The same content is kept once per task, so an artifact revised twenty times with the same screenshot has one copy.
- A task keeps the 100 images it used most recently. Older copies are removed when a new one is stored. An item that still names one is read without it: a component then says the image is not stored any more, and HTML or Markdown draws a broken picture.
- A request that is turned down for any reason leaves no item and no copy behind.
- A task's folder is removed when the task has nothing shown any more (the task was deleted, or the person removed what it showed). `AgentUIService.RemoveOrphanImages` does it when a web server starts, and leaves a folder that changed in the last hour alone.

**Serving the file.** `GET /agent-ui-images/{task_id}/{file}` gives out a copy. It is the one route of this feature that asks for no token, and it is outside `/api/` for that reason:

- An image is drawn by the sandboxed frame an agent's HTML runs in, and that frame must never hold the person's token.
- What stands in for the token is the address. It is the SHA-256 of the image's content, so it can be named only by someone who already has the image.
- The route gives out only its own copies, and only content that is a raster image. It answers `404` to everything else: another task's folder, a name that is not a hash, a path that climbs out.
- The response is `nosniff`, has a sandboxing content security policy, and is cached as `private, immutable`.

The item API names each image for the web UI: `images: [{name, url}]` on `GET /api/ui-items/{id}`, where `url` is the route's path and `name` is the name in `legato-image:<name>` or, for a component, the path the agent gave.

**Showing it full size** (`ImageLightbox`). A click on an image opens it over the whole window, fitted to it; a click on the image there switches to its own size. The arrow keys and two buttons step through the item's other images, around the ends, and Escape closes it.

- In a component card or artifact, every `image` and `image_grid` entry is a button that opens it.
- In Markdown, a click on an image from `images` opens it.
- In HTML, see Images in a frame below.

Checked in a browser at 1500 px and 390 px of width with an HTML artifact, a component artifact, a Markdown artifact and a component card: every image loaded, the lightbox opened from a click and from the keyboard, and Escape closed the lightbox without closing the artifact sheet behind it. A headless Claude Code session (Sonnet 5.5) told only "show me the screenshots in this folder" called `show_components` with one `image_grid` of the six files. That is one run.

## Videos

An agent can show a narrated video it rendered with [libretto](https://github.com/cpave3/libretto): `show_video` takes the `.manifest.json` that `libretto render` writes beside the MP4. The person watches it in the side panel, and can pause it and ask the agent about that moment.

**How an agent learns to make one.** The guidance (`MCPGuidance`) names the commands an agent needs before it has looked the tool up: `libretto --help` and `libretto schema` for the format, `libretto voices` for a free local voice, `validate` (which also warns about what will not look as meant, and estimates the length), `render --no-captions`, the contact sheet and `libretto frame` for checking the result, then `show_video`. libretto's own skill file says the same at more length; an agent does not need it.

This was tried three times, each with two agents that knew nothing of libretto and were asked to explain some code in a video of 60 to 90 seconds: one had only this guidance and the `show_video` description, the other only libretto's skill file. Every agent finished a video with no error from the tool. Between rounds, what they had stumbled on was fixed in libretto and in this guidance. The agent with only the guidance took 7:46 and six renders in the first round, and 5:44 and three renders in the third. That is one run of each per round, with a capable model; a smaller model, Codex and Chimera were not tried.

**What is stored.** A video is an item of format `video`, in the panel only. Its content is the manifest, as libretto wrote it: JSON that says what is on screen and what is being said at every moment (scenes, chapters, captions, each action with its time, and the code a scene shows). Its one file is the video.

- `show_video` reads the manifest (1 MiB at most, as any content) and takes the video the manifest names in its own folder, or the path given as `video`.
- `AgentUIService` checks the manifest's `version` (1), that it has a duration and scenes, and nothing else: the web UI reads the rest. A manifest of another version is turned down with a message that says so.
- Showing a new render under the same `key` is a revision, as for any item, and the revision stepper moves between cuts.

**Keeping the file** (`internal/service/agent_ui_video.go`). The file is copied to `<data_dir>/agent-ui-videos/<task-id>/<sha256 of the content>.<ext>`, hashed while it is copied, since a video does not fit in memory as an image does. The folder is `0700` and a copy `0600`.

- Only content that is MP4 or WebM is taken, up to 1 GiB; the file's name does not count.
- A task keeps the 12 videos it showed most recently. An item whose copy is gone is read without `video`, and the player says the video is not stored any more.
- `RemoveOrphanVideos` removes a task's folder when the task has nothing shown any more, as `RemoveOrphanImages` does.

**The poster.** A video file can carry a cover picture, and libretto writes one into every video: a title card, or the first frame. A browser does not read it: a video element shows the page's `poster` or the video's first frame, never anything from the file. So Legato reads it when it stores the video (`mp4Cover` in `agent_ui_video_cover.go`: an MP4 is a tree of named boxes, and the picture is at `moov/udta/meta/ilst/covr/data`), keeps it with the task's images, and names it as the video's `poster`. The player passes it to the video element.

- The picture is read from the boxes' headers; the video itself is never read. The search stops after 4096 boxes, and any length that does not fit its file ends it.
- The poster is kept only when its content is one of the image kinds Legato shows. A video with no cover, with a cover of another kind, or in a Legato with no folder for images is stored with no poster and shown by its first frame: a poster is never a reason to turn a video down.
- It is stored beside the video in the revision's files, under the name `poster`, and given out by the image route. It is not listed in the item's `images`.

**Serving the file.** `GET /agent-ui-videos/{task_id}/{file}` gives out a copy, with byte ranges so the player can seek. It asks for no token, for a reason of its own: a `<video>` element cannot send one. As with an image, the address is the SHA-256 of the content, and the route gives out only its own copies, only content that is a video, with `nosniff` and a sandboxing content security policy.

**The player** (`AgentVideoPlayer`, `web/src/lib/agentVideo.ts`). A video is drawn by Legato's own player, not in the sandboxed frame: that frame allows no media from a server (`media-src data: blob:`), and it could not put anything in the message box with the playback time. Nothing the agent wrote runs; the manifest is read as data.

- The browser's own `<video controls>`, so full screen, speed and the keyboard work as people expect.
- Before it is played the video shows its poster, when it carries one.
- Captions are a text track built from the manifest's cues and shown by default. A manifest with `captionsBurned` gets none: the captions are in the picture. An agent should render with `--no-captions` for Legato.
- The chapter list seeks on a click and marks the chapter that is playing.
- A line under the video says what is on screen now: the file and the highlighted lines of a code scene.

**Asking about a moment.** **Ask about this moment** pauses the video and puts a description of that moment in the agent's message box (`proposeComposerText`, as a card's `actions` button does): the time, the chapter and scene, the sentence being said, and what is on screen. For a code scene that is the file, the lines shown and the highlighted lines with their code, taken from the manifest's own copy, so it holds after the files change. The person writes the question after it and sends it, or does not. The answer arrives in the conversation as any reply does.

`web/src/lib/agentVideo.ts` reads a manifest the way libretto's own `libretto at` does (`momentAt` and `describeMoment` there mirror `Manifest.At` and `Moment.Markdown` in libretto's `internal/service/manifest.go`). A change to one belongs in the other.

Checked in a browser against a scratch Legato, with a 2:46 video shown through `legato mcp serve`: the video loaded and played, 49 captions were on a showing track, a click on a chapter seeked to it, and **Ask about this moment** at 1:16 put the scene's file and its two highlighted lines in the terminal session's message box and moved the focus there. No agent was asked a question in that check; what an agent does with one is not tested.

## Studios

A person can say a video's words themselves. `show_studio` opens a studio on a storyboard in the side panel: the person records each scene with their microphone, and their camera if they like, beside the conversation in which they and the agent agreed what the scenes are. The studio is [libretto](https://github.com/cpave3/libretto)'s: libretto keeps the takes, transcribes them and draws the scenes. Legato adds the conversation.

**A tool, not an artifact.** An artifact is something the agent made for the person to read. A studio is a place the person works in. It shares the side column and nothing else: it is not among the artifact tabs and is not counted with them, it has its own button in the session's toolbar and its own heading, and it has no revisions to step through, no source to copy, no download and no remove. `AgentArtifactPanel` draws the column either way; an item of format `studio` puts it in that mode.

**The flow the guidance asks for.**

1. **Plan, with the studio open.** The person says what they want to show. The agent writes a first rough storyboard at once and calls `show_studio`, and the two of them shape the plan while the person looks at it. When the person says to add a part, drop one, or go deeper on something, the agent changes the storyboard file there and then, and the panel shows it within a couple of seconds. The agent calls `show_studio` again with `scene` to turn the panel to the part under discussion. **Ask about this scene** puts a scene in the message box, for the person to say what they think of it.
2. **Record.** The person records each scene in the panel, in their own words.
3. **Line up.** **Make the video from my takes** puts a request in the message box. The agent reads each take with `libretto transcribe` and edits the storyboard to follow what was said, then renders it and calls `show_video`.

**Talking points, not a script.** A scene's `notes` are two to four short lines, each starting with `- `: the details to get across. The panel draws them as a list, large, under the scene's picture. The guidance and the tool's description tell the agent never to write the person sentences to read, to give such a scene no narration, and to plan each picture in full with no actions that wait for a marker: the markers come after the take, from what was said.

**Leaving the plan is direction.** A person who records will say things that were not planned, skip things that were, and go into something they thought of while talking. Nothing in Legato or libretto treats that as an error. The panel marks such a scene "your own way" and says the agent will follow; the request the button writes says the same, and names the scenes; the guidance tells the agent to add a picture for what the person went into (a terminal for commands they named, a diagram, the code), to drop what they skipped, and not to ask for another take because they left the script.

**What is stored.** A studio is an item of format `studio`, in the panel only. Its content is `{"storyboard": "<absolute path>"}` and nothing else. Legato keeps no copy of the storyboard or the takes: they are files in the person's own folder, which the agent goes on editing.

**The libretto it needs** (`internal/engine/libretto`). Legato runs without libretto. Before it relies on it, it asks `libretto version --json` for the studio's level, and wants `libretto.StudioLevel`.

- **No libretto, or one too old.** `Runner.Probe` returns `ErrNotInstalled` or a `*TooOldError` (a libretto from before the studio has no `version` command at all, and is told from a missing one). `Explain` turns either into what is wrong and what to do.
- **The agent is told first.** `show_studio` probes before it stores anything, in the agent's own process, and fails with that explanation and "Tell the person this": a panel that can only say libretto is missing is no use to the person. No item is left behind.
- **The panel is told too.** The server probes again each time it starts a studio, since its `PATH` is not the agent's, and since libretto may be installed while Legato runs. The route answers `503` with `{"error", "code"}`, where `code` is `libretto_missing` or `libretto_old`, and the panel says the studio cannot open on this machine, what to do, and that nothing is lost. It asks again every few seconds, so the studio opens once libretto is put right.
- **A libretto newer than this Legato.** The studio's answers carry their level (`api`). A page that reads a level above its own (`STUDIO_API` in `web/src/lib/agentStudio.ts`) says Legato needs updating and shows nothing it might misread.

**Running a studio** (`service.AgentStudioService`). A studio is a `libretto studio <storyboard> --port 0 --until-stdin-closes` process, started the first time its panel asks for it and kept until Legato stops. One storyboard has one studio, however many items name it; one that died is started again when next wanted. Legato holds the studio's input open, so a studio never outlives a Legato that ends without stopping it.

**Reaching it** (`internal/server/agent_ui_studio.go`). The studio listens on the server's own machine alone, and the page may be on another. `ANY /api/ui-items/{id}/studio/{path}` passes a request on to the studio of that item: under the studio's own address, with the header libretto's own page sends (`X-Libretto-Studio`), and without Legato's token. It is under `/api/`, so it asks for the token, which an `<img>` or `<audio>` element cannot send: the panel fetches a still or a take and shows it from an address of the page's own (`fetchStudioFile`).

**The panel** (`AgentStudioPanel`, `web/src/lib/agentStudio.ts`). Legato's own panel, drawn outside the sandboxed frame, which allows no microphone.

- One scene is in front of the person at a time: its picture, its talking points and the record button. Under it is the plan, laid out by chapter, with what is recorded. Arrows step between scenes.
- It reads the storyboard from the studio every two seconds, except while a take is being recorded or kept. What the agent changed since the person last looked is said in a line ("The plan changed: added sync; removed intro; changed steps.") and marked on each scene, `new` or `changed`, until the person goes to it (`planChanges` in `agentStudio.ts`). A scene's recording is not a change of plan.
- An item shown again with a `scene` turns the panel to that scene, unless a take is being recorded. The panel is kept through an item's revisions, where an artifact is drawn afresh, so the microphone and the person's place are not lost.
- A scene's picture shows everything the scene will show. The person's own camera is drawn over it where their face will be in the video. The picture before stays, dimmed, while a changed one is drawn.
- The microphone and the camera are set once: they are behind a settings button, the choice is remembered in the browser, and nothing about them is on the panel otherwise. **Test** opens them without recording and names the ones in use. A camera that cannot be opened (a screen recorder has it) leaves a take of sound alone, with a note; only a microphone that cannot be opened stops a take.
- **Two views.** **Plan** is where the person and the agent agree what the video covers; **Record** is where it is said. The panel opens on the plan while any scene is only planned or nothing is recorded, and does not change views by itself after that. The plan shows the brief (`writeBrief`: the agent is told, and drafts scenes from it), then the scenes by chapter with a length for each, a guess from its talking points until it is said (`lengthOf`). The scene in front of the person opens in place: its picture, its `shows` line and its talking points to rewrite (`rewriteScene`, saved when the field is left), up and down (`moveScene`), comments, and removal. A chapter's title is renamed where it stands, and a line at the foot adds a scene after the current one (`addScene`). A scene with a `shows` line and no picture is planned: libretto draws a placeholder. **Agree the plan** (`agreePlan`) tells the agent to draw the pictures; with none left to draw the button is **Start recording**.
- **The loop.** When a take is kept, the panel's request carries `X-Legato-Studio-Notify`, and the studio route (`tellAgent` in `internal/server/agent_ui_studio.go`) queues a message for the item's task through `service.AgentMessageQueuer`: at once when the agent is idle, between steps when it is working. `service.StudioNote` words it: review this take with `libretto transcribe` and `libretto review`, bring the scene in line with what was said, and say nothing while the person presents. A comment, a scene removed and a decision to add a scene are told the same way. The person turns this off in the panel's settings (`legato:studio-notify`); the header is then left out, and "Ask for a review" and "Make the video from my takes" put the same requests in the message box.
- **The review.** libretto's project carries each scene's words with their times (`transcript`) and what the agent made of its take (`review`). `transcriptRuns` in `agentStudio.ts` joins the words into runs: one that covers a talking point is underlined, one off the plan is marked, and each sentence shows when it was said. A stretch off the plan is a card with the agent's note and three answers (`decideOffPlan`): **Add scene** asks the agent in a comment, **Keep in this scene** dismisses it, **Cut it** has libretto skip the stretch. A take with no review yet says the agent is looking at it.
- **The layout** follows the Studio board of the Legato design canvas, in the new look (`lw-` colours and the Geist typefaces, set in `web/src/index.css`; the studio is the first surface to use them). From the top: a timeline with a block for each scene, as wide as the scene is long; the scene, with its picture and a bar that plays the part of the take the scene uses; the takes, as chips, and the record control; what the agent asks; the transcript and the talking points; comments for the agent; the outline; and a footer that renders the video through libretto (`startRender`) and plays the result in place of the picture. Above the footer, once there is a video, a strip gives the file's whole path (`render.file`) with a button that copies it: the panel plays the video, and this is the only place that says where the file is. A libretto that does not report the path gets no strip.
- The panel holds a microphone only while it records: it is opened for each take and let go after it, and **Test** lets it go again after twenty seconds. A browser gives every page the same feed from a microphone, and a feed open for hours runs late or comes in bursts. A whole afternoon of takes in this panel lost their last seconds, or held almost no sound, because libretto's own studio page was open in another tab with the microphone it had been given that morning; closing that tab ended it. Nothing in the panel can see another page, so when a take's sound is missing, the first thing to ask is what else has the microphone. A take runs on for a third of a second after **Stop**, for the last word.
- The sound of a take is taken from the microphone as samples (`web/src/lib/takeSound.ts`, `samplesOf`): a small program on the browser's audio thread counts every sample from start to stop, and answers "done" only when the last is handed over, so a busy page makes a take slow to finish and nothing else. They are sent as a WAV file. The browser's own recorder was used before; fed a microphone that came and went, it wrote a file that was whole and simply ended early, which is why the fault above took so long to find. Samples of a microphone that stopped are exact silence, and take up their time. The recorder is still what records the camera, with no sound, and both are sent (`uploadTake`, as a form with `take` and `sound`); libretto joins them. A camera recording too small to be one is not sent, and the take is kept as sound alone. In a browser that cannot give samples the recorder takes the sound as before, and the panel says so after the take.
- The samples say what the microphone did, and the panel passes it on. While a take is recorded, a red bar over the scene's picture says that the microphone has given exactly nothing for three seconds or more (a room is never that quiet, so it has stopped). After it, the panel says how much of the take was such silence and where the longest stretch was, or that less sound came than the take lasted. A take of nothing but such samples is not kept.
- Under a scene the person can write the agent a comment (`commentOnScene`), take one back, and remove the scene after being asked once (`removeScene`). libretto writes both into the storyboard file as `comments`, on the scene or on the storyboard, which is where the agent reads them; `libretto validate` lists them as warnings, and the agent deletes each when it is done. A change the person made themselves is shown and is not announced as a change to the plan. The outline counts a scene's comments.
- A take starts after a count from three, drawn over the scene's picture (`studioPace.countdownMs`). The panel listens while it counts: a microphone that gives exactly nothing for most of the count is one another page or program has, and the take is not started. **Cancel** calls the count off.
- **Cancel**, beside **Stop** while a take is recorded, throws the take away: it is not sent to the studio, so nothing is kept or transcribed, and the scene is as it was.
- A take under a second is not kept. libretto refuses a recording it cannot read, so a broken one never reaches the storyboard. A take in which nothing is heard is kept by libretto but not given to the scene, and the panel says why.
- A scene is drawn though another scene of the storyboard has a problem: the agent is often half way through an edit.
- A recorded scene shows what was heard. Its takes are folded away under it, played on request, and an earlier one can be chosen again.

Not checked in a browser: the panel is covered by component tests with a pretend microphone, recorder and studio, and libretto's own page, which records the same way, was driven in a real browser there. The first run against a real Legato and a real microphone is still to do.

## Web UI

**Cards in the conversation.** `legatoUICall` (`web/src/lib/agentUi.ts`) recognises a display tool call under each agent tool's naming and reads the item marker from its result. `NativeAgentPanel` draws such a call as `AgentUICallCard` in place of a tool row, and it stands alone as a `spawn_agent` call does: a placeholder while the call runs, then the card at the revision that call made, or a line that opens an artifact. A call that failed keeps its ordinary tool row, where the error can be read. The link from call to item is in the transcript, so cards are drawn again after a page reload, a snapshot repair and a resumed session.

**Artifact panel** (`AgentArtifactPanel`). It lists what the session showed as tabs, artifacts first, with a revision stepper, Copy source, Download and Remove.

- **Download** saves an HTML item as a whole page that looks the same outside Legato (`buildStandaloneDocument`). Its image files go into the page as `data:` addresses (`inlineImages`), so it opens where Legato is not; an image that cannot be read keeps its address at Legato. A saved page has no larger view of an image. The agent wrote its styles against Legato's theme, which the frame supplies and a raw file would be without, so the theme variables and base styles go into the file. So does the content security policy: outside Legato nothing else limits what the agent's scripts may reach. `legato.proposeMessage` does nothing there and `legato.openLink` opens the link, so scripts that call them do not fail. Markdown and component cards are saved as the agent wrote them (`.md`, `.json`). **Copy source** always gives what the agent wrote.
- On a wide screen it is a resizable column beside the conversation (drag its left edge, or Left/Right on it; the width is remembered).
- Below 1100 px of viewport (1500 px beside the swarm event log) it is a full-screen sheet, as on a phone, above the bottom navigation.
- `useArtifactPanel` decides when it opens: a new artifact from the agent in view opens it, focused on that artifact; one from another agent is counted on that agent's sidebar entry and opens when the person goes there. A card never opens it. Where the panel is a sheet, a new artifact only marks the button, so it cannot land on top of what the person is typing.
- The **Artifacts** button is in the session's own toolbar (the native panel's status bar, the terminal `PromptBar`), with a count and a mark for new ones. A native session counts its artifacts; a terminal session counts its cards too.

**Terminal sessions.** A session shown through its terminal has no conversation feed. Its cards are in the panel beside its artifacts.

**The sandbox** (`SandboxedHtml`, `buildSandboxDocument`). HTML an agent wrote is never trusted: an agent can read hostile text.

- The frame is `sandbox="allow-scripts"` and nothing else. Without `allow-same-origin` its content has an origin of its own: it cannot read Legato's storage (where the auth token is) or call Legato's API as the person, and it cannot open or navigate windows or submit forms.
- A content security policy, placed ahead of everything the agent wrote, allows no network (`default-src 'none'`, `connect-src 'none'`), no frames, no form targets and no `<base>`. Scripts, styles, fonts and images may load from the hosts in `agents.mcp.resource_domains`, and from nowhere else.
- The server never serves item content as a page. It leaves `GET /api/ui-items/{id}` inside JSON, with `nosniff`.
- Every frame carries a **Made by the agent** label that the content cannot remove.

Checked in a browser: script in a frame was refused the parent's storage and document, `fetch` to Legato and to another site, `window.open`, navigating the top window, and a script from an unlisted host.

What an agent's page can and cannot do there. Each entry was checked in a browser, except `sessionStorage`, cookies, submitting forms and `eval`, which follow from the same sandbox attribute and policy:

| Works | Does not work |
| --- | --- |
| The page's own `<style>` and `<script>`, inline SVG, canvas | `fetch`, XHR, WebSocket (`connect-src 'none'`) |
| Scripts for interaction: tabs, sections that open, details on click | `localStorage`, `sessionStorage`, cookies (they throw: the origin is opaque) |
| Libraries from the allowed hosts as a classic script or an ES module: Mermaid and D3 were run | Web workers (`child-src 'none'`), which a few libraries need |
| A `<pre class="mermaid">` block, drawn by the frame's own script (see Diagrams in a frame) | Forms that submit, pop-ups, navigating the top window, `eval` |
| A `mermaid` fence in a Markdown artifact, drawn by Legato's own bundled Mermaid | |
| An image file listed in `images`, in an `<img>`, a CSS `url()` or from a script | Any other address at Legato: the API, a page, an image that was not listed |

A frame starts with a small script that gives the content `legato.proposeMessage(text)` and `legato.openLink(url)`, reports its height, and passes link clicks to the host (`legato.diagrams` and `legato.selectNode`, and `legato.image`, are described below). The messages are JSON-RPC with MCP Apps' method names (`ui/notifications/size-changed`, `ui/open-link`, `ui/message`), and the theme is given as MCP Apps' CSS variables (`--color-background-primary`, `--color-text-primary`, `--font-sans`, …), so the frame is a start on drawing other MCP servers' UI. The host acts only on messages from the frame's own window, and opens only `http` and `https` links, after the person confirms.

**Diagrams in a frame.** The frame's script also draws Mermaid, so an agent writes a diagram's source and no library code (`diagramsScript` in `web/src/lib/agentUi.ts`):

```html
<pre class="mermaid">flowchart TB; api[API]:::blue --> db[(Database)]:::cyan</pre>
<p id="about">Select a part.</p>
<script>
  const about = { api: "Checks the token and routes the call.", db: "One container for each domain." }
  legato.diagrams({ onNode: (id) => { document.getElementById("about").textContent = about[id] } })
</script>
```

- `legato.diagrams(options)` draws every `.mermaid` block on the page and returns a promise. A page that only wrote the blocks gets them drawn when it has loaded. A page that loads a Mermaid of its own is left alone.
- `onNode(id, element)` makes every node a button: reachable with the keyboard, marked `aria-pressed="true"` when chosen, and scrolled into view. `id` is the node's name in the diagram's source. `legato.selectNode(id)` chooses a node from the page's own script.
- A node takes a color by name: `:::blue`, `:::cyan`, `:::green`, `:::amber`, `:::red`, `:::pink`, `:::purple`, `:::gray`. `classDef` still works for a style of the agent's own.
- The diagram is drawn in Legato's theme (`mermaidConfig`, `diagramStyle`), with Mermaid's `securityLevel: "loose"`: the frame is the sandbox, so a label may hold markup and a diagram's own `click` lines work.
- Mermaid is loaded from the first allowed host that serves it (`MERMAID_SOURCES`: jsDelivr, then unpkg), at the release the web UI bundles. With neither allowed, a block stays as its text and the page's script carries on.
- A diagram Mermaid cannot read stays as its source, with the error in its `title`; the others are drawn.

Each of these is something an agent got wrong, or would, when it ran Mermaid itself. The first two rows were measured in the frame with Mermaid 11.16; the others follow from how Mermaid sizes and replaces a block:

| Without the helper | What the helper does |
| --- | --- |
| A diagram drawn inside a hidden tab is 16 px wide: Mermaid measures text in the block it draws into. | Draws outside the block, then puts the result in. |
| A node's group is named `mermaid-<n>-flowchart-<name>-<i>` and has no `data-id`, so a click handler written from memory finds nothing. | Reads the name out for flowcharts, state, class and entity diagrams, and hands it to `onNode`. |
| A wide diagram shrinks with the panel until its text cannot be read. | Stops at 70% (14 px text at 10 px); the block scrolls sideways from there. |
| Mermaid's own dark theme. | Legato's colors and font. |
| The diagram's source shows until the library has loaded. | The source is hidden while the diagram is on its way, and shown again if it will never come. |

The saved file (`buildStandaloneDocument`) carries the same script, so a downloaded page draws its diagrams when it is opened with a network connection.

**Images in a frame** (`imagesScript`, `imageSources` in `web/src/lib/agentUi.ts`). The frame cannot read the disk or reach Legato's API, so the page that builds its document does three things with the item's images:

- It writes each image's address where the HTML says `legato-image:<name>`. The address is at the server the page talks to, which the agent cannot know when it writes the HTML.
- It adds one source to the frame's `img-src`: `<that server>/agent-ui-images/`. The folder of stored images, and not the API beside it. A frame with no images gets nothing added.
- It gives scripts `legato.image(name)`, which returns the address (empty for a name the item does not have), for an image a script adds.

The frame is as narrow as the panel, so the larger view is the host's to draw. A click on one of the item's images, or Enter or Space on it, posts `legato/show-image` with the image and the page's other images in document order; `SandboxedHtml` opens the lightbox with those that are the item's own addresses and drops any other, so the frame cannot make the page load an address of its choosing.

- Every image of the item opens, including one a script adds later, and is marked `data-legato-zoom="on"` (a zoom cursor, and a place in the tab order).
- An image does not open when it or something around it has `data-legato-zoom="off"`, or when it is inside a link or a button of the page's own.
- A `<figcaption>` in the image's `<figure>` is its caption in the larger view.
- `class="legato-image-grid"` on a container lays its images, or its figures, out as a grid of thumbnails.

Checked in a browser, from inside a frame that had six images: `fetch` to Legato's API, the API's own image route as an `<img>`, another file at Legato's address and an image at another host were each refused by the policy, and the six images loaded.

**Proposing a message.** A card or an artifact can put text in the person's message box: an `actions` component's button, or `legato.proposeMessage(text)` from HTML. The text is added to what is there, a notice says where it came from, and nothing is sent until the person sends it. It travels as a window event (`proposeComposerText` / `onComposerProposal`), so the native composer and the terminal `PromptBar` both take it without knowing of the panel.

## In the TUI

The TUI cannot draw a card or an artifact, so it counts them and links to the web UI. In the agents view, the selected agent gets a line under the header when it has shown something:

```
Shown in web UI: 2 cards · 1 artifact · latest: Sync design  o open
http://localhost:3080/agents?task=fekz3ove&artifact=ui-885e1b7a26
```

- `o` opens that address in the browser (`clipboard.OpenURL`). It answers for every agent: for one that has shown nothing, or with the web UI off, the status bar says why nothing opened. When no browser can be started (a remote terminal), the status bar gives the address to open elsewhere.
- The address is never cut short, since a partial one can be neither opened nor copied. It shares the first line when there is room, takes a line of its own when there is not, and is left out when the panel is narrower than the address; `o` still works.
- When an agent shows something, the status bar says so (`fekz3ove showed an artifact in the web UI`), whichever agent is selected, and the line updates. Both come from `EventAgentUIChanged`; the counts are also reloaded whenever the agent list is.
- The link is `service.AgentUIWebLink(base, taskID, itemID)`: `/agents?task=<id>&artifact=<item-id>`, naming the item shown last. The web UI selects the agent, opens the artifact panel on that item (a card too, and on a phone, where a new artifact does not open by itself), and drops both parameters from the address.
- The base address is `localWebURL(cfg)`: `http(s)://localhost:<web.port>`, or `web.tls.hostname` in place of `localhost` when TLS is on and a host name is configured. It is empty unless `web.enabled`: the line then reads `web UI is off`. A web UI started separately with `legato serve` while `web.enabled` is false is not linked to.

`internal/tui/agents/shown.go` draws the line; `internal/tui/app_agent_ui.go` loads the summaries (`AgentUIService.Shown`), announces new items and opens the link. The key is `ActionAgentsOpenShown` in the hotkey registry, behind `CapabilityAgentUI`.

## HTTP API and events

- `GET /api/agents/{task_id}/ui-items` — a session's items at their latest revision, without content.
- `GET /api/ui-items/{id}[?revision=N]` — one item with its content and, for HTML, `resource_domains`.
- `DELETE /api/ui-items/{id}` — removes an item and its revisions.
- `GET /agent-ui-images/{task_id}/{file}` — Legato's copy of an image file an agent showed. No token; see Image files.
- `GET /agent-ui-videos/{task_id}/{file}` — Legato's copy of a video an agent showed, with byte ranges. No token; see Videos.
- `ANY /api/ui-items/{id}/studio/{path}` — passed on to the libretto studio of a studio item, which is started if it is not running. `503` with a `code` when libretto is missing or too old; see Studios.

The first three and the studio route need the bearer token. Agents write through the MCP server, never through this API.

`agent_ui_changed` carries `task_id`, `item_id`, `placement`, `revision` and `kind` (`shown`, `deleted`). The MCP server broadcasts it over IPC; both `handleIPCMessage` (TUI) and `handleServeIPCMessage` (`legato serve`, which acts on a shorter list of messages) publish `EventAgentUIChanged`; `Server.StartAgentUIEvents` forwards it to WebSocket clients, where `QuerySyncProvider` invalidates the session's list and the item.

## Configuration

```yaml
agents:
  mcp:
    enabled: true            # default; false gives no session the server or the guidance
    resource_domains:        # hosts an agent's HTML may load scripts, styles, fonts and images from
      - cdnjs.cloudflare.com
      - cdn.jsdelivr.net
      - unpkg.com
      - fonts.googleapis.com
      - fonts.gstatic.com
```

The list above is the default. `resource_domains: []` allows no network at all, so everything must be inline. Entries must be plain host names: a scheme, a path, a port, a wildcard or a policy keyword fails config loading, because the list becomes part of a content security policy. Each allowed host sees the viewer's IP address when content loads from it. The list is read by the web server and by each `legato mcp serve`, which tells the agent which hosts it may use.

A session must be started again to gain or lose the server: agent tools read their MCP servers at start.

## Packages

- `internal/engine/mcpserver/` — a stdio MCP server from a list of tools, over the official Go SDK. It knows the protocol and nothing about Legato. `NewTool[In]` infers the input schema; `Serve` runs until the client goes away.
- `internal/cli/mcp.go` — Legato's tools (`MCPServerOptions`), their names (`MCPToolNames`) and the guidance (`MCPGuidance`).
- `cmd/legato/mcp_cmd.go` — the `legato mcp serve` command.
- `internal/service/agent_ui.go`, `agent_ui_components.go` — `AgentUIService` and the component catalog. `agent_ui_images.go` — the copies of image files: keeping, pruning, opening one to serve. `agent_ui_video.go` — the same for a video item's file, and the check of its manifest. `task_note.go` — `AddTaskNote`, shared with the workflow `task.note` step.
- `internal/engine/libretto/` — asks the installed libretto what it can do (`Probe`), says what is wrong when it cannot serve a studio (`Explain`), and starts one (`StartStudio`).
- `internal/service/agent_ui_studio.go` — the `studio` format, and `AgentStudioService`, which runs the studios.
- `internal/engine/store/agent_ui.go` — persistence.
- `internal/server/agent_ui.go` — the HTTP API, the image and video routes and the WebSocket forwarder. `agent_ui_studio.go` — the route to a studio.
- `internal/engine/hooks/` — `claudemod/tools/` and `InstallClaudeToolsMod`; `SetMCPTools` and `SetToolsPrompt` on the Codex and Chimera adapters; `mcp_tools.go` for what they share.
- `internal/tui/agents/shown.go`, `internal/tui/app_agent_ui.go` — the TUI's line and link for what an agent showed.
- `web/src/lib/agentUi.ts`, `hooks/useAgentUI.ts`, `hooks/useArtifactPanel.ts`, `components/AgentArtifactPanel.tsx`, `AgentUICallCard.tsx`, `AgentUIItemView.tsx`, `AgentUIComponents.tsx`, `SandboxedHtml.tsx`, `ImageLightbox.tsx`, `ArtifactsButton.tsx`. `lib/agentStudio.ts`, `components/AgentStudioPanel.tsx` — the studio panel and what it asks the studio. `lib/agentVideo.ts`, `components/AgentVideoPlayer.tsx` — a video's manifest and its player.

## Debugging

- `legato mcp serve --task <id>` can be driven by any MCP client over stdio; `claude mcp list` and `codex mcp list` (with the same `-c` overrides) show whether an agent tool connects to it.
- `sqlite3 <db> "SELECT i.id, i.task_id, i.placement, r.format, r.title FROM agent_ui_items i JOIN agent_ui_item_revisions r ON r.item_id = i.id AND r.revision = i.revision"` lists what was shown.
- `claude --debug` logs the plugin's hook decision: `Hook approved tool use for mcp__plugin_legato-tools_legato__…, bypassing permission prompt`.
- A scratch Legato for trying it without touching real state is set up as in [Claude Code native integration](claude-code-native.md) § Trying it without touching real state. Its agents still run the person's globally installed activity hooks, which call the *installed* `legato`; against a scratch database migrated by a newer build those calls fail with a schema error, which is noise from the scratch setup only.

## Known limits

- Only the web UI draws artifacts and cards. The TUI's agents view says what the selected agent has shown and links to it (see In the TUI); in an attached terminal the person sees the tool call and the one-sentence reply. `agents.mcp.enabled: false` turns the tools off.
- A view can propose a message. It cannot send one, call a tool, or read anything from Legato.
- A question about a video is a proposed message: it is not kept with the video, so the video shows no thread of what was asked at which time, and a question for an agent whose session has ended goes nowhere. Review tours have both (`ArtifactInteractionService`); a video does not yet.
- `show_video` needs the agent to have rendered a video already, with libretto on its `PATH`. Legato does not render one itself.
- A studio needs libretto, with a studio of the level Legato is written for, on the `PATH` of the Legato server as well as the agent's. Without it `show_studio` fails and says so.
- The studio records a voice and a camera. It does not record the screen.
- Lining a video up with what was said happens when the person asks, not after each take.
- A studio is not stopped when its panel is closed or its item removed, only when Legato stops.
- The studio panel has not been run in a browser against a real Legato yet.
- The panel is narrow, and code in a 1920 by 1080 video is small in it. The player's full-screen button is the way to read it.
- **Download** on a video saves its manifest, not the MP4.
- An image in a Markdown artifact opens on a click only; the keyboard does not reach it. In HTML and in components it does.
- A page saved with **Download** holds each image twice when a script may ask for it (in the HTML and in what `legato.image` answers), so a page with large images is a large file.
- Image files are not shown again from their original path: Legato's copy is what every revision draws. A task's 101st image removes its oldest.
- Legato draws the UI of its own server only. It is not a general MCP Apps host for other servers' `ui://` resources.
- Chimera subagents (`spawn_agent`) read the global Chimera config and do not get the server.
- A sandboxed frame can navigate *itself* to another page. That page gets nothing from Legato and keeps the **Made by the agent** label, but it is no longer the agent's content.
- For Codex, the guidance is the session's `developer_instructions`, which replaces a `developer_instructions` of the person's own in `~/.codex/config.toml` for Legato-launched sessions (as a role prompt or learned context already did).
- A small model may ignore the guidance and answer in text.
- An agent's items are removed with its task (for a swarm worker, with its sub-task). Until then they stay in the database, within the per-task limits; nothing removes them when the session ends.
