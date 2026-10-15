# Resources

A **resource** is a named set of files and folders the person gives agents often: an Obsidian vault, a folder of specs, a templates directory. Each path is given to **read** or to **write**. Instead of typing a vault's path into every session, the person attaches the resource when they start an agent, or adds it to one that is running.

Resources are not the board's **workspaces**, which group tasks.

## Defining them

In the configuration:

```yaml
resources:
  - name: Obsidian vault
    paths:
      - path: ~/Documents/My Vault   # spaces are fine
        access: write                # read (the default) or write
      - path: /srv/templates
```

- A name is required and unique, ignoring case. Every resource needs at least one path.
- A path is absolute or starts with `~/`; `~` is expanded when the configuration is loaded. Anything else fails config loading.
- These are listed everywhere but changed only in the file; Legato marks them `config.yaml`.

Or from Legato, where they are kept in the database (migration 068: `resources`, `resource_paths`; see [Database](database.md) § Resources):

- **Web:** Settings › Resources. Create, edit and remove, with a Read/Write choice per path.
- **CLI:** `legato resource list [--json]`, `legato resource add <name> <path>... [--write]` (creates the resource, or adds paths to a saved one; `--write` makes the given paths writable) and `legato resource remove <name>`.

Saving checks that each path exists. A saved resource cannot take the name of a configured one; if one already did (the name was added to the file later), the configured one wins and the saved one is not listed.

## What an agent gets

| Agent | At launch | Added while it runs |
| --- | --- | --- |
| Chimera | `--add-read-path` / `--add-write-path`, one per path, shell-quoted | `POST /v1/sessions/:id/paths` for each path |
| Claude Code, Codex | Nothing on the command line | The bridge accepts the grant and leaves access to the tool |
| Every agent | A `## Resources` section in the launch prompt: each resource, its paths, and `read only` or `read and write` | The same text after a `[legato resources]` tag, appended to the conversation: a busy agent reads it at its next step, and an idle Chimera takes it as a message and answers it. The web UI shows it as Legato's own message |

- A path that does not exist when the agent starts or the resource is added (a disk not mounted, a folder moved) is left out, and a resource left with none is skipped. Chimera exits at launch when a path it is given is missing.
- A path that one resource gives to read and another to write is written. Each path is granted once.
- A resumed session (`session_policy: resume`) gets the command-line grants but no launch prompt, as for every resumed session; Chimera keeps the paths of the session it resumes anyway.
- Claude Code and Codex get the paths as text only. A Claude Code session asks before it reads a file outside its working directory, as it would for any path the person typed.

Nothing can be taken back from a running agent: Chimera has no call that narrows what a session may touch. The resources given to an execution are recorded in `execution_resources`, by name, so the web control can mark them.

## Picking them when an agent starts

- **Web:** the spawn dialog (`SpawnAgentDialog`, for both **Spawn agent** and a task's **Open agent**) lists the resources as boxes to tick under "Where", when there are any. The choice is sent as `resources: [names]` on `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent`; a name the server does not know is a `400` and no agent is started.
- **TUI:** the spawn overlay has a **resources** line between **agent** and **where**, when there are any. `h`/`l` move, `space` ticks; resources that give any path to write are marked `✎`.

Checked on 2026-10-14 against Chimera with a scratch Legato: a resource whose path has a space was granted at launch (`GET /v1/sessions/:id/paths` listed it as writable) and the agent wrote a file in it with its own write tool; a second resource added to the running session was granted to read, and the agent answered the note.

## Adding one to a running agent

`POST /api/agents/{task_id}/resources {"names": ["Obsidian vault"]}` gives the agent the named resources and answers `{"attached": [...]}` with everything it now has; `GET` on the same address answers the same list. It needs the agent's native endpoint (a Chimera session, or Claude Code or Codex through its bridge); a session driven only through its terminal gets `409`.

In the web UI this is the **Resources** button in the native panel's status bar, beside the model selector. It lists every resource with its paths, marks the ones the agent already has, and attaches one on a tap. Below the `sm` breakpoint it is a sheet from the bottom with rows sized for a thumb; above it, a list over the button.

## Paths with spaces

Every route keeps a path with spaces as one path: the configuration and the API take paths as values, Chimera's flags are shell-quoted, and the launch prompt writes each path in backticks. In a message, Chimera's TUI and Legato's native composer read `@path` and `#path` with a path in double quotes (`@"My Vault/today.md"`) or with escaped spaces (`@My\ Vault/today.md`); Chimera's file list quotes a path it completes, and `/add-read-path`, `/add-write-path` take the rest of the line as the path.

## Packages

- `config/resources.go` — the `resources:` block and its checks.
- `internal/engine/store/resource.go` — saved resources and the resources of an execution.
- `internal/service/resource.go` — `ResourceService`: list, save, remove, resolve by name, attach; the launch prompt and grants. `agent_resources.go` — `AttachResources` on the agent service and `GrantPaths` on the native interaction.
- `internal/engine/hooks/chimera_paths.go` — Chimera's flags (`service.PathGrantingAdapter`).
- `cmd/legato/resource_contract.go`, `resource_runtime.go` — the CLI and the service's construction.
- `web/src/lib/resources.ts`, `components/ResourcesButton.tsx`, `components/ResourcesSettings.tsx`.
