# Subscription usage

How much of the Claude, Codex and OpenCode Go subscriptions' usage limits the operator has used (LEG-659). It is not `service.Usage` / `UsageService`, which are Manticore's token counts (see [TUI](tui.md) § Token usage). Site page: `using/usage`. Usage profiles and rules (LEG-660) switch defaults for new work, without changing the provider readers or accounts.

## Providers

`config/usage_providers.go` is the one table that declares the providers: their stable id, the name the operator knows them by, and the meters they report, each with its short and long label. `IsUsageMeter` (qualified rule and pin meters, `provider.window`), the service's `UsageProviderName` / `UsageMeterLabel` / `UsageMeterShort`, the TUI's row labels and the web's catalog all derive from it. The six Claude/Codex qualified meter ids are unchanged; OpenCode Go adds `opencode.five_hour`, `opencode.weekly` and `opencode.monthly`. Adding a provider means a table row, an engine reader package, a service adapter (and its error mapping), one wiring line in `usageReaders`, and settings rows; the polling, cache, backoff, ownership, policy and API surfaces are provider-generic. A future account axis should compose with the reader identity (`SubscriptionUsageReader.Provider()` keys the poll schedule, the cache row and the API row), not multiply this table.

## Sources

Claude's endpoint is undocumented; Codex's is an app-server call; OpenCode Go's is documented. Every answer the readers do not understand is an error with a reason, never a guess, and a test pins each response shape relied on.

### Claude: `internal/engine/claudeusage`

- `GET https://api.anthropic.com/api/oauth/usage`, with the headers `Authorization: Bearer <claudeAiOauth.accessToken>` and `anthropic-beta: oauth-2025-04-20`. This is what Claude Code 2.1.332's `/usage` view calls, found in its binary's strings.
- The answer has the windows `five_hour`, `seven_day`, `seven_day_opus` and `seven_day_sonnet`. Each is `{utilization 0–100, resets_at ISO-8601}` or `null`. Other fields (`seven_day_oauth_apps`, `extra_usage`, and new ones) are ignored.
- The answer is decoded leniently:
  - no known window at all → `ErrEndpointChanged`;
  - every known window `null` → `ErrNoSubscription`;
  - a window of another shape → `ErrEndpointChanged`.
- Status codes: 401 → `ErrLoginExpired`, 403 → `ErrNoSubscription`, 404 or 410 → `ErrEndpointChanged`, 429 → `*RateLimitedError{RetryAfter}` (seconds or an HTTP date).
- **The login** (`CredentialSource`; `HostCredentials` per OS):
  - On Linux it is `$CLAUDE_CONFIG_DIR/.credentials.json`, else `~/.claude/.credentials.json` (`FileCredentials`, `CredentialsPath`).
  - On macOS it is the Keychain item `Claude Code-credentials` (`security find-generic-password -w`), else that file.
  - It is read fresh on every call and is **never refreshed or written**: a refresh rotates the refresh token and would sign Claude Code out.
  - An `expiresAt` in the past is `ErrLoginExpired` with no request made.
- **Token safety:**
  - Transport errors are replaced by fixed text, because the standard library quotes the URL.
  - Neither a body nor a header reaches an error.
  - The default HTTP client ignores the environment's proxies.
  - Tests assert a sentinel token never appears in an error and that the credentials file's bytes and mtime are unchanged.

### Codex: `internal/engine/codexusage`

