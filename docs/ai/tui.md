# TUI

How the terminal UI looks and how its screens are drawn. Keys are in the hotkey registry (`internal/tui/hotkey/defaults.go`); behaviour that is not about looks is in [development notes](dev-notes.md).

## Theme

`internal/tui/theme/theme.go` holds the palette. Views use its names and no color literals of their own.

| Name | Color | Used for |
| --- | --- | --- |
| `Background` | `#1f1d27` | the page |
| `Surface` | `#1b1a22` | a panel beside the page: the agent list, the task panel, the activity panel |
| `Card` / `CardActive` | `#24222d` / `#2a2735` | a card or a quiet tile header / the selected card or row |
| `Bar` | `#26232f` | the key bar and chips |
| `Border` | `#3a3648` | rules and panel edges |
| `TextPrimary` / `TextSecondary` / `TextFaint` | `#dcd9e6` / `#7d7896` / `#57536a` | what is read first / the rest / separators and placeholders |
| `Accent` | `#b79cf7` | the brand, selection, the column in progress |
| `Green`, `Amber`, `Blue`, `Red`, `Yellow`, `Cyan` | | running, needs the person, handed over, failed, in progress, waiting on background work |

`StateColor(activity)` and `StateTint(activity)` give the color of an agent state and the background of a strip that reports it. `TextTertiary` is the old name of the quiet text color and equals `TextSecondary`, so views written before the palette changed stay readable.

### Backgrounds

The app paints its own background, so it looks the same whatever the terminal's colors are. `App.View` passes every frame through `theme.Canvas`, which pads it to the terminal's size and puts `Background` behind all of it.

A styled span ends by resetting every attribute, the background included. `theme.Fill(block, width, bg)` puts the background back after each reset, pads or cuts every line to `width`, and so gives a block a solid background without any span inside it carrying one:

- Render the spans of a panel or a card with foreground colors only, then `Fill` the block. Do not set `Background` on the spans.
- A span that does set a background keeps it (a chip).
- A block filled earlier keeps its background inside a larger block filled afterwards, so a card (`Card`) sits in a column (`Background`) that sits in the frame.
- With no colors (tests run this way) `Fill` only pads.

Captured terminal output is filled the same way: `Fill` also resumes after a sequence that clears only the background.

### Chrome

- `theme.TitleBar(width, view, facts, trailing)` is the line a view opens with and the rule under it: `♩ legato`, the view's name, facts about what it shows, and at the far end what it is set to. Trailing pieces are dropped from the last when the line is too narrow.
- The key bar (`internal/tui/statusbar`) is one line along the bottom. A message comes first, then the page's keys; bindings marked `BarEnd` (leave the page, change how it is shown, the key reference) sit at the far end. Neighbours with the same label are one hint (`j/k select`), and `enter` is written `↵`.
- The board and the agents view show the sync state and the web server themselves (`SetStatus`, fed from `statusbar.Model.SyncLabel` and `WebLabel`). The app tells the bar so with `StatusShownElsewhereMsg`; in the other views the bar shows them.

## Board

`internal/tui/board`: `view.go` (layout, header, task panel), `column.go`, `card.go`.

- **Header.** How many tasks, how many have an agent, the workspace filter and `hide empty: on|off` as chips, then the sync state and the web server.
- **Columns.** A name, a count and a rule in the column's color (`theme.ColumnBorderColor`; the name of the column in progress is in the accent). Open columns share the width, between 24 and 72 columns each. When they do not all fit, the ones nearest the cursor are kept.
- **Collapsed columns.** A column with no cards is drawn as a rail five columns wide: its rule, its count, and its name written downwards. `z` opens them all and collapses them again. `h` and `l` step over a collapsed column; a number key still reaches one. A board with nothing on it keeps its columns open.
- **Cards.** The priority mark (`▲`, `▼`, or `·` for none) and the key, with where the task comes from at the far end; the title; then its type, workspace, swarm and review marks and the state of its pull request (`✓ CI · ✓ review · 2 comments`). The pull request gets a line of its own when both do not fit.
- **Agent strip.** The last line of a card with an agent, on the tint of its state: `running`, `needs you`, `background`, `error`, `mixed`, `idle`, with the time that goes with it. `ready for review` (blue) replaces `idle` when the agent marked its review tour ready. A card with no agent now shows the time agents spent on it.
- **Selection.** The selected card has a rounded frame in the accent. The first and last line of a card are its frame, blank when it is not selected, and a column lays the last line of one card over the first of the next: cards are one line apart and selecting one moves nothing.
- **Task panel.** From 100 columns of width, a panel a quarter of the width (34 to 48 columns) describes the selected task: priority, type and key, the title, where it stands (column, workspace, source, agent, pull request, worktree, swarm, agent time), the start of its description as plain text, and the keys that act on it. The description is read with `GetCard` once for each task that becomes selected.

