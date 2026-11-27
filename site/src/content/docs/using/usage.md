---
title: Subscription usage
description: See subscription limits and select defaults for new work with usage profiles, rules and pins.
sidebar:
  order: 25
---

Subscription usage shows how much of your Claude, Codex and OpenCode Go subscriptions' usage limits you have used. It shows the same windows as Claude Code's `/usage` view: Claude's 5-hour and weekly limits, and the weekly limit of a model when your plan has one. It also shows your Codex limits, and OpenCode Go's 5-hour, weekly and monthly limits. Each window says when it resets. Use it to see which subscription has room before you start an agent.

Legato reads Claude's and Codex's usage on the machine it runs on, with the logins those tools already keep. OpenCode Go is read with its API key, which you store once as a Legato secret and name in Settings; the reading is your account's, so it counts the calls every client of that key makes — including calls another harness like Manticore makes — not only Legato's own.

## Where it shows

- **Web UI:** a strip at the head of the agents list (under the agent picker on a phone, and on the Agents page when no agent runs) shows each subscription in brief, such as `Claude 5h 42% wk 91%`. Click it to open the Usage card:
  - a bar for each window, with when it resets;
  - how old the reading is ("as of 2m ago");
  - why the usage is stale or unavailable, when it is;
  - **Read now**, which reads the usage again at once.
- **TUI:** the agents view lists each subscription at the foot of the agent list, and the ensemble lists them on its top line.
- **CLI:** `legato usage` prints the same.

```bash
legato usage             # the last reading
legato usage --refresh   # read it now
legato usage --json      # for scripts
```

A bar turns amber at 75% and red at 90%.

## Where the numbers come from

- **Claude:** Legato asks the usage endpoint that Claude Code's own `/usage` view uses, with Claude Code's login on this machine. On Linux, that login is the file `~/.claude/.credentials.json` (or the folder `CLAUDE_CONFIG_DIR` names). On macOS, it is the Keychain.
  - Legato only reads the login. It never refreshes it, because that would sign Claude Code out.
  - When the login has expired, Legato waits until Claude Code signs in again, which happens the next time you use Claude Code.
- **Codex:** Legato runs `codex app-server` on this machine for a moment and asks it for the account's rate limits, which uses Codex's own login.
  - Without the `codex` command, or when that fails, Legato takes the newest limits Codex wrote into its session logs under `~/.codex/sessions`. These are only as fresh as your last Codex turn, and the card says so.
  - Chimera's Codex models count against the same ChatGPT account, as long as Chimera signs in with the same account as the Codex CLI.
- **OpenCode Go:** Legato asks the usage endpoint OpenCode documents for its clients, with the API key named by `usage.opencode.api_key`. OpenCode Go is off until you turn it on in Settings › Usage.
  - Store the key first: `legato secret add opencode-go` (paste the key, it is never printed), then name the secret in Settings, `legato://opencode-go`. The row shows **Stored**, never the key.
  - The reading is your account's allowance: 5-hour, weekly and monthly, each as a percent and a reset time exactly as OpenCode reports it. No OpenCode inference needs to run on Legato.
  - Replacing the key's value in the secret store applies when Legato restarts; the setting says so, and so does a refused key.

No login or key is ever sent to an agent, shown in the UI or written to a log. An agent in Legato's sandbox can run `legato usage`, but it only gets the readings.

:::caution
Claude's usage endpoint and Codex's rate-limit call are not published APIs and can change without notice. When Legato cannot understand an answer, it shows the subscription as unavailable and says why. It never guesses.
:::

## How often it is read

Legato reads each subscription every 5 minutes (`usage.poll_interval`, at least `2m`). **Read now** and `legato usage --refresh` read at once, but at most once a minute for each subscription.

When a subscription refuses or fails, Legato waits longer before it asks again: 10 minutes, then 20, then 30 at most, or as long as the service asked. Only one Legato process on a data folder reads the usage. The TUI and `legato serve` share what it reads.

## Stale and unavailable

A reading is **stale** when it is older than two poll intervals, or when the last read failed and Legato shows the reading before it. A stale reading is dimmed and says how old it is.

A subscription is **unavailable** when there is no reading to show. The card says why:

| Reason | What to do |
| --- | --- |
| Sign in to Claude Code (or Codex) on this machine | Sign in with the tool on the machine Legato runs on. |
| Login has expired | Use the tool once; it signs in again. |
| No API key named (OpenCode Go) | Name the key's secret in Settings › Usage. |
| API key refused | Replace the key's secret, then restart Legato. |
| No subscription limits | The account or key has a plan without these limits. |
| The codex command is not installed | Install Codex on this machine, or use it once so its session logs have a reading. |
| Nothing reported yet | Codex has not written any limits yet. Use it once. |
| Its usage report changed | The service changed its answer. Update Legato. |
| Asked too often / could not reach it | Legato waits and tries again by itself. |

## Turning it off

Each subscription can be turned off in Settings › Usage › Subscription usage, or in `config.yaml`:

```yaml
usage:
  claude:
    enabled: true
  codex:
    enabled: false   # never run codex or read its logs
  opencode:
    enabled: true    # off by default; needs usage.opencode.api_key
    api_key: legato://opencode-go
  poll_interval: 5m
```

