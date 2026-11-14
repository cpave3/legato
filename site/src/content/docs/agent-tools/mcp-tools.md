---
title: Legato's tools for agents
description: The MCP tools Legato gives every agent it starts, what each one does, and where you see what agents show with them.
sidebar:
  order: 50
---

Every agent that Legato starts gets a set of Legato's own tools, through an MCP server that Legato adds to the session: `legato mcp serve`. With these tools an agent can show you something better than a wall of text, read the task it works on, leave a note on it, and talk to the other agents in its worktree.

The tools work the same for [Claude Code](/legato/agent-tools/claude-code/), [Codex](/legato/agent-tools/codex/) and [Chimera](/legato/agent-tools/chimera/). You install nothing, and Legato writes nothing to the agent tool's own configuration.

## Turn them on or off

The tools are on by default:

```yaml
agents:
  mcp:
    enabled: true
```

With `enabled: false`, no session gets the server or the guidance about it. An agent tool reads its MCP servers when it starts, so a change applies to sessions you start, or reload, afterwards.

Each agent tool runs Legato's tools without asking you first. Legato also tells each agent to use them without being asked: Claude Code and mod-capable Chimera read the server's own instructions. Codex and legacy Chimera get the same text in their launch prompt.

## The tools

| Tool | What it does |
| --- | --- |
| `show_components` | Shows a card in the conversation, built from Legato's own parts: headings, text, figures, tables, lists, badges, progress bars, bar charts, code, images and buttons. |
| `show_card` | Shows a card of the agent's own HTML in the conversation. |
| `show_artifact` | Opens a page in the side panel beside the conversation: HTML, Markdown, or Legato's parts. |
| `show_video` | Opens a narrated video the agent made with libretto in the side panel. |
| `show_studio` | Opens a libretto studio in the side panel, where you record a video in your own voice. |
| `session_info` | Tells the agent its task ID and title, its agent tool, its swarm role and its folder. |
| `task_context` | Gives the agent its task as Markdown: the description, the linked ticket, repository details and notes. |
| `add_task_note` | Records a note on the task. |
| `list_peers` | Lists the other agents in the same Git worktree. |
| `message_peer` | Sends a message to one of those agents. |
| `broadcast_peers` | Sends a message to all of them, and says which ones it reached. |
| `message_reeve` | Sends a message to this Legato's [Reeve](/legato/using/reeve/): a report it asked for, or a question. Every agent but the Reeve has it. |
| `request_access` | Asks you for a credential that Legato holds. See [Credential broker](/legato/security/credential-broker/). |
| `reply_case` | Answers a case from Argus. See [Argus](/legato/integrations/argus/). |

Some tools are offered only when they can work:

- The three peer tools need tmux.
- `request_access` needs credentials in the configuration.
- `reply_case` needs `integrations.argus`.

A swarm worker's `task_context` and `add_task_note` act on the swarm's parent task.

Outside a Legato session, `legato mcp serve` offers no tools. Each call also checks that the task still has a running agent.

## What agents show you

An agent shows you something in one of two places:

- **A card** takes the place of a reply in the conversation: a comparison, a status, a small chart.
- **An artifact** opens in the side panel beside the conversation: a design, an explanation with a diagram, a report. The panel lists every artifact of the session as a tab.

Only the web UI draws cards and artifacts. See [Web UI](/legato/using/web-ui/).

### In the web UI

- On a wide screen the side panel is a column beside the conversation. Drag its left edge to resize it. On a narrow screen it is a sheet over the page.
- A new artifact from the agent you are looking at opens the panel. One from another agent is counted on that agent's entry in the sidebar.
- When an agent shows something again under the same name, the panel keeps the earlier versions. Step between them, copy the source, download the page, or remove it.
- Click an image to see it full size. The arrow keys step through the other images.
- A button in a card can put text in your message box. Nothing is sent until you send it.

### In the TUI

The TUI cannot draw cards or artifacts. In the agents view, the selected agent gets a line that counts what it has shown, with a link to it in the web UI:

```
Shown in web UI: 2 cards · 1 artifact · latest: Sync design  o open
http://localhost:3080/agents?task=fekz3ove&artifact=ui-885e1b7a26
```

Press `o` to open the link in your browser. If no browser can start, the status bar gives the address. The link needs `web.enabled`; without it the line reads `web UI is off`.

### What is safe

An agent's HTML runs in a sandboxed frame in the web UI:

- It cannot read Legato's storage, call Legato's API as you, open windows, or submit forms.
- It cannot reach the network. It can load scripts, styles, fonts and images only from the hosts in `agents.mcp.resource_domains`.
- Every frame has a **Made by the agent** label that the content cannot remove.
- A link in the frame opens only after you confirm it.

The default hosts are the common script and font CDNs:

```yaml
agents:
  mcp:
    resource_domains:
      - cdnjs.cloudflare.com
      - cdn.jsdelivr.net
      - unpkg.com
      - fonts.googleapis.com
      - fonts.gstatic.com
```

Each entry is a plain host name. A scheme, a path, a port or a wildcard fails config loading. `resource_domains: []` allows no host, so an agent's HTML must hold everything itself. Each host you allow sees your IP address when content loads from it.

Mermaid diagrams in a frame are drawn from the first allowed host that serves Mermaid: jsDelivr, then unpkg. With neither allowed, a diagram stays as its source text.

### Images

An agent can show image files from disk, such as a screenshot it took. Legato keeps its own copy of each file, so the picture stays when the file changes or goes.

- PNG, JPEG, GIF, WebP and BMP, up to 25 MiB each. SVG is refused; an agent writes SVG into its HTML.
- At most 60 images in one card or artifact.
- A task keeps the 100 images it used most recently.

### Limits on what is kept

- A card or an artifact holds at most 1 MiB of text.
- A task keeps 200 cards and artifacts. Past that, the oldest go.
- An artifact keeps its newest 20 versions.
- Cards and artifacts stay until you remove them or delete the task. They are not removed when the session ends.

## Videos and studios

Videos and studios use [libretto](https://github.com/cpave3/libretto), a separate command that makes narrated videos. Legato runs without it. Install it only if you want these two tools.

- **Videos.** An agent renders a video with libretto, then calls `show_video`. You watch it in the side panel, with chapters and captions. **Ask about this moment** pauses the video and puts a description of that moment in your message box, for a question to the agent. Legato keeps MP4 and WebM files up to 1 GiB, and the 12 videos a task showed most recently.
- **Studios.** When you want to present something in your own voice, an agent opens a studio with `show_studio`. You and the agent plan the scenes, then you record each scene with your microphone, and your camera if you like. The agent lines the video up with what you said and renders it.

A studio needs libretto on the machine that runs Legato's web server, as well as on the agent's. When it is missing or too old, `show_studio` fails and the agent tells you what to install.

## Limits

- Only the web UI draws cards and artifacts.
- A card or an artifact can propose a message. It cannot send one, call a tool, or read anything from Legato.
- A small model may ignore the guidance and answer in text.
- Legacy Chimera subagents do not get launch-only MCP tools. Chimera mods v1 carries the bundled server into subagents too; they never take over the parent's Legato connection.

For how the server works inside, see [Agent tools on GitHub](https://github.com/cpave3/legato/blob/main/docs/ai/agent-mcp.md).
