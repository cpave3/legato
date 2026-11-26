# Subscription usage

How much of the Claude and Codex subscriptions' usage limits the operator has used (LEG-659). It is not `service.Usage` / `UsageService`, which are Manticore's token counts (see [TUI](tui.md) § Token usage). Site page: `using/usage`. Switching the default agents by usage is LEG-660 (usage profiles and rules), not built yet.

## Sources

Neither source is a published API. Every answer the readers do not understand is an error with a reason, never a guess, and a test pins each response shape relied on.

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
  - It takes the newest `rate_limits` of `event_msg`/`token_count` lines (snake_case: `used_percent`, `window_minutes`, `resets_at`, `credits.has_credits`). It tries the 10 newest `rollout-*.jsonl` files under `sessions/YYYY/MM/DD`, reading the last 512 KiB of each.
  - `AsOf` is the event's `timestamp`.
  - `SessionsDir` is `$CODEX_HOME/sessions`, else `~/.codex/sessions`.
- **`Classify(minutes)`** names a window by its length:
  - up to 360 minutes → `five_hour`;
  - a week less an hour or more → `weekly`;
  - anything else → dropped.
  - Primary and secondary are not fixed lengths: the ticket's sample had a weekly primary and no secondary.
- Chimera's Codex models are assumed to use the same ChatGPT account as the Codex CLI (the Reeve's decision, LEG-659). If they do not, a Chimera-side source is needed.

## The service: `internal/service/subscription_usage.go`

- **`SubscriptionUsageReader`** (`Provider`, `Read`) is one provider.
  - `ClaudeUsageReader` and `CodexUsageReader` (`subscription_usage_readers.go`) map the engine's windows to meters: `five_hour`, `weekly`, `weekly_opus`, `weekly_sonnet`.
  - They map the engine's errors to `*UsageUnavailableError{Reason, RetryAfter}`. The reasons are `not_signed_in`, `login_expired`, `no_subscription`, `codex_missing`, `no_data`, `endpoint_changed`, `rate_limited`, `unreachable` and `error`. `not_read` means never read.
  - `CodexUsageReader` asks the app server first and falls back to the logs for any failure but `ErrNotSignedIn`.
- **`SubscriptionUsageService`** reads providers and keeps their readings.
  - `Poll` reads each provider whose next read is due, every `PollInterval` (`usage.poll_interval`, 5m, at least 2m).
  - `Refresh` reads now, but at most once a minute per provider and never during a back-off.
  - `RequestRefresh` refreshes in the background in the process that polls; elsewhere it calls `Forward`.
  - `Run` polls once a minute for whatever is due.
  - One read per provider is in flight at a time.
  - The first `Poll` or `Refresh` in a process takes each provider's next read from its kept reading's `checked_at` (`seed`), so a restarted Legato (a hot reload, an update) does not ask every provider again at once. A back-off is not kept across a restart.
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

- `usageReaders` follows `usage.claude.enabled` and `usage.codex.enabled` (both on when unset).
- `subscriptionUsageFor` builds the service on a store. It returns nil with both providers off.
- `startSubscriptionUsage` runs the poller in the one process of a data folder that holds `<data_dir>/usage.lock` (flock, like `mqtt.lock`). Elsewhere it does nothing.
- A kept reading publishes `EventSubscriptionUsageChanged` and broadcasts the IPC message `usage_changed`. Other processes publish `EventSubscriptionUsageInvalidated` for it. Both reach the web as the WebSocket message `usage_changed`.
- A refresh asked in a process that does not poll broadcasts `usage_refresh`. That becomes `EventSubscriptionUsageRefreshRequested`, which only the poller listens to.
- Serve and the TUI wire it. `legato usage` (`usage_cmd.go`) reads the cache.
  - `--refresh` reads in the command itself when it can take `usage.lock`; otherwise it broadcasts `usage_refresh` and waits 3 s.
  - It is forwarded from the sandbox like `doctor`, so the logins are only read on the host. The Reeve's class for it is `read`.

## Surfaces

- `GET /api/usage` returns `{providers: [ProviderUsage], poll_interval_seconds}`.
  - Each `ProviderUsage` has `provider`, `state`, `stale`, `reason`, `windows` (`meter`, `used_percent`, `resets_at`, `window_minutes`), `credits`, `plan`, `as_of`, `checked_at` and `source` (`endpoint`, `app_server` or `logs`).
  - Without a usage service it is `200` with no providers, never an error.
- `POST /api/usage/refresh` answers `202` and the reading follows as `usage_changed`.
- Web: `UsageStrip` (`web/src/components/UsageStrip.tsx`) sits at the head of `AgentSidebar` (prop `usage`), under the picker bar on a phone, and on the empty Agents page.
  - It shows each provider in brief and opens to the Usage card: a bar per window, its reset time, as-of, stale and unavailable states, and **Read now**.
  - It renders nothing with no providers.
  - Tests of the Agents page stub it, as they stub `InterruptedAgents`.
- TUI: `App.SetSubscriptionUsage` reads `Snapshot` at start and every minute. `agents.Model.SetSubscriptionUsage` draws a row per provider at the foot of the list and on the ensemble's top line.
