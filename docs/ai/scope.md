# Legato Scope

Legato Scope lets the operator comment on a live web page, usually the dev server of an app an agent is building. In the Legato Chrome extension the operator turns on pin mode, clicks an element and writes what is wrong. Each comment is a **pin**. Pins collect as drafts in the extension; **Send** puts them into Legato's **pool** as one **batch**. The agent whose task slot subscribes to the page's origin gets the batch as one `[legato scope]` message, reads each pin's context through its Legato tools, fixes what it can, and marks each pin fixed or won't fix. The extension shows those states on the page. (Terms: `GLOSSARY.md` § Legato Scope. Decisions: Burnlite LEG-281 to LEG-288.)

This page covers Legato's side: storage, `ScopeService`, routing and delivery. The HTTP API, the agent tools and the extension each have their own section below as they land.

## Storage

Migration `122_scope.sql`:

| Table | Holds |
| --- | --- |
| `scope_batches` | One row per press of Send: `origin`, `created_at`, `delivered_at` (first time its agent was told), `delivery_error` (why the last push failed, cleared by a later delivery). |
| `scope_pins` | One row per pin: `batch_id` (cascade), `origin`, `seq` (order in the batch), `url`, `path` (path and query, for the message line), `comment`, `state`, `note` (the agent's one line), `selector` (the best candidate), `context_json` (page, target and styles, as the extension captured them), `source_json` (component source, nullable), `screenshot` (a file name), `resolved_by_task`, times. |
| `scope_subscriptions` | `origin` (primary key: one subscriber per origin), `task_id` (cascade on task delete), `slot`. |

Screenshots are files under `<data_dir>/scope/` (`scopeDir`, from `config.ResolveDataDir`), named `<pin id>.<png|jpg|webp>`, never in the database. Withdrawing a pin removes its file.

Store methods are in `internal/engine/store/scope.go`. State changes are conditional updates: a change the pin's state does not allow is `store.ErrConflict`, which `ScopeService` turns into `ErrScopePinState`.

## Pin states

`draft` (extension only) → `sent` → `delivered` → `fixed` | `wont_fix`.

- **sent**: in the pool, no agent told yet. Only a sent pin can be **withdrawn** (the row and its screenshot are deleted; an empty batch goes too).
- **delivered**: an agent was told of it: a push succeeded, or the pin was in the result of `Subscribe`, or the agent listed it on one of its own origins (`ListForSlot`).
- **fixed** / **wont_fix**: `Resolve` with a one-line note (line breaks collapsed, at most 500 characters), from `sent` or `delivered`.
- **Reopen** (operator): from `fixed` or `wont_fix` with a new comment, back to `sent` (note, resolver and delivery cleared), then pushed again.

Pins have no threads.

## Routing

- `Subscribe(task, slot, origin)`: the newest subscriber takes the origin. The slot that loses it gets a `[legato scope]` notice through `QueueMessage` (best effort). Pins waiting on the origin are returned (`ScopeSubscribeResult.Waiting`, `Report()` is the text for the agent) and marked delivered; nothing is pushed.
- `Unsubscribe(task, slot, origin)`: only the slot's own subscription; `store.ErrNotFound` otherwise.
- Origins are normalized by `NormalizeScopeOrigin`: `http` or `https`, a host, an optional port, lower case, nothing else (a trailing `/` is accepted). Anything else is `ErrScopeInvalidOrigin`.
- A subscription belongs to the **task slot**, not the execution, so it survives Reload, restart and resuming an interrupted agent. It ends (`endIfGone`) when the slot has neither a running nor an interrupted agent session, or the task is archived; a deleted task's rows go by cascade. This is checked:
  - on `EventAgentDied` for that task and slot (killed, exited, archived or deleted tasks stop their agents with an announcement; a restart stops without one, so it never fires there);
  - on every change to the interrupted agents (`AddInterruptedListener`), which covers a dismissal;
  - on `EventCardsRefreshed`, which archiving publishes: archiving stops the task's agents, but an interrupted one goes without an `EventAgentDied` or an interrupted change;
  - at start (`PruneEnded` in `StartEventLoop`).
  The reconcile path publishes `EventAgentDied` for an agent it marks interrupted too, which is why the handler looks at the slot's sessions (`store.ScopeSlotStatus`) rather than trusting the event.
- Pools and subscriptions belong to one Legato instance. Nothing is relayed to worker instances.

## Delivery

- `SendBatch(origin, pins)` checks the limits (`ScopeMaxPinsPerBatch` 20, `ScopeMaxScreenshotBytes` 500 KB, a non-blank comment of at most 4000 characters, every pin's page on the batch's origin, a screenshot that sniffs as PNG, JPEG or WebP), writes the screenshots, stores the batch, then pushes.
- A push goes to the subscribing slot only while it has a **running** agent (`ScopeSlotStatus`). It sends every `sent` pin on the origin (so a batch whose earlier push failed rides along with the next) as **one** message through `AgentService.QueueMessage`, addressed with `WithAgentSlot`. The message opens with `[legato scope]` and has one line per pin: ID, path, the comment in quotes (one line, cut at 200 characters), `at <best selector>`, and `(file:line)` when the component source is known; it ends by naming `page_pin_get` and `page_pin_resolve`.
- A failed push leaves the pins `sent` and records `delivery_error` on their batches. With no subscriber, or no running agent in the slot, the batch simply waits.
- Waiting pins reach the agent on the next subscribe (in the result), list (`ListForSlot` on the slot's own origins) or start of the slot's agent (`EventAgentStarted`, which covers resume, Reload and restart: they are pushed).
- `SendBatch` answers `subscribed` (a slot holds the origin) and `delivered` (this batch reached a running agent), so the extension can say when pins will wait.
- Pushing, subscribing, listing for a slot and withdrawing hold `ScopeService.mu`, so a pin is pushed once per process, and a withdrawal waits for a push in flight (which may be telling the agent of that pin) and then finds the pin delivered.

`EventScopeChanged` (`events.ScopeChangedPayload{Origin}`) is published whenever an origin's pins or subscription change: a batch, a delivery, a resolve, a reopen, a withdraw, a subscribe or unsubscribe, a subscription ended.

## HTTP API

`internal/server/scope.go`, behind the bearer token like every `/api/` route, wired into both web servers by `approvalRuntime.attach` (`SetScopeService`, `StartScopeEvents`). The extension's service worker is its only client.

| Route | Does |
| --- | --- |
| `POST /api/scope/batches` | Send. Body `{origin, pins}`; a pin is `{comment, page: {url, title, viewport: {width, height}, scroll: {x, y}, dpr}, target: {selectors, text, box: {x, y, width, height}}, styles: {computed, classes, inline, parent}, source?: {name, file, line, column, owners, confidence}, screenshot?: "data:image/…;base64,…"}`. The body is read up to 16 MB (`scopeMaxBodyBytes`). `201 {batch_id, pin_ids, subscribed, delivered}`. |
| `GET /api/scope/pins?origin=` | `{pins}`: `ScopePin` (`id`, `batch_id`, `origin`, `url`, `path`, `comment`, `state`, `note`, `selector`, `selectors`, `text`, `box`, `has_screenshot`, `location`, `created_at`, `updated_at`). Withdrawn pins are gone. |
| `POST /api/scope/pins/{id}/reopen` | `{comment}`; the pin, back to `sent` (and pushed again). |
| `POST /api/scope/pins/{id}/withdraw` | `204`. |
| `GET /api/scope/pins/{id}/screenshot` | The image, with `nosniff` and a sandboxing CSP; `404` when the pin has none. |

Errors: `400` (`ErrScopeInvalidOrigin`, `ErrScopeInvalid`, a screenshot that is not a base64 image `data:` URL, malformed JSON), `404` (unknown pin), `409` (`ErrScopePinState`), `413` (`ErrScopeTooLarge`, or the body limit), `503` (no service).

`EventScopeChanged` reaches `/ws` clients as `{"type": "scope_changed", "origin": "http://localhost:5173"}`.

## Agent tools

`internal/cli/mcp_scope.go`, offered when `MCPOptions.Scope` is set; see [Agent tools](agent-mcp.md) for the table. Each call is bound to the calling session (`m.session()`); subscriptions use its task and slot. A swarm worker is refused (its identity is a sub-task, which has no task row). Errors come back as the call's result, so an unknown pin is a message naming it, never a crash.

- `page_pin_subscribe {origin}` → `ScopeSubscribeResult.Report()`.
- `page_pin_unsubscribe {origin}`.
- `page_pin_list {origin?, state?}` → one line per pin, grouped by origin, with state and note.
- `page_pin_get {pin_id}` → `ScopePinDetail.Text()` and the screenshot as image content.
- `page_pin_resolve {pin_id, outcome, note}`; `resolved_by_task` is the caller's task.

Host: `hostBrokerDeps` gets the host's `ScopeService`. The broker's `mcp.call` returns a tool's `images`, which `MCPRelay` already read, so a sandboxed agent sees the screenshot. Without a broker, `legato mcp serve` builds `standaloneScope`: its own `ScopeService` over the store, with the agent service as messenger and `Notify` broadcasting `scope_changed` over IPC (`Content` is the origin), which both host runtimes publish as `EventScopeChanged`.

`cli.MCPGuidance` tells every agent to subscribe the origin of a dev server it starts or knows of, read pins with `page_pin_get`, resolve each, and use `page_pin_list` when told pins wait. Claude Code reads it as the server's instructions; Codex and Chimera get it in the launch prompt (`SetToolsPrompt`). `MCPToolNames` names the five tools, so Chimera runs them without asking.

## Wiring

`startScope` (`cmd/legato/scope_runtime.go`) builds the one `ScopeService` of a host process (the TUI's and `legato serve`'s, from `startApprovals`), with the agent service as its messenger, starts its event loop and adds the interrupted listener. It is kept on `approvalRuntime.scope`.

`ScopePinDetail.Text()` is the full context as text for an agent: page, selectors, text, box, component source and owners, styles that differ from the defaults, the parent's layout.