- **`AppServer.Read`** starts `codex app-server` (stdio, newline-delimited JSON-RPC without `jsonrpc`, in its own process group) for one read.
  - It sends `initialize`, waits for the result, then sends `initialized` and `account/rateLimits/read` (openai/codex PR #5302). Notifications in between are skipped.
  - It stops the server after the read. The read is bounded by `Timeout` (10 s), after which the group is killed.
  - The reply's shape is `{rateLimits: {primary, secondary, credits?, planType?}}`, each window `{usedPercent, windowDurationMins, resetsAt (epoch s)}`.
  - Error mapping:
    - `-32601`, "method not found" or "unknown variant" → `ErrUnsupported`;
    - an auth or login message → `ErrNotSignedIn`;
    - no window → `ErrNoData`;
    - no `codex` command → `ErrCodexMissing`.
- **`ReadLogs(root)`** reads the session logs.
  - It reads the last `rate_limits` of `event_msg`/`token_count` lines (snake_case: `used_percent`, `window_minutes`, `resets_at`, `credits.has_credits`) in the 10 most recently written `rollout-*.jsonl` files of every day folder under `sessions/YYYY/MM/DD` (each folder listed and its files stat'ed, never opened), reading the last 512 KiB of each, and takes the newest event among them: a resumed session keeps writing to its first day's folder.
  - `AsOf` is the event's `timestamp`.
  - `SessionsDir` is `$CODEX_HOME/sessions`, else `~/.codex/sessions`.
- **`Classify(minutes)`** names a window by its length:
  - up to 360 minutes → `five_hour`;
  - a week less an hour or more → `weekly`;
  - anything else → dropped.
  - Primary and secondary are not fixed lengths: the ticket's sample had a weekly primary and no secondary.
- Both packages open files in folders a sandboxed agent can write (`.claude`, `.codex`) without blocking, and read them only when the opened descriptor is a regular file (`openRegular`), so a FIFO cannot hang the host's poll or its shutdown.
- Chimera's Codex models are assumed to use the same ChatGPT account as the Codex CLI (the Reeve's decision, LEG-659). If they do not, a Chimera-side source is needed.

### OpenCode Go: `internal/engine/opencodeusage`

- `GET https://opencode.ai/inference/go/v1/usage`, the endpoint OpenCode documents for its clients, with `Authorization: Bearer <the Go API key>`. The reading is the account's: it counts every client that uses the key, so usage consumed elsewhere (by Manticore, by another harness) appears on the next read.
- The answer has one meter per limit — `rolling` (the 5-hour window), `weekly`, `monthly` — each `{status: ok|rate-limited, percent: 0–100, resetsAt: ISO-8601}`. `resetsAt` is surfaced as the endpoint gives it; Legato never reconstructs a reset from the advertised schedule. Zero percent is a real reading; a meter that is missing or null is left out, not invented.
- The answer is decoded strictly: an unknown status, a percent that is not a finite 0–100, a limit used up below 100, or a missing reset time is `ErrEndpointChanged`, never a guess.
- Error mapping: 401 → `invalid_key` (the reading is dropped, as an expired login's is: it may be another account's), 403 → `no_subscription` (the key's workspace has no Go subscription), 429 → `*RateLimitedError{RetryAfter}`.
- **The key** is a Secret setting, `usage.opencode.api_key`, naming a secret in Legato's sealed store (`legato://opencode-go`, stored with `legato secret add`) or 1Password; the settings machinery resolves it into the process at start, like `manticore.api_key`. Replacing the key's value in the secret store applies at restart, which the setting's help says. `usage.opencode.enabled` is **off by default** (the provider is opt-in: it needs a key and asks an external service). No key means `missing_key`, and **no request is made**. No error, log, API answer or export carries the key; the engine replaces transport errors with fixed text, does not follow redirects (the origin is fixed), ignores the environment's proxies and bounds the answer.
- The opencode reader needs no OpenCode inference on Legato: it is a subscription reader only, separate from any inference adapter.

## The service: `internal/service/subscription_usage.go`

- **`SubscriptionUsageReader`** (`Provider`, `Read`) is one provider.
  - `ClaudeUsageReader`, `CodexUsageReader` and `OpenCodeUsageReader` (`subscription_usage_readers.go`) map the engine's windows to meters: `five_hour`, `weekly`, `weekly_opus`, `weekly_sonnet`, and for OpenCode Go `monthly` (its rolling window maps to `five_hour`).
  - They map the engine's errors to `*UsageUnavailableError{Reason, RetryAfter}`. The reasons are `not_signed_in`, `login_expired`, `no_subscription`, `missing_key`, `invalid_key`, `codex_missing`, `no_data`, `endpoint_changed`, `rate_limited`, `unreachable` and `error`. `not_read` means never read.
  - `CodexUsageReader` asks the app server first and falls back to the logs for any failure but `ErrNotSignedIn`.
  - `UsageProviderName`, `UsageMeterLabel` and `UsageMeterShort` come from the config provider table; the web keeps its reason words of its own in `web/src/lib/usage.ts`.
- **`Snapshot`** also carries **`Catalog`** (`service.UsageCatalog`, from the config provider table): each provider's id, name and meters with `window`, `qualified`, `short` and `long`. `/api/usage` serves it beside the readings; the web labels providers and meters from it, and Settings feeds the usage rule form's meter choices from the same fetch. Without a usage service the route answers the catalog with no providers.
- **`SubscriptionUsageService`** reads providers and keeps their readings.
  - `Poll` reads each provider whose next read is due, every `PollInterval` (`usage.poll_interval`, 5m, at least 2m).
  - `Refresh` reads now, but at most once a minute per provider and never during a back-off.
  - `RequestRefresh` refreshes in the background in the process that polls; elsewhere it calls `Forward`.
  - `Run` polls once a minute for whatever is due.
  - One read per provider is in flight at a time.
  - The first `Poll` or `Refresh` in a process takes each provider's last and next read from its kept reading's `checked_at` (`seed`), so a restarted Legato (a hot reload, an update) does not ask every provider again at once, and the once-a-minute refresh limit holds across processes, such as successive `legato usage --refresh` runs. A back-off is not kept across a restart.
- **Back-off:** after `rate_limited`, `endpoint_changed`, `unreachable` or `error`, the next read waits `interval << failures`, capped at 30 minutes, or the provider's `Retry-After`. The other reasons keep the normal interval, since their checks are local and cheap (no request is made without a live login).
- **Kept state:**
  - Each provider's newest reading is kept as JSON in `subscription_usage` (migration 156; `store.PutSubscriptionUsage`, `ListSubscriptionUsage`). That table is the cache every process serves.
  - After `endpoint_changed`, `rate_limited`, `unreachable` or `error`, the previous windows are kept with the new reason.
  - After the other reasons, they are dropped.
- **`Snapshot`** reads the cache and **never a provider**. It judges each reading:
  - `unavailable` without windows;
  - `stale` when it has a reason, or is older than two poll intervals;
  - `ok` otherwise.
  - `AsOf` is the read's time, or the log event's time.
- `UsageReasonText`, `UsageProviderName` and `UsageMeterLabel` are the TUI's and CLI's words. The web UI keeps its own words in `web/src/lib/usage.ts`.

## Runtime: `cmd/legato/usage_runtime.go`

- `usageReaders` follows `usage.claude.enabled` and `usage.codex.enabled` (both on when unset) and `usage.opencode.enabled` (off when unset). OpenCode Go's reader is built with the key the settings resolved into the process; a config.yaml pin that still names a secret (`legato://`/`op://`) resolves through the sealed store once at wiring, and a secret that cannot be read is no key (`missing_key`), never an error that names it.
- `subscriptionUsageFor` builds the service and attaches the usage policy on a store, even with both readers off: policy state and pin controls remain available without provider readings.
- `startSubscriptionUsage` runs the poller in the one process of a data folder that holds `<data_dir>/usage.lock` (flock, like `mqtt.lock`). A process that cannot take the lock serves the cache and tries again every `usageClaimRetry` (a minute), so it takes the poller over when the holder ends (`awaitUsagePoller`).
- A kept reading publishes `EventSubscriptionUsageChanged` and broadcasts the IPC message `usage_changed`. Other processes publish `EventSubscriptionUsageInvalidated` for it. Both reach the web as the WebSocket message `usage_changed`.
- A refresh asked in a process that does not poll broadcasts `usage_refresh`. That becomes `EventSubscriptionUsageRefreshRequested`, which only the poller listens to.
- Serve and the TUI wire it. `legato usage` (`usage_cmd.go`) reads the cache.
  - `--refresh` reads in the command itself when it can take `usage.lock`; otherwise it broadcasts `usage_refresh` and waits 3 s.
  - It is forwarded from the sandbox like `doctor`, so the logins are only read on the host. The Reeve's class for reading is `read`; `usage pin` and `usage unpin` are `change`.

## Profiles, rules and pins

- `usage.profiles` entries are `{name, agent, tier?, reviewer?: {agent, tier?}}`. Agents are `claude-code`, `codex` or `chimera`. Both selections use configured `adapters.<agent>.tiers`, never raw models; `model` is rejected. Names are unique ignoring case.
- `usage.rules` entries are `{name, order, meter, at_least, profile}`. `order` must be explicit: lower first, ties by name. `at_least` is 1–100 inclusive. The qualified meters are those the provider table declares: `claude.five_hour`, `claude.weekly`, `claude.weekly_opus`, `claude.weekly_sonnet`, `codex.five_hour`, `codex.weekly`, `opencode.five_hour`, `opencode.weekly` and `opencode.monthly`. Unknown agents, tiers, meters or profile references are refused on save.
- `usage.fallback_profile` names the profile when no rule matches; empty means the usual configured defaults. Profiles, rules and fallback are live settings, edited in Settings › Usage with typed entry forms.
- `EvaluateUsageRules` (`internal/service/usage_policy.go`) is pure. The first matching rule wins, using only an `ok` reading at or above its threshold. Missing or stale evidence holds the current defaults rather than forcing a fallback when no rule matches.
- The latch captures the triggering rule's identity and reset time. It releases only after that captured reset has passed and an `ok` reading with `AsOf` at or after it is below the threshold. Passing wall-clock time or rereading an old low value is not enough. A changed rule invalidates its latch.
- A pin beats rules: indefinitely, until a future RFC3339 instant, or until a qualified meter's reset. An until-reset pin requires a fresh reading with a known future reset and captures that instant; it does not follow later reset dates. Expiry removes the pin and evaluates policy again; it does not bypass a rule's latch.
- `UsagePolicyService` (`usage_profile.go`) keeps `active_usage_profile` and appends `usage_profile_switches` atomically (migration 158). History is bounded to 200 rows; the policy snapshot returns the latest 20. Reads of defaults never evaluate policy or contact providers.
- Only the `usage.lock` holder evaluates and writes policy, including pins. `cmd/legato/usage_policy_runtime.go` lets a short-lived command take an unheld lock; otherwise pin/unpin uses acknowledged IPC (`usage_pin` / `usage_pin_reply`), returning the owner's refusal or a timeout, not a false success.
- A profile that needs a tier not installed in this process is not applied. The Usage card shows a restart-required error and retains the previous defaults. Saving an adapter tier does not install it into a running adapter; Legato never restarts automatically for policy.

### New work and review passes

`AgentServiceOptions.NewWorkDefaults` reads the committed profile only for a new spawn with no explicit agent. It supplies the agent and, only if omitted, its tier. An explicit agent ignores the profile entirely, including its tier; an explicit tier without an agent is retained. `DefaultAdapter()` follows the active profile for new-work selectors. This covers web, TUI, CLI, MCP, macros, handoff workers and workflow `agent.run` calls that omit an agent.

Resume uses the kind of the slot's newest saved native session, not the current profile. Main and named slots are independent. Running agents, started runs and in-flight passes are not migrated. Swarm roles, the learning curator, the Reeve and doctor are outside this policy.

A profile's optional reviewer replaces the live reviewer agent/tier defaults for each new pass (clearing a raw-model default). Explicit loop/handoff reviewer overrides still win. The resolved selection is persisted per pass: retries, reviewer respawns and repinning the same pass use that snapshot. Legacy loops without explicit-override metadata retain their frozen settings rather than being reinterpreted as live defaults. See [Integrations](integrations.md) § Review passes.

### Switch visibility

Each committed switch, including a return to fallback, creates an informational `usage_profile_switched` Attention item from producer `usage`, in rail group `legato`, revision `profile:since`. **Open Usage** opens the Usage card; the pin action opens its pin controls. `notifications.usage` (live, default true) gates one notification per switch through the configured ntfy/OS transports, independently of Attention. The card shows the active profile, reason, since time, pin, errors and recent switches, even when readers are off.

Initializing the fallback is a silent baseline. If the first reading already triggers a different profile, that is a switch from the fallback and records history and notifications normally.

### Deployment follow-up (operator only)

After installing this feature, the operator should:

1. Configure the profiles, rules and fallback, with their tiers already installed; inspect the active selection and reason in Usage.
2. Remove **only temporary explicit Chimera choices** added to workflows or macros to work around missing usage policy, and the corresponding unwanted tool-specific tier. Keep intentional explicit selections.
3. Check remembered input defaults for those workflows too: a saved explicit agent or tier can still bypass the intended defaults. Clear only the temporary choices.
4. Leave started runs and agents unchanged. If Usage reports a tier needing a restart, choose when to restart; policy does not do so.

This is an operator checklist, not an automatic settings migration. No live settings, provider sources or account choices are changed by this documentation.

## Surfaces

- `GET /api/usage` returns `{providers: [ProviderUsage], poll_interval_seconds, policy?}`. Policy holds `active` (profile, reason, since, latch, pin and any error), configured `profiles` and recent `switches`.
  - Each `ProviderUsage` has `provider`, `state`, `stale`, `reason`, `windows` (`meter`, `used_percent`, `resets_at`, `window_minutes`), `credits`, `plan`, `as_of`, `checked_at` and `source` (`endpoint`, `app_server` or `logs`).
  - Without a usage service it is `200` with no providers, never an error.
- `POST /api/usage/refresh` answers `202` and the reading follows as `usage_changed`.
- `PUT /api/usage/pin` takes `{profile, until?, until_reset?}`; `until` is RFC3339 and is mutually exclusive with `until_reset`. Neither expiry means indefinite. `DELETE /api/usage/pin` removes the pin. Both acknowledge the owner's committed change; refusals reach the caller.
- `legato usage pin <profile> [--until <RFC3339>|--until-reset <meter>] [--json]` and `legato usage unpin [--json]` use the same policy controls, forwarded to the host from a sandbox.
- Web: `UsageStrip` (`web/src/components/UsageStrip.tsx`) sits at the head of `AgentSidebar` (prop `usage`), under the picker bar on a phone, and on the empty Agents page.
  - It shows each provider in brief and opens to the Usage card: a bar per window, its reset time, as-of, stale and unavailable states, and **Read now**.
  - Policy remains visible when readers are off; without providers or policy there is nothing to draw.
  - Tests of the Agents page stub it, as they stub `InterruptedAgents`.
- TUI: `App.SetSubscriptionUsage` reads `Snapshot` at start and every minute. `agents.Model.SetSubscriptionUsage` draws a row per provider at the foot of the list and on the ensemble's top line.

## Named Claude accounts

`claude_accounts` is an instance-local, restart-mode list of `{name, label?, config_dir}`. The implicit default remains unchanged; never move or copy its login. Names are lowercase slugs (not `default`), directories must be absolute and normalized, and account roots cannot overlap. The settings list is risky because it chooses local authentication state. Owner login is through the official binary, not the poller:

```sh
CLAUDE_CONFIG_DIR='/operator-chosen/extra-directory' claude auth login --claudeai
```

The directory above is a template; the operator chooses the live directory and label. No login is initiated by configuring a reader.

The provider table still describes kinds. A named Claude reading has `provider: claude:<name>`, `kind: claude`, `account: <name>`; its catalog uses `claude.<name>.<window>` meters. The default cache key `claude` and existing `claude.<window>` meters are unchanged, including persisted latches and pins. Rule parsing is structural (`config.ParseUsageMeter`); configuration additionally validates local account references. Readers have independent polling schedules, persisted rows and error state; polling is bounded to four concurrent reads. Account catalog entries remain available when polling is off.

Named credential readers read only the selected directory's credentials file. On macOS, named Keychain polling is explicitly unavailable when no file-backed login exists (`named_keychain_unavailable`); no guessed Keychain name or default/unsuffixed fallback is attempted. Default platform behavior is unchanged. Polling never refreshes, writes or logs out a login.

New work resolves explicit agent/account selection before the usage profile. An explicit account selects Claude; explicit Claude without an account selects the implicit default. Profiles carry independent author and reviewer accounts. Spawn, macros, workflow inputs, handoffs, swarm configuration and Reeve configuration carry stable IDs; workers resolve those IDs locally. Resume, reload, interrupted recovery and history restore keep the execution's config identity and transcript root, refusing removed/remapped accounts. Named subscription launches reject conflicting environment/settings/provider overrides with sanitized guidance; the implicit default retains its existing auth behavior. See [Claude Code](claude-code-native.md) and [Sandbox](sandbox.md).
