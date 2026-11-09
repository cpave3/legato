# The Reeve

The Reeve is the agent of a Legato instance itself (see `GLOSSARY.md`): the person talks to it to run and configure that instance, not to work on a task. Spec: Burnlite milestone `LEG-106`. Site page: `using/reeve`.

## Sessions

- **A session is a hidden task** of kind `reeve` (`tasks.kind`, migration 101; `store.CreateReeveTask`, ephemeral, so it is off the board and out of every task listing). `reeve_sessions` holds each session's `state` (`running`, `suspended`, `ended`), `agent_kind`, `model`, `launch_args` (JSON), its API token and the token's hash, `started_by`, `created_at`, `updated_at`, `active_at` (for the idle suspend) and `ended_at`.
- **One running session per instance.** `ReeveService.Start` refuses with `ErrReeveRunning` while a session is `running` or `suspended` (`store.CurrentReeveSession`). `Resume` of another session is refused the same way.
- **Starting** (`ReeveService.Start`, `internal/service/reeve.go`): makes the task, a new API token, and spawns the agent through the agent service with the `reeve.*` settings read now (`ReeveDefaults`: `reeve.agent`, `reeve.model`, `reeve.launch_args`), overridden for one session by `ReeveStart.Agent` and `Model` (the Reeve's own start button; "Ask the Reeve" always uses the defaults). `AgentSpawnOptions.LaunchArgs` adds the launch arguments after the adapter's own. The brief, when there is one, is the launch prompt (the full prompt comes with R2).
- **Ending** (`End`): stops the agent, and `SetReeveSessionState(ended)` clears the API token and its hash, so it stops working at once. The broker token is the execution's, revoked when the agent ends, as for every agent.
- **Resuming** (`Resume`): a new API token (for an ended session), the session's tool and model unless overridden, and the spawn with `SessionPolicy: resume`, so the conversation comes back.
- **Deleting** (`Delete`): only an ended session (`ErrReeveNotEnded`); removes its row and its task.

## Where it runs

- **Always in Legato's sandbox.** `SpawnAgent` refuses a `reeve` task with `Unsandboxed` (Run without sandbox) or when the sandbox is off (`ErrReeveSandboxed`); `RestartAgentSwitchingSandbox` (the TUI's `U`, the web UI's `switch_sandbox`) refuses it too. Swarm and workflow agents were already pinned.
- **The Reeve's folder**: `$XDG_STATE_HOME/legato/reeve`, else `~/.local/state/legato/reeve` (`service.ReeveFolder`), given to the session read-write as a resource, with the working directory `<folder>/work` inside it. It is outside any repository and outside the data folder, which the sandbox refuses to show. It will hold the Reeve's engram store (`<folder>/engram`) once engram sessions exist.
- `LEGATO_REEVE=1` is in the session's environment.

## Tokens and what the Reeve may call

- **An API token** `lgr_<48 hex>` per session (`ReeveTokenPrefix`), kept with its SHA-256 in `reeve_sessions` and resolved by `ReeveService.Authenticate` (an ended session's token is gone). `serveReeveToken` (`internal/server/reeve.go`), reached from the auth middleware for a Bearer token with that prefix, classes the request (`ClassifyRequest`, see [Web UI](web-ui.md) § Route classes): **read** passes, with the session in the request's context (`ReeveCaller`); **never** is refused (`403`, code `never`); **change** and **risky** are refused (`403`, code `needs_approval`): the Reeve proposes changes as change sets, which Legato runs itself.
- No other session's token reaches the Reeve's routes; the web token does (the person).

## API and web

- `GET /api/reeve` (`{current, history, folder}`), `POST /api/reeve/sessions` (`{agent?, model?, brief?}`; `201`; `409` while one runs, or with the sandbox off), `POST /api/reeve/sessions/{task_id}/end`, `POST /api/reeve/sessions/{task_id}/resume` (`{agent?, model?}`), `DELETE /api/reeve/sessions/{task_id}` (`409` unless ended). Each change sends `agents_changed`.
- Settings › Reeve (`ReeveSettings.tsx`): the current session with **End session**, or **Start the Reeve** with optional agent tool and model for this session; the history with **Resume** (disabled while one runs) and **Delete**; and the `reeve.*` settings as registry rows.
