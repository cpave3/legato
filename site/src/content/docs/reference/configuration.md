---
title: Configuration
description: Every setting of config.yaml, with its type, its default and what it does.
sidebar:
  order: 10
---

Legato reads its settings from one YAML file, `config.yaml`. This page lists every setting in it. Each feature page explains its own settings in more detail and links here.

## The file

Legato looks for the file in this order and uses the first that is set:

1. `$LEGATO_CONFIG`
2. `$XDG_CONFIG_HOME/legato/config.yaml`
3. `~/.config/legato/config.yaml`

You do not need to write a file. When there is none, the TUI or `legato serve` writes one that sets nothing: comments that point to Settings and show the file-only settings you might want. On the first start, when the database has no columns yet, a setup wizard adds the columns of `board.columns` and offers to set up Jira, whose settings it saves in the database. A `config.yaml` written by provisioning (an Ansible playbook, say) still pins everything it holds.

Legato expands environment variables in the file before it reads the YAML, so you can keep secrets out of it:

```yaml
jira:
  api_token: ${LEGATO_JIRA_TOKEN}
```

Both `${NAME}` and `$NAME` are expanded, and a variable that is not set becomes an empty string. The one exception is `macros`: Legato sends macro text exactly as you wrote it, `$` and all.

:::caution
Because of the expansion, a `$` in any other value starts a variable name, and there is no way to escape it: `pa$word` becomes `pa` followed by the value of `word`, which is usually empty. Put such a value in an environment variable and write `${NAME}`; Legato does not expand the variable's value again.
:::

Legato checks the file when it starts. A value it cannot use (an unknown option, a malformed address, a duration it cannot read) stops it with an error that names the setting.

### Settings in the file and in the database

Most settings can also be kept in Legato's database, and changed from the web UI without touching this file. The two layer:

- **A value in `config.yaml` wins.** The setting is *pinned*: the web UI shows it locked, and only an edit to the file changes it. Remove it from the file and, at the next start, the database's value (or the default) applies again.
- **Otherwise the database's value applies,** or the default when there is none.
- **Lists are pinned entry by entry.** Workspaces, macros, agent tools (`adapters`), credentials, credential sources, tmux options, sandbox mounts and swarm prompts are each the file's entries plus those added in Settings › Lists, matched by name. An entry whose name the file has is locked, and the web UI will not add another of that name. Repositories, resources and repository guidance work the same way, in their own sections.
- **Secrets are kept by name.** A setting that holds a secret (`jira.api_token`, `github.token`, `notifications.ntfy.token`, `manticore.api_key`) takes, in the database, only the name of a secret: `legato://<name>` in Legato's own store (`legato secret add`), or `op://<vault>/<item>/<field>` in 1Password. Legato reads the secret when it starts. In the file, `${ENV}` references work as before.
- **A change is checked before it is kept**: the value by its own rule, then the whole configuration with the rules Legato uses for this file. A change that would leave it invalid is refused with that error, so a value in the database never stops Legato from starting.

Some settings are **file-only**: they live only in this file, and nothing else changes them. They are what Legato needs before its database opens (`db.*`, `data_dir`), what unseals its secrets (`secrets.master_key`, `secrets.onepassword`), how it is reached (`web.enabled`, `web.port`, `web.tls`, `web.public_url`), and the signing trust root (`approval_devices.android_app_digests`). They are marked *file-only* below.

**Moving settings out of the file.** After the upgrade that brought settings into the database, Attention offers once to move every setting `config.yaml` pins into the database, listed in Settings › Lists with a tick each; untick any to keep it in the file. Each pinned setting in Settings also has **Move to the database**. A move copies the values into the database, rewrites `config.yaml` without them (its comments and order kept, the old file kept beside it as `config.yaml.<time>.bak`), and restarts Legato; if Legato does not start on the result, it puts the old file and values back. A setting that holds a secret, or whose value reads an environment variable, stays in the file. So does a setting the file needs beside a secret it keeps, such as `manticore.url` beside `manticore.api_key`: Legato reads `config.yaml` before the database when it starts, so a move never leaves a file that does not load by itself.

**Export and import.** `legato config export` writes the settings kept in the database as a file shaped like `config.yaml`; `legato config import` turns such a file into one change set to approve, so one instance can set up another.

You can see what Legato runs with, and where each value comes from, with `legato config show --sources`.

### When a change takes effect

A running Legato (the TUI or `legato serve`) reads the file once, when it starts. Restart it after you change the file. Commands such as `legato task create` read the file each time they run.

Some things change without a restart, because they are not in this file:

- The items of a 1Password vault listed in `credential_sources`. Legato lists the vault again every two minutes.
- The 1Password token in `secrets.onepassword.token_file`. Legato reads the file each time it uses the token.
- Repositories, directories and resources you add in the web UI, and the update mode, the tint and Burnlite's projects you set there. Legato keeps those in its database.

Board columns live in the database too. After you change `board.columns`, run this to make the board match the file:

```bash
legato columns sync
```

### A small example

```yaml
board:
  columns:
    - name: Backlog
    - name: Doing
    - name: Review
    - name: Done

workspaces:
  - name: Work
    color: "#4A9EEF"
default_workspace: Work

web:
  enabled: true

repositories:
  - name: Legato
    repo: cpave3/legato
    primary_dir: ~/Projects/legato
    base_branch: main

notifications:
  ntfy:
    topic: legato-change-me-7f3a9c
```

## General