Off, Legato never asks that service. OpenCode Go is off until you turn it on and name a key. See the [configuration reference](/legato/reference/configuration/#subscription-usage).

## Profiles and rules

A **usage profile** sets the default agent and optional tier for new work. It can also set a reviewer agent and tier for new review passes. A **usage rule** selects a profile when a subscription meter reaches a threshold. For example, switch away from Claude at 90% weekly usage, then return after its reset.

In **Settings › Usage**, add profiles and rules with the typed forms, then choose a fallback profile. Profiles choose existing adapter tiers, never raw model names. Set up the tiers first. This example assumes both tiers already exist under their adapters:

```yaml
usage:
  profiles:
    - name: usual
      agent: claude-code
      tier: standard
    - name: spare
      agent: chimera
      tier: codex
      reviewer:
        agent: claude-code
        tier: standard
  rules:
    - name: claude-weekly-high
      order: 10
      meter: claude.weekly
      at_least: 90
      profile: spare
  fallback_profile: usual
```

Each rule needs a name, an explicit `order`, a `meter`, `at_least` (1–100 inclusive) and a profile. Lower orders come first; names break ties. The first matching rule wins. The meters are:

- `claude.five_hour`
- `claude.weekly`
- `claude.weekly_opus`
- `claude.weekly_sonnet`
- `codex.five_hour`
- `codex.weekly`
- `opencode.five_hour`
- `opencode.weekly`
- `opencode.monthly`

Only fresh, available readings can trigger a rule. Once triggered, a rule holds until its **captured reset time** has passed and a fresh reading dated at or after that reset is below its threshold. An old low reading or the clock passing the reset alone does not release it.

When no rule matches, Legato uses the fallback profile. With no fallback set, it uses your usual configured defaults. Missing or stale readings hold the current defaults instead of forcing a switch back. Turning readers off does not hide the policy or its pin controls.

The Usage card shows the active profile, why it was selected, when it took effect and recent switches. A switch, including a return to the fallback, adds an informational [Attention item](/legato/using/attention/). `notifications.usage` is on by default and controls switch notifications through your configured ntfy and desktop notifications; turning it off does not turn off the rules or Attention items.

Starting with the fallback does not send a switch notification. If the first reading already selects a different profile, Legato records and notifies that switch too.

:::note
If a profile needs a tier saved since Legato started, Usage can show a restart-required error and keep the previous defaults. Choose when to restart Legato to install that tier into its running adapter. Policy never restarts Legato for you.
:::

### What changes

Profiles apply only to **new work with no explicit agent**: web, TUI, CLI, MCP, macros, worker handoffs and workflow `agent.run` steps. An explicit agent ignores the whole profile, including its tier. If you give a tier but leave the agent unset, your tier is retained.

Resuming a conversation keeps the agent kind of that slot's newest saved conversation, even after a profile switch. Running agents, started runs and in-flight review passes do not change. Swarm roles, the learning curator, the Reeve and doctor do not follow this policy.

Each new review pass reads the profile's optional reviewer defaults; an explicit reviewer override on its loop or handoff still wins. The pass saves its resolved choice, so retries keep it. Older loops keep their frozen reviewer settings.

## Pinning a profile

Open the Usage card's pin controls to keep a profile regardless of the rules: until you unpin it, until a time, or until a meter resets. The CLI offers the same choices:

```bash
legato usage pin spare
legato usage pin spare --until 2026-12-01T18:00:00Z
legato usage pin spare --until-reset claude.weekly
legato usage unpin
```

`--until` takes a future RFC3339 time with a time zone. `--until-reset` takes one of the qualified meters above, requires a fresh reading with a known future reset, and captures that reset time. Choose only one expiry. With neither, the pin is indefinite. Expiry or unpinning evaluates the rules again; a rule's reset hold still applies.

Pinning works with readers off, except that an until-reset pin needs a fresh reset reading. Pin changes are confirmed by the one process that owns usage policy. A refusal or timeout is an error, not a successful pin. Scripts can use `PUT /api/usage/pin` with `{ "profile": "spare" }`, optionally `until` or `until_reset`, and `DELETE /api/usage/pin` to unpin.

## After deployment

This is an operator follow-up, not an automatic settings migration:

1. Configure profiles, rules and fallback, then check the active choice and reason in Usage.
2. Remove **only temporary explicit Chimera choices** from workflows and macros that were added to work around the lack of usage policy. Remove their corresponding unwanted tool-specific tier too. Keep intentional explicit choices.
3. Check those workflows' remembered input defaults for the same temporary choices. A remembered explicit agent or tier still affects new work.
4. Leave started runs and agents unchanged. Restart only when you choose to apply a tier that Usage says is not installed yet.

This does not change subscription sources, provider accounts or live settings by itself.

## Additional Claude usage accounts

You can add named Claude configuration directories under **Settings › Usage › Accounts**. Each account has its own reading, reset times and unavailable state. Existing default Claude meters stay unchanged; a named account uses meters such as `claude.work.weekly`. See [Named Claude usage accounts](/legato/reference/configuration/#named-claude-usage-accounts) for fields and the current launch limitation. On macOS, named usage requires a file-backed login; named Keychain polling is unavailable.