## Agents

`internal/tui/agents`: `list.go` (the list beside one terminal), `grid.go` (the ensemble), `conversation.go` (a native agent's conversation in a tile), `shown.go`, `usage.go`.

Agents are listed by what they are doing: **NEEDS YOU** (waiting, or stopped on an error), **RUNNING**, **BACKGROUND**, **IDLE**, **ENDED**. Inside one state, swarm participants stay together (conductor first), then grouped agents, then the rest. The selection follows an agent that changes state.

### List

- **Sidebar** (28, 34 or 40 columns by terminal width, on `Surface`). The brand and the count; each agent as two lines: its mark and ID with the time that goes with its state, then the tool it runs and its task with its recent activity (`▰` for a stretch it worked or waited, `▱` for none). A swarm worker adds `↳ swarm <parent>`, a grouped agent `● <group>`. The selected agent has an accent bar down its left side.
- **Foot of the sidebar.** The tokens used today when known (see below), then the sync state and the web server.
- **Header.** `<id> › <tool> › <task>`, and at the far end the state and its time.
- **Banner.** When an agent other than the selected one needs the person, a line above the foot names it. `w` selects the next such agent, around.
- **Foot.** How to talk to the selected agent (`↵ attaches`, `m` sends a macro, `v` dictates).

### Ensemble

`e` shows every agent at once; `e` again goes back. The registry has the two layouts as modes of the agents page (`ModeList`, `ModeGrid`), so `h` and `l` move between tiles in the ensemble while `l` toggles worker details in the list.

- **Header.** Agents counted by state.
- **Tiles.** Up to four in a row, each at least 40 columns wide, in pages of rows that follow the selection. A tile has a head (the agent's mark, ID and tool, and its state or, while it runs, its time), its task, what the agent is at, and a foot with its recent activity and how full its context is (`ctx ▰▰▰▱▱ 60%`, amber from 70%, red from 85%). A tile that needs the person has its frame and header in the state's color; the selected tile has an accent frame. A last tile says how to spawn an agent.
- **What an agent is at.** For an agent with a native endpoint (Claude Code, Codex, Chimera) the tile shows the end of its conversation, read from the same snapshot the web UI's native panel is drawn from. For an agent driven through its terminal it shows the last lines of the pane.
- **Reading it.** While the ensemble is in front, the app asks every running agent every two seconds (`PreviewsDue`, `loadAgentPreview` in `app.go`): `service.AgentConversationReader.ConversationTail` first, and the pane (`CaptureOutput`) when the agent has no endpoint or it does not answer. A `ConversationMsg` replaces what a `PreviewMsg` left for that agent, and the other way round. The list captures only the selected agent's pane, five times a second.
- **Activity.** From 120 columns of width, a panel lists what changed among the agents since the TUI first saw them, newest first: started, started working, needs you, went idle, ended, closed. The record is kept in memory (the last 100 changes) and is fed from every agent list the app loads, so it also grows while the board is in front.
- **Under the activity.** The tokens of each of the last 24 hours when usage is known; otherwise how many agents were at work in each recent stretch of time.

### A conversation in a tile

`conversationLines` fills the tile from the newest entry back:

| Entry | Drawn as |
| --- | --- |
| What the person sent | `› text`, at most two lines |
| What the agent said | Plain text set in from the marks, without Markdown's marks. The newest gets up to half the tile and is bright; older ones get two lines. A message that is cut ends with `…` |
| A tool call | One line: its state, the tool, and what the call is for. `●` done, `✗` failed, a spinner while the agent runs it |
| A run of tool calls | Its newest three, under `⋮ N earlier calls`; more when the tile has room to spare |
| An open request | `Wants to run:` with the command set apart, `Asks you to approve a plan:`, or `Asks:` with the question and its options |

- A tool call is described by `service.ConversationTail`: the file name for `read`, `edit` and `write`; otherwise the agent's own description of the call when it gave one (Claude Code's `Bash`), then the endpoint's summary, then the command.
- A plugin's tool is named without the plugin's prefix (`show_card`).
- Claude Code raises a permission request in Legato only while a native viewer (the web panel) is attached, so the snapshot of a waiting session often has no open request. Its unfinished call is then marked `◆` and shows the command itself, since that is what waits to be allowed.
- A snapshot holds the whole conversation (1 to 4 MB for a long Claude Code session; about 10 to 35 ms to fetch from the bridge and 5 to 20 ms to read). Only the entries that are kept are decoded past their name; tool arguments and results stay raw. That cost is why the ensemble reads every two seconds and not faster.

## Spawning an agent

`internal/tui/overlay/agent_spawn.go`. `s` in the agents view opens it for an ephemeral agent, and `a` on a board card for that task's agent. It asks for a title, the agent tool, the resources to give it (when there are any), and where the agent starts. `tab` and `shift+tab` move between them, `↵` spawns from any of them, and a line above the keys says what `↵` will do (`→ chimera in ~/Development/autopilot · titled legato complete`).

- **Title.** Typed for an ephemeral agent; a task's title is shown and cannot be changed.
- **Agent.** The default adapter first, then the other adapters, then `shell`. `h`/`l` or the arrow keys choose, also while "where" lists directories.
- **Resources**, when there are any (see [Resources](resources.md)): a box for each, between **agent** and **where**. `h`/`l` move and `space` ticks; one that gives any path to write is marked `✎`.
- **Where, as a list.** The directory the agent would start in without a choice (the directory Legato runs in; for a task, its worktree or remembered directory), then the directory presets, then `custom path…`. `↑`/`↓` or `j`/`k` move, a number key picks the row with that number, and `/` or `↵` on the last row asks for a path. A default directory that is one of the presets is selected there and not listed twice.
- **Where, as a path.** The typed path starts inside the selected directory. Under it are the directories that complete it: directories only, the one typed in full first, then presets, then Git repositories, then the rest; hidden ones once a dot is typed. Each says what it is (`git · preset 2`, `git`, `dir`), and a line says how many matched.
  - `tab` completes the path to the picked suggestion, `↑`/`↓` pick, `ctrl+w` goes up a directory, `esc` returns to the list, and `shift+tab` leaves the field.
  - `↵` spawns in the picked suggestion. A path that ends in `/` is the directory itself, and its suggestions are what is inside it; `↓` picks one of those.
  - `ctrl+s` saves the directory as a preset, named after the directory. The line above the keys offers it only for a directory that is not a preset yet.
- **Where the directories come from.** The overlay reads no disk. It sends `AgentSpawnPathChangedMsg` and `AgentSpawnSavePresetMsg`; the app asks `service.DirectoryService` (`app_spawn.go`) and answers with `AgentSpawnSuggestionsMsg` and `AgentSpawnPresetsMsg`. An answer for a path that has changed since is dropped. Without a directory service the overlay offers the default directory and a path typed in full.
- **Presets** are the `repositories` of the configuration that name a `primary_dir`, then the directories saved here or in the web UI (`directory_presets`). There is no way to remove a saved one yet.
- The panel is drawn on `theme.Inset`, darker than the page it covers, at most 92 columns wide. A terminal too short for its blank lines gets it without them.

## The mark of a running agent

A running agent's mark is a spinner (`theme.Spinner(frame)`, ten frames). The app advances the frame every 100 ms (`app_spinner.go`) and passes it to the board and the agents view. It ticks only while the view in front shows a running agent and no overlay is open, and one tick at a time, so the app does not redraw between events when nothing moves. `Icons.AgentWorked` is the still mark for time an agent spent working.

## Token usage

With a `manticore:` block in the config, the agents view shows how many tokens were used today: the total, and under it what went in and what came out. The ensemble's header has the same, and its activity panel draws the tokens of each of the last 24 hours.

- `internal/engine/manticore` reads the instance's dashboard API: `GET <url>/api/dashboard/summary?startDate=<UTC>` and `GET <url>/api/dashboard/time-series?bucket=hour&startDate=<UTC>`. That API takes no key; the configured key is used only by the usage mod that reports to the instance.
- `service.UsageService` counts today from local midnight and lays the last 24 hours out hour by hour. It exists only when the block is set (`app.SetUsageService` in `cmd/legato/main.go`).
- The app reads it at start and every minute (`app_usage.go`). A read that fails leaves the last number in place.
- The numbers are those of the whole instance: every Client it records, not only this Legato. Input counts the whole of each prompt, the part read from a cache included.

## Checking the look

Tests assert text and sizes, not colors: lipgloss adds no escape sequences without a terminal. A view test should check that the view is exactly the size it was given (every line as wide as the width), which is what keeps panels side by side.

To see colors, run a build with a path of its own against a throwaway config (see [Claude Code native integration](claude-code-native.md) § Trying it without touching real state) inside a tmux session with `COLORTERM=truecolor`, and read the pane with `tmux capture-pane -e -N -p`. Two things about that output: tmux writes only what changes from one cell to the next, across lines too, so a converter must carry colors from line to line; and without `-N` it drops trailing cells, which makes a filled panel look unpainted.