Settings at the top level of the file.

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `icons` | string | `unicode` | The glyphs on cards: `unicode`, or `nerdfonts` if your terminal font is a Nerd Font. |
| `editor` | string | `$VISUAL`, then `$EDITOR`, then `vi` | The command that opens when you edit a task's description (`e` in the detail view). |
| `db.driver` | string | `sqlite` | *File-only.* The database. `sqlite` is the only one; any other value stops Legato at start. |
| `db.path` | path | `$XDG_DATA_HOME/legato/legato.db`, else `~/.local/share/legato/legato.db` | *File-only.* Where the database file is. |
| `data_dir` | path | the folder that holds `db.path` | *File-only.* Where Legato keeps its other files: the web token, TLS certificates, Jira attachments, workflow artifacts, repository clones and its own secret store. |
| `ssh_agent_socket` | path | none | Sets `SSH_AUTH_SOCK` for Legato and everything it starts. Without it, Legato keeps the `SSH_AUTH_SOCK` it was started with; when that is empty, it uses 1Password's agent at `~/.1password/agent.sock` if that exists. With an `ssh` credential configured, agent sessions get their own socket instead (see [the credential broker](/legato/security/credential-broker/)). |
| `theme` | string | `default` | Not used. |
| `keybindings.vim_mode` | bool | `true` | Not used. The board always takes vim keys. |

## Jira

Connects the board to Jira, so that tickets you import stay up to date and moving a card moves its issue. Legato talks to Jira only when `jira.base_url`, `jira.email` and `jira.api_token` are all set. See [Jira](/legato/integrations/jira/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `jira.base_url` | URL | none | Your Jira site, such as `https://example.atlassian.net`. |
| `jira.email` | string | none | The email address of your Atlassian account. |
| `jira.api_token` | string | none | An Atlassian API token. Use `${VAR}` to keep it out of the file. |
| `jira.project_keys` | list of strings | none | The projects that the import search (`i` on the board) looks in. Empty searches every project. |
| `jira.jql_filter` | string | none | JQL that every sync adds to its query. A tracked ticket that does not match is not refreshed. |
| `jira.sync_interval_seconds` | integer | `60` | How often Legato pulls the tickets it tracks. |
| `jira.status_sync_enabled` | bool | `true` | Moves cards when their Jira status changes, and transitions the issue when you move a card. `false` keeps pulling ticket details but changes no status in either direction. |
| `jira.attachment_max_size_bytes` | integer | `26214400` (25 MiB) | The largest image attachment Legato downloads from a ticket. It downloads only images. |

## Burnlite

Connects the board to Burnlite, so that you can import its tasks and they stay up to date. Legato reads Burnlite only after you connect it: run `legato integration connect burnlite`, or use Settings › Integrations in the web UI. Moving a card never changes the task in Burnlite. See [Burnlite](/legato/integrations/burnlite/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `burnlite.url` | URL | none | Your Burnlite, such as `https://burnlite.example.com`. `legato integration connect burnlite` and the web UI's connect form use it when you give no address. |
| `burnlite.projects` | list of strings | none | The project keys (`EST` in `EST-42`) that the import search looks in. Empty uses the projects saved in the web UI, and with none of those, every project. |
| `burnlite.sync_interval_seconds` | integer | `60` | How often Legato pulls the tasks it tracks. |
| `burnlite.status_sync_enabled` | bool | `true` | Moves a card when its task changes state in Burnlite. `false` keeps pulling task details but never moves a card. |
| `burnlite.columns.ready` | string | `Ready` | The column of an open task that is not claimed and has no open blocker. |
| `burnlite.columns.blocked` | string | `Backlog` | The column of an open task with an open blocker. |
| `burnlite.columns.claimed` | string | `Doing` | The column of an open task that someone has claimed. |
| `burnlite.columns.closed` | string | `Done` | The column of a closed task, completed or not planned. |

## Board

The columns of the board, in order. Each column can map Jira statuses to it. Run `legato columns sync` after a change. See [The board](/legato/using/board/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `board.columns[].name` | string | Backlog, Ready, Doing, Review, Done | The column's name. |
| `board.columns[].remote_statuses` | list of strings | see below | Jira statuses whose tickets belong in this column. A column with none is local only: sync never moves a card out of it. |
| `board.columns[].remote_transition_id` | string | none | The Jira transition to run when you move a card into this column. Without it, the move stays local. |

The default columns map these statuses: Backlog takes `To Do`, `Open` and `Backlog`; Ready takes `Ready for Dev` and `Selected for Development`; Doing takes `In Progress` and `In Development`; Review takes `In Review`; Done takes `Done` and `Closed`.

When `legato columns sync` removes a column, its tasks move to a column named `Unmapped` if you have one, or else to the first column.

```yaml
board:
  columns:
    - name: Backlog
      remote_statuses: [To Do, Open]
    - name: Doing
      remote_statuses: [In Progress]
      remote_transition_id: "21"
    - name: Done
      remote_statuses: [Done]
      remote_transition_id: "31"
```

## Workspaces

Workspaces group tasks, such as work and personal. Legato adds the ones listed here to its database each time it starts. See [The board](/legato/using/board/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `workspaces[].name` | string | none | The workspace's name. It must not be blank, and names must differ, ignoring case. |
| `workspaces[].color` | string | none | A hex colour for the workspace, such as `"#4A9EEF"`. Quote it: YAML reads `#` as a comment. |
| `workspaces[].path` | path | none | Not used. |
| `default_workspace` | string | none | The workspace a new task goes in when you do not pick one: in the TUI, in the web UI, and for `legato task create` without `--workspace`. It must name a workspace above. |
| `default_import_workspace` | string | `default_workspace` | The workspace an imported ticket goes in when you do not pick one. It must name a workspace above. |

A choice you make yourself always wins over these defaults, including **No workspace**. The defaults apply to new tasks only.

## Agents

How Legato runs the agents it starts. See [Agents](/legato/using/agents/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `agents.escape_key` | string | `ctrl+]` | The key that detaches you from an agent's terminal after you attach to it from the TUI. `ctrl+<key>` is turned into tmux's `C-<key>`; other values go to tmux as written. |
| `agents.tmux_options` | map of strings | none | tmux options set on each agent's session, such as `mouse: "on"` or `history-limit: "50000"`. |
| `agents.sparkline_window` | duration | `10m` | The stretch of time each agent's activity sparkline covers, in the TUI and the web UI. A value Legato cannot read falls back to `10m`. |
| `agents.peer_awareness` | bool | `true` | Tells an agent when other agents work in the same folder, and tells them when it joins. `false` stops these notices; agents can still message each other. |
| `agents.reviews` | bool | `true` | Tells agents to make a [review tour](/legato/using/reviews/) and a handoff packet at the end of their work. `false` leaves those instructions out and tells agents to skip both unless you ask for a review. The review pages and `legato review` commands still work. |
| `agents.guidance[].repo` | string | none | The repository an entry of guidance is for: `owner/name` on GitHub, `host/owner/name`, or a Git remote address. Legato matches it against the origin remote of the folder an agent works in, so it applies in every checkout and worktree. One entry per repository. See [Guidance for a repository](/legato/using/agents/#guidance-for-a-repository). |
| `agents.guidance[].text` | string | none | The guidance itself, added to the launch prompt of every agent that works in that repository. Must not be empty. |
| `agents.repository_resources[].repo` | string | none | A repository whose agents all get some [resources](/legato/agent-tools/resources/), written as for `agents.guidance[].repo`. Legato matches it against the task's repository, else the origin remote of the folder an agent works in, so it applies in every checkout, worktree and fresh clone. One entry per repository. Only in this file. See [Resources for a repository](/legato/agent-tools/resources/#resources-for-a-repository). |
| `agents.repository_resources[].resources` | list of strings | none | Names of resources in this file's `resources` block. Every agent working in the repository gets them as if they were ticked when it started: listed in its launch prompt, granted to Chimera, and mounted in its sandbox. A name that is not in `resources` stops Legato loading the file. |
| `agents.mcp.enabled` | bool | `true` | Gives every agent session Legato's own tools, such as cards, artifacts and task notes. Nothing is written to the agent tool's own settings. See [Legato's tools for agents](/legato/agent-tools/mcp-tools/). |
| `agents.mcp.resource_domains` | list of strings | `cdnjs.cloudflare.com`, `cdn.jsdelivr.net`, `unpkg.com`, `fonts.googleapis.com`, `fonts.gstatic.com` | The hosts that HTML an agent shows may load scripts, styles, fonts and images from. Write plain host names only. An empty list allows none. |
| `agents.sandbox.enabled` | bool | `false` | Starts every agent tool inside a bubblewrap sandbox, unless you start it with **Run without sandbox**. Swarm and workflow agents cannot opt out. Needs `bwrap`. See [The sandbox](/legato/security/sandbox/). |
| `agents.sandbox.backend` | string | this host's | What builds the sandbox: `bwrap` (bubblewrap) on Linux, `seatbelt` (`sandbox-exec`) on macOS. Leave it empty for the host's own; naming the other host's fails to load. |
| `agents.sandbox.default_mounts` | bool | `true` | Shows your toolchains inside the sandbox read-only, and your package and build caches read-write. |
| `agents.sandbox.mounts[].path` | path | none | Another file or folder every sandbox sees. Absolute, or starting with `~/`. |
| `agents.sandbox.mounts[].access` | string | `read` | `read` or `write`. |
| `agents.sandbox.scratch_dir` | path | `<data_dir>/sandbox` | Where each sandbox's `/tmp`, home folder and throwaway layer are kept on disk until its agent ends. Absolute, or starting with `~/`, and not your home folder, a folder that holds it, or a folder that overlaps Legato's `repos`, `claude-mod` or `jobs` folders, such as the data folder itself. A folder the sandbox would otherwise show, such as a cache, is then not shown. See [Scratch space](/legato/security/sandbox/#scratch-space). |
| `agents.sandbox.docker.enabled` | bool | `false` | Gives every sandboxed agent a Docker socket of its own that passes its requests to your Docker daemon only when they keep within its sandbox: its own containers, networks and volumes, and bind mounts only of what its sandbox sees, read-only where it sees them read-only. Agents run without the sandbox are unchanged. See [Docker in the sandbox](/legato/security/sandbox/#docker-in-the-sandbox). |
| `agents.sandbox.docker.host` | string | your `DOCKER_HOST`, else your Docker context, else `unix:///var/run/docker.sock` | The daemon the socket passes requests to: `unix:///path` or `tcp://host:port` without TLS. A rootless Docker's or Podman's socket works too. |
| `agents.sandbox.docker.cap_add` | list of strings | none | Capabilities a container may add with `--cap-add`, such as `SYS_PTRACE`. Capabilities that reach the host, such as `SYS_ADMIN` and `ALL`, stop Legato loading the file. |
| `agents.sandbox.docker.publish` | string | `loopback` | Where published ports bind: `loopback` puts every published port on `127.0.0.1`, which agents reach since they share your network; `any` binds what the agent asks for. |

```yaml
agents:
  tmux_options:
    mouse: "on"
    history-limit: "50000"
  sandbox:
    enabled: true
    mounts:
      - path: ~/notes
      - path: ~/scratch
        access: write
```

## Adapters

Settings for each agent tool, keyed by its name: `claude-code`, `codex` or `chimera`. See [Model settings](/legato/agent-tools/model-settings/) and the pages for [Claude Code](/legato/agent-tools/claude-code/), [Codex](/legato/agent-tools/codex/) and [Chimera](/legato/agent-tools/chimera/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `adapters.<tool>.launch_args` | list of strings | none | Flags added to the command that starts the tool. Legato quotes them for the shell. |
| `adapters.<tool>.tiers.<tier>.description` | string | none | What the tier is for. A swarm's conductor reads it to pick a tier for each sub-task. |
| `adapters.<tool>.tiers.<tier>.launch_args` | list of strings | none | Flags added after the tool's own `launch_args` for a sub-task of this tier, so a tier's `--model` wins. |
| `adapters.<tool>.modes` | map of strings | none | Chimera only. Maps a swarm role (`conductor`, `worker`, or a role of your own) to a Chimera mode, passed as `--mode`. A role with no entry of its own takes the `worker` entry. Legato ships no modes. |
| `adapters.<tool>.native_bridge` | bool | `true` | Claude Code and Codex only. Connects each session to Legato beyond its terminal, so Legato can message it, interrupt it and answer its permission requests directly, and the web UI can show its conversation. `false` keeps the tool's sessions in tmux only. |
| `adapters.<tool>.models` | list of strings | none | Claude Code only. Model IDs the web UI offers for a session, beside the model it runs. Write full IDs, such as `claude-sonnet-5-5`, not aliases. |

Tier names are yours to choose. A swarm plan that names a tier its tool does not have is refused.

```yaml
adapters:
  claude-code:
    launch_args: ["--permission-mode", "acceptEdits"]
    models: [claude-sonnet-5-5, claude-opus-5-5]
    tiers:
      small:
        description: Quick, well-defined edits
        launch_args: ["--model", "haiku"]
      large:
        description: Design work and hard bugs
        launch_args: ["--model", "opus"]
  chimera:
    modes:
      conductor: my-orchestrator
      worker: my-worker
```

## Swarms

How a swarm's conductor and workers start, and how large a plan may be. See [Swarms](/legato/using/swarms/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `swarm.enabled` | bool | `false` | Turns swarms on: the `legato swarm` commands, the swarm endpoints, the swarm controls in the terminal UI and the **Start swarm** buttons in the web UI. Off by default. Restart Legato after changing it. |
| `swarm.conductor.agent` | string | `swarm.worker.agent`, then `claude-code` | The tool a new conductor runs: `claude-code`, `codex` or `chimera`. |
| `swarm.conductor.model` | string | none | A model for new conductors, added to the tool's launch flags. |
| `swarm.worker.agent` | string | `claude-code` | The tool a worker runs when its sub-task names none. It is also Legato's default agent tool. |
| `swarm.worker.model` | string | none | A model for workers that run `swarm.worker.agent` and name no tier. |
| `swarm.max_concurrent_agents` | integer | `4` | How many workers of one swarm may run at once. Further sub-tasks wait. |
| `swarm.max_subtasks_per_plan` | integer | `10` | The most sub-tasks a plan may have, in total and in any one step. |
| `swarm.max_steps_per_plan` | integer | `10` | The most steps a plan may have. |
| `swarm.strict_scope` | bool | `false` | Refuses to dispatch a sub-task whose files overlap another's. When `false`, the conductor gets a warning instead. |
| `swarm.brief_kickoff_delay_ms` | integer | `250` | How long Legato waits after starting a tool before it types "read your brief". Raise it if your tool starts slowly. |
| `swarm.prompts` | map of maps | none | Your own system prompt for a role and tool, in place of Legato's. The outer key is the role, the inner key the tool, and the value is the prompt's text. |
| `swarm.require_user_close` | bool | `false` | Not used yet. |

```yaml
swarm:
  worker:
    agent: claude-code
  conductor:
    agent: claude-code
    model: claude-opus-5-5
  prompts:
    backend:
      claude-code: |
        You are a backend worker. ...
```

The older settings `swarm.default_agent`, `swarm.conductor_agent` and `swarm.conductor_tier` are gone. Legato stops at start when it finds them and names the setting to use instead.

## Macros

Named text you send to an agent with one key: `m` in the TUI's agents view, or **Macros** in the web UI. See [Agents](/legato/using/agents/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `macros[].name` | string | none | The label in the macro list. |
| `macros[].keys` | string | none | The text sent to the agent, exactly as written. A trailing `\n` presses Enter. |

```yaml
macros:
  - name: run tests
    keys: "task test\n"
```

## Groups

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `groups.defaults` | list of strings | none | Group names always offered when you set an agent's group (`g` in the TUI's agents view), beside the groups agents already have. The web UI's agent list shows these groups first, even when empty. |

## Voice

Dictate to an agent: press `v` in the TUI's agents view to start recording and again to stop. Legato sends the audio to a wyoming-faster-whisper server and gives the text to the selected agent. Recording uses `arecord`, so it works on Linux only.

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `voice.enabled` | bool | `false` | Turns dictation on. Without `voice.whisper_url` it stays off. |
| `voice.whisper_url` | string | none | The Wyoming address of the whisper server, such as `tcp://192.168.1.220:10300`. A bare `host:port` works too. It can be the Whisper that Home Assistant Assist already uses; Legato's requests and Assist's then wait for each other. |
| `voice.autosend` | bool | `false` | Sends the text as a message. When `false`, Legato types the text into the agent's terminal without pressing Enter, so you can finish it there. |
| `voice.mic_device` | string | `default` | The ALSA device to record from, such as `hw:1,0`. |

## Web server

The web UI and its API. The TUI starts it beside itself when `web.enabled` is on; `legato serve` runs it without a TUI. See [The web server](/legato/running/web-server/) and [Web authentication](/legato/security/web-auth/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `web.enabled` | bool | `false` | *File-only.* Starts the web server with the TUI. If the port is in use, the TUI starts without it. |
| `web.port` | string | `"3080"` | *File-only.* The port of the web server, for the TUI's (`web.enabled`) and for `legato serve`. `legato serve --port` overrides it for one run. |
| `web.tls.enabled` | bool | `false` | *File-only.* Serves HTTPS with a certificate Legato makes and signs itself, kept in `<data_dir>/certs/`. |
| `web.tls.cert` | path | none | *File-only.* A certificate file of your own. With `web.tls.key`, it turns HTTPS on. |
| `web.tls.key` | path | none | *File-only.* The private key of `web.tls.cert`. |
| `web.tls.hostname` | string | none | *File-only.* Another host name for the certificate Legato makes, such as `mybox.local`. It already covers `localhost` and this machine's addresses. |
| `web.tint` | string | none | A colour that tells this Legato from others in the web UI: `red`, `orange`, `yellow`, `green`, `teal`, `blue`, `purple` or `pink`. Usually set in Settings › Appearance. |
| `web.discovery` | bool | `true` | Announces the server on the local network (mDNS), so the Legato phone app finds it. See [Devices](/legato/running/devices/). |
| `web.public_url` | URL | the first local address Legato announces | *File-only.* The address a phone uses to reach this server, put in the links of approval notifications. It must be an `http` or `https` address. |

## Reeve

The Reeve's defaults, read when a session starts. Usually set in Settings › Reeve. See [The Reeve](/legato/using/reeve/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `reeve.name` | string | empty: "the Reeve" | What Legato calls the Reeve on this instance, in the web UI, the desktop app, the TUI, notifications and the Reeve's own prompt. One line of plain text, at most 40 characters. See [Give it a name](/legato/using/reeve/#give-it-a-name). |
| `reeve.agent` | string | the default agent tool | The agent tool a session runs: `claude-code`, `codex` or `chimera`. |
| `reeve.model` | string | the tool's default | The model or tier a session runs. |
| `reeve.idle_timeout` | duration | `2h` | How long a session may be idle before its agent is stopped. The next message to it resumes it. |
| `reeve.levels.change` | string | `approve` | What a change the Reeve proposes needs: `auto` (it runs at once, with no approval asked), `approve` (a tap), `signed` (an enrolled device) or `verified` (signed, with a fingerprint or a PIN). Making it stricter needs nothing; anything else needs a signed approval. See [What a change needs](/legato/using/reeve/#what-a-change-needs). |
| `reeve.levels.risky` | string | `default` | The same for a risky change (restarts, stopping agents, settings that choose what runs on this machine). `default` is `signed` once a device is enrolled, else `approve`. `auto` runs risky changes with no approval asked; set it in Settings, where it carries a warning. In `config.yaml`, `auto` here is read as `default`, which is what it meant before `auto` was a level. |
| `reeve.rules[].pattern` | string | none | A call a rule matches: `METHOD /path`, a `legato` subcommand, or a host operation (`host_shell:<command>`, `edit_file:<path>`, `manage_service:<action> <unit>`; `~` is your home folder), `*` for any run of characters. |
| `reeve.rules[].level` | string | none | The level the rule gives the calls it matches, higher or lower than their class's: `auto`, `approve`, `signed` or `verified`. A rule never lowers a call below its floor (see [What a change needs](/legato/using/reeve/#what-a-change-needs)), and never makes a shell command with `;`, `|`, `&`, `$(`, a backquote or a redirect `auto`. |
| `reeve.host_operations` | bool | `true` | Offers the Reeve a shell, file edits and systemd services on this machine, each as a change you approve. Turning it on needs a signed approval. |
| `reeve.host_operations_auto` | bool | `false` | Runs the Reeve's host operations that are not as root at once, with no approval asked: **the Reeve can then run any command as your user on this machine.** Each is still audited. A command as root still needs a verified approval. Turning it on needs a signed approval; turning it off never does. See [Commands on the machine](/legato/using/reeve/#commands-on-the-machine). |
| `reeve.host_timeout` | duration | `10m` | How long one host command may run. |
| `reeve.host_output_cap` | int | `65536` | How many bytes of a host command's output the Reeve is given; the rest is kept in a file it can read. |
| `reeve.peer` | string | `start` | What a message from another instance's Reeve does when no session runs here: `start` a session, `ask` you first in Attention, or `refuse` it. Changed only with a signed approval. |
| `reeve.launch_args` | list of strings | none | Arguments added to the agent tool's launch command. |
| `reeve.voice.enabled` | bool | `false` | Lets the Reeve speak out loud on this machine's speakers with its `speak` tool. Sessions started after you turn it on get the tool; turning it off mutes the Reeve at once. See [The Reeve's voice](/legato/using/reeve/#the-reeves-voice). |
| `reeve.voice.command` | string | none | Makes a line's audio. Legato runs it as `<command> "<text>" <out-file>`, and it writes an audio file to `<out-file>`. |
| `reeve.voice.credential` | string | none | A credential Legato gives the command in an environment variable, as `elevenlabs`. Leave it empty for a local voice. |
| `reeve.voice.credential_env` | string | `<NAME>_API_KEY` | The variable the command finds the credential in. When empty, the credential's name in capitals with `_API_KEY`, as `ELEVENLABS_API_KEY`. |
| `reeve.voice.player` | string | `pw-play`, else `ffplay` | Plays a line's audio file, named after the command and its arguments. |
| `reeve.voice.pause` | duration | `1s` | When you talk to the Reeve, how long you stay quiet before what you said is sent, from `0.3s` to `2s`. See [Talking to the Reeve](/legato/using/reeve/#talking-to-the-reeve). |
| `reeve.voice.follow_up` | duration | `6s` | How long the voice listens for your reply after the Reeve has spoken. `0s` for a single turn. |
| `reeve.voice.quiet_hours` | string | none | Hours when no line plays on this machine's speakers, as `22:00-07:00` in its local time; the Reeve replies in writing instead. A reply to something you said to it still plays on your device. |
| `reeve.voice.partials` | bool | `true` | Shows the words so far while you speak, by transcribing about once a second. Turn it off when Whisper runs on a CPU or is shared with Home Assistant. |
| `reeve.watch.settle` | duration | `30s` | How long an agent the Reeve watches must stay idle, or busy only in the background, before the Reeve is told, so brief pauses do not wake it. See [Watching agents](/legato/using/reeve/#watching-agents). |
| `reeve.watch.started` | bool | `true` | Watches every agent the Reeve starts or messages. |
| `reeve.watch.wake` | bool | `false` | When a watched agent needs the Reeve and no session is running, starts or resumes one to tell it. Off, the news waits for the next session. On costs tokens while you are away. |

## Updates

How this Legato takes new releases. Usually set in Settings › Updates. See [Updates](/legato/running/updates/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `updates.mode` | string | `ask` | `auto` installs a new release once no agent is in the middle of a turn, and restarts; `ask` offers it; `off` never looks. |

## GitHub

How Legato reads pull requests for your tasks. Legato only reads from GitHub here; it never writes. See [GitHub](/legato/integrations/github/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `github.auth` | string | `auto` | Where the token comes from: `auto` (`github.token` if set, else `gh auth token`), `gh` (only `gh auth token`) or `token` (only `github.token`). |
| `github.token` | string | none | A GitHub token. Use `${VAR}` to keep it out of the file. |
| `github.poll_interval_seconds` | integer | `600` | How often Legato looks for a pull request on a task's branch that has none yet. |
| `github.resolved_poll_interval_seconds` | integer | `600` | How often Legato refreshes pull requests it already knows. |

Press `r` on the TUI's board to sync at once.

## Notifications

Push notifications through ntfy, and desktop notifications. See [Notifications](/legato/integrations/notifications/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `notifications.ntfy.url` | URL | `https://ntfy.sh` | The ntfy server. |
| `notifications.ntfy.topic` | string | none | The topic to publish to. Without it, Legato sends no push notifications. Pick a name nobody could guess. |
| `notifications.ntfy.token` | string | none | An access token for a protected topic. |
| `notifications.approvals` | bool | `true` | Sends a notification for each new approval request. Tapping it opens the request in the Legato app. |
| `notifications.handoff` | bool | `true` | On a main instance, sends a notification when an away task fails or is lost on its worker, and when a worker no longer accepts this main or has been unreachable for 15 minutes. See [Hand off tasks](/legato/using/handoff/#attention-and-notifications). |
| `notifications.os.enabled` | bool | `false` | Shows desktop notifications too, through `notify-send` on Linux and `osascript` on macOS. |

With a topic set, Legato notifies you when an agent finishes, when it needs you, and when every worker of a swarm is idle. Turn notifications off for one agent with `n` in the TUI or the bell in the web UI.

## Repositories

The repositories Legato offers when you start an agent or a workflow. You can add more in the web UI (Settings › Agents › Repositories); those are kept in the database. See [Agents](/legato/using/agents/) and [An agent server](/legato/running/agent-server/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `repositories[].name` | string | none | The name shown in lists. |
| `repositories[].repo` | string | none | The GitHub repository, as `owner/name`. |
| `repositories[].primary_dir` | path | none | Your own checkout of it. With it, the repository is offered as a folder to start agents in. Without it, each job gets a fresh clone from the repository cache. |
| `repositories[].base_branch` | string | none | The branch work starts from, such as `main`. |
| `repositories[].jira_projects` | list of strings | none | Jira project keys whose tasks belong to this repository. Legato uses this to find a task's pull requests. |
| `repositories[].workspaces` | list of strings | none | Workspaces whose tasks belong to this repository, used the same way. The first one is also where tasks that [Argus](/legato/integrations/argus/) sends land. |

A task gets a repository from these lists only when exactly one repository matches it.

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `workflows.repositories[].name` | string | none | Older place for `repositories[].name`. |
| `workflows.repositories[].repo` | string | none | Older place for `repositories[].repo`. |
| `workflows.repositories[].primary_dir` | path | none | Older place for `repositories[].primary_dir`. |
| `workflows.repositories[].base_branch` | string | none | Older place for `repositories[].base_branch`. |
| `workflows.repositories[].jira_projects` | list of strings | none | Older place for `repositories[].jira_projects`. |
| `workflows.repositories[].workspaces` | list of strings | none | Older place for `repositories[].workspaces`. |

Legato reads `workflows.repositories` only when there is no top-level `repositories` list. Move it there.

## Repository cache

Legato's own copy of each GitHub repository, which fresh clones for jobs are made from. Cloning needs a `github` credential that lists the repository. See [An agent server](/legato/running/agent-server/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `repository_cache.enabled` | bool | `false` | Offers **Clone** beside **Folder** in the web UI's spawn dialogs: a fresh clone of a repository, on a branch of its own. Jobs and the API can clone without it. |
| `repository_cache.max_age` | duration | `5m` | How long Legato trusts its copy before a job fetches it again. `0s` fetches for every job. |
| `repository_cache.keep_idle` | duration or days | `7d` | How long a clone that no agent works in is kept, so the next agent can carry on in it. Write a duration (`36h`) or days (`7d`). `0` keeps clones until you remove them. A clone with commits the cache does not have is kept 7 days more. |

## Worktrees

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `worktrees.yggdrasil.enabled` | bool | `false` | Lets Legato make and remove a Git worktree for a task with the `yg` command. It also enables the workflow steps that prepare a pull request's worktree and remove a worktree. See [Yggdrasil](/legato/integrations/yggdrasil/). |

## Resources

Named sets of files and folders you can give an agent when it starts or while it runs, such as a notes vault. You can also save resources in Legato; the ones here are changed only here. See [Resources](/legato/agent-tools/resources/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `resources[].name` | string | none | The resource's name. Names must differ, ignoring case. |
| `resources[].paths[].path` | path | none | A file or folder. Absolute, or starting with `~/`. Each resource needs at least one. |
| `resources[].paths[].access` | string | `read` | `read` or `write`. |

```yaml
resources:
  - name: notes
    paths:
      - path: ~/Documents/notes
        access: write
      - path: ~/Documents/specs
```

## Credentials

Credentials that Legato holds for agents, so that no agent needs a secret in its prompt or environment. An agent asks for a use, your rules decide, and Legato records each request and use. See [The credential broker](/legato/security/credential-broker/) and [Setting up credentials](/legato/security/credentials-setup/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `credentials[].name` | string | none | The credential's name: letters, digits, `.`, `_` and `-`, up to 64 characters. Names must differ, ignoring case. |
| `credentials[].secret` | string | none | Where the secret is, as `<store>://<path>`. The stores are `legato` (Legato's own), `op` (1Password), `systemd-creds`, `keychain` (macOS), `env` and `file`. |
| `credentials[].profile` | string | `secret` | What kind of credential it is: `secret`, `api-key`, `elevenlabs`, `ssh`, `github` or `azure`. A profile of your own in `<config folder>/profiles/` works too. |
| `credentials[].type` | string | the profile's | The secret's type, when the profile takes several: `value`, `userpass`, `keypair`, `oauth-client` or `oauth-refresh`. |
| `credentials[].settings.upstream` | URL | none | `api-key`: the `https` address of the API. Required. |
| `credentials[].settings.key.header` | string | none | `api-key`: the header that carries the key. Give this or `credentials[].settings.key.query`. |
| `credentials[].settings.key.format` | string | `{secret}` | How the header's value is written, such as `Bearer {secret}`. It must contain `{secret}`. |
| `credentials[].settings.key.query` | string | none | `api-key`: the query parameter that carries the key, for an API that takes it there. |
| `credentials[].settings.client_id` | string | none | `github`: your GitHub App's client ID. `azure`: the app (client) ID that `az` signs in as, a GUID. Required for both. |
| `credentials[].settings.repositories` | list of strings | none | `github`: the repositories agents may use, as `owner/name`, or `owner/*` for all of an owner's. Required. |
| `credentials[].settings.tenant_id` | string | none | `azure`: the Microsoft Entra tenant (directory) ID, or its domain, that `az` signs in to. Required. See [Azure](/legato/security/credential-broker/#azure). |

### Rules

`policy` replaces the profile's own rules. Leave it out to use them. For each use, the most specific matching rule decides. A rule scores one point for naming operations and one for each literal character of its resource pattern; on a tie, the earlier rule wins. A use no rule matches is refused.

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `credentials[].policy[].operation` | string or list | any operation | The operations the rule covers, such as `read`, `ssh.login` or `git.push`. |
| `credentials[].policy[].resource` | string | any resource | A pattern of the resources the rule covers. `*` matches within a `/`-separated part and `**` across them. |
| `credentials[].policy[].approve` | string | none (required) | How you are asked: `auto` (never), `session` (once for the agent's session), `window` (once for `window`), `request` (each use) or `deny` (refused). |
| `credentials[].policy[].window` | duration | none | How long a `window` grant lasts, such as `30m`. Required with `approve: window`. |
| `credentials[].policy[].limit` | map | none | A count per period for a meter (`requests`, `characters` or `bytes`), such as `requests: 200/day`. The period is `minute`, `hour`, `day` or `week`. Past the limit, the next rule decides. |
| `credentials[].policy[].signed` | bool | `true` for `request` rules once you enroll a device | Allows a use only by a decision signed on an enrolled device. See [Devices](/legato/running/devices/). |

```yaml
credentials:
  - name: elevenlabs
    secret: legato://elevenlabs-key
    profile: elevenlabs
    policy:
      - { operation: tts.speak, approve: auto, limit: { characters: 20000/day } }
      - { operation: tts.speak, approve: window, window: 30m }
  - name: openai
    secret: op://Legato/OpenAI/credential
    profile: api-key
    settings:
      upstream: https://api.openai.com/v1
      key: { header: Authorization, format: "Bearer {secret}" }
  - name: github
    secret: legato://github-app
    profile: github
    settings:
      client_id: Iv23liAbCdEf
      repositories: [cpave3/*]
```

## Credential sources

1Password vaults whose items are credentials: each SSH Key item a credential of the `ssh` profile, and each API Credential, Password and Login item one of the `secret` profile. A credential of the same name in `credentials` wins. See [Setting up credentials](/legato/security/credentials-setup/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `credential_sources[].vault` | string | none | The vault, as `op://<vault>`. List each vault once. |
| `credential_sources[].profile` | string | both | `ssh` or `secret` takes only those items. Leave it out for both. |
| `credential_sources[].policy[].operation` | string or list | any operation | As `credentials[].policy[].operation`. |
| `credential_sources[].policy[].resource` | string | any resource | As `credentials[].policy[].resource`. |
| `credential_sources[].policy[].approve` | string | none (required) | As `credentials[].policy[].approve`. |
| `credential_sources[].policy[].window` | duration | none | As `credentials[].policy[].window`. |
| `credential_sources[].policy[].limit` | map | none | As `credentials[].policy[].limit`. |
| `credential_sources[].policy[].signed` | bool | as for credentials | As `credentials[].policy[].signed`. |

One policy covers every item of the vault. Each item takes the rules that name an operation of its own profile, or no operation; when none fit, it takes its profile's own rules.

```yaml
credential_sources:
  - vault: op://Legato
    policy:
      - { operation: ssh.login, approve: window, window: 1h }
```

## Secrets

Where Legato keeps the key of its own secret store, and how it reads 1Password.

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `secrets.master_key` | string | `auto` | *File-only.* Where the key of Legato's own store lives: `auto`, `systemd-creds`, `secret-service`, `keychain`, `passphrase` or `file`. `auto` takes the first of them that works. |
| `secrets.onepassword.token_file` | path | none | *File-only.* A file (mode `0600`) that holds a 1Password service account token, for `op://` secrets. Absolute, or starting with `~/`. Prefer this to `secrets.onepassword.token`. |
| `secrets.onepassword.token` | string | none | *File-only.* The service account token itself. Set this or the file, not both. |
| `secrets.onepassword.backend` | string | `sdk` | *File-only.* How Legato reads 1Password: `sdk` (built in, no `op` command needed) or `op` (the `op` command). |

## Credential channels

How agents reach credentials without holding them. See [The credential broker](/legato/security/credential-broker/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `credential_channels.http_proxy.listen` | string | `127.0.0.1:0` | The loopback address and port of Legato's proxy for API keys. Port `0` picks a free one, and Legato keeps the same port across restarts, so running agents keep reaching it. Any address that is not loopback stops Legato at start. |
| `credential_channels.ssh_agent.known_hosts` | path | `~/.ssh/known_hosts` | The file that names SSH hosts by their keys, so rules can name hosts. |
| `credential_channels.gh.isolate_login` | bool | `true` | With a `github` credential, keeps each agent's `gh` from your own GitHub login: it gets Legato's tokens or none. `false` lets an agent's `gh` fall back to your login when Legato gives no token. |

## Approval devices

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `approval_devices.android_app_digests` | list of strings | the digest of the public debug key the app is built with | *File-only.* SHA-256 digests (hex, colons allowed) of the certificates the Legato Android app may be signed with. Only that app's keys may sign approvals. Set the digest of your own release key. See [Devices](/legato/running/devices/). |

## Learning

Legato Learn: lessons from finished work, curated by an agent and checked by you. Settings alone start nothing; `learning.enabled` turns on the automatic part. See [Learn](/legato/using/learn/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `learning.enabled` | bool | `false` | Reflects on a task and curates its lessons when the task enters one of `learning.transition_columns`. The ledger and the manual commands work either way. |
| `learning.prompt_enabled` | bool | `false` | Adds accepted lessons to the prompt of agents Legato starts. |
| `learning.transition_columns` | list of strings | none | Board columns whose entry starts learning. Required when `learning.enabled` is on; each must be a column of `board.columns`. |
| `learning.curator_adapter` | string | `swarm.worker.agent` | The tool the curator agent runs. It must have an entry under `adapters`. |
| `learning.curator_tier` | string | none | A tier of that tool for the curator. It must exist under `adapters.<tool>.tiers`. |
| `learning.curator_working_dir` | path | the task's checkout | An absolute folder the curator always works in. |
| `learning.packet_max_items` | integer | `100` | The most items in what the curator is given. |
| `learning.packet_max_bytes` | integer | `262144` (256 KiB) | The most bytes in what the curator is given. |
| `learning.prompt_max_items` | integer | `20` | The most lessons added to an agent's prompt. |
| `learning.prompt_max_bytes` | integer | `16384` (16 KiB) | The most bytes of lessons added to an agent's prompt. |
| `learning.reflection_timeout` | duration | `5m` | Not used yet. It must still be a positive duration. |
| `learning.reflection_retries` | integer | `3` | Not used yet. It must still be positive. |
| `learning.consolidation_automatic` | bool | `false` | Not used yet. Run consolidation by hand. |
| `learning.consolidation_threshold` | integer | `50` | Not used yet. It must still be positive. |

All limits must be positive.

### Semantic search

With embeddings on, `legato learn search` finds lessons by meaning as well as by words.

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `learning.embedding.enabled` | bool | `false` | Turns semantic search on. |
| `learning.embedding.provider` | string | `openai-compatible` | The kind of API. `openai-compatible` is the only one. |
| `learning.embedding.model` | string | none | The embedding model. Required when enabled. |
| `learning.embedding.base_url` | URL | `https://api.openai.com/v1` | The API's address. |
| `learning.embedding.api_key_env` | string | `OPENAI_API_KEY` | The environment variable that holds the API key. A local server may need none. |
| `learning.embedding.timeout` | duration | `30s` | How long one request may take. |
| `learning.embedding.dimensions` | integer | `0` | The vector size to ask for. `0` takes what the model gives. |
| `learning.embedding.batch_size` | integer | `32` | Not used yet. It must still be positive. |

## Engram

Whether agents can be started as engrams. Usually set in Settings. See [Engrams](/legato/agent-tools/engrams/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `engram.enabled` | string | `auto` | `auto` offers engrams when the engram command answers; `on` does too, and logs an error when it does not; `off` hides everything about engrams. |
| `engram.command` | string | none | The engram binary. Without it, `engram` is looked up on the `PATH` of the Legato server. |

## Manticore

Reports the tokens each Claude Code session uses to a Manticore instance, and shows today's total in the TUI. Set both settings or neither. See [Manticore](/legato/integrations/manticore/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `manticore.url` | URL | none | The instance's `http` or `https` address. |
| `manticore.api_key` | string | none | The key of the Manticore client the usage counts against. Each Claude Code session gets it in its environment, so make a client for this purpose. |

## Integrations

### Argus

Argus sends cases to Legato's agents and receives their replies. Without `integrations.argus`, the integration is off. See [Argus](/legato/integrations/argus/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `integrations.argus.url` | URL | none | Argus's address. Required. It must be `https`, or `http` to this machine. |
| `integrations.argus.delivery` | string | `auto` | `auto` hands each case to an agent at once; `ask` waits for your approval. |
| `integrations.argus.agent` | string | `claude-code` | The tool an agent started for a case runs: `claude-code`, `codex` or `chimera`. |

```yaml
integrations:
  argus:
    url: https://argus.example.net
    delivery: ask
```

## Review passes

On a worker instance, a reviewer on another model reviews the pull requests of each make-a-change handoff when its author finishes, and the author works through the findings until a pass is clean. Settings alone start nothing; `review_passes.enabled` turns it on. The main instance can change the reviewer, the skill and the number of passes for one handoff in the Hand off dialog. See [Handing off a task](/legato/using/handoff/).

| Setting | Type | Default | What it does |
| --- | --- | --- | --- |
| `review_passes.enabled` | bool | `false` | Runs review passes on this worker's make-a-change handoffs, and tells main instances it does. |
| `review_passes.reviewer.agent` | string | none | The reviewer's tool: `claude-code`, `codex` or `chimera`. Required when enabled. Choose a different model from the author's; Legato does not check. |
| `review_passes.reviewer.model` | string | the tool's | The reviewer's model. Set this or `review_passes.reviewer.tier`, not both. |
| `review_passes.reviewer.tier` | string | none | A tier of `adapters.<name>.tiers` for the reviewer. |
| `review_passes.reviewer.skill` | string | none | The review skill the reviewer is told to use, such as `3-tier-review`. |
| `review_passes.github_credential` | string | none | The credential of the reviewer's GitHub App, on the `github-reviewer` profile, which no agent is given; like a `github` credential it needs `settings.client_id` and `settings.repositories`. Legato posts reviews and polls pull requests with it. Required when enabled. |
| `review_passes.max_passes` | number | `3` | Passes before the review is stuck and the main instance is asked. |
| `review_passes.pass_timeout` | duration | `45m` | How long a reviewer has for one pass. |
| `review_passes.max_concurrent` | number | `2` | Reviewers that run at once on this worker; others wait. |
| `review_passes.review_column` | string | `Review` | The column a card is in while it is reviewed. It must be a column of `board.columns`. |
| `review_passes.poll_interval` | duration | `60s` | How often the pull requests are checked for new comments. |
| `review_passes.quiet_time` | duration | `2m` | How long new feedback waits for more before the author is told. |
| `review_passes.counted_bots` | list of strings | none | Bot logins whose comments count as feedback, as the repository's owners, members and collaborators do. |
| `review_passes.required_checks` | list of strings | none | The checks that must pass. Empty means the branch protection's required checks. |

```yaml
review_passes:
  enabled: true
  reviewer: {agent: codex, model: gpt-5.5, skill: 3-tier-review}
  github_credential: reviewer-app
  counted_bots: ["cubic-dev-ai[bot]"]
```
