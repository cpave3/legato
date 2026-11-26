---
title: Subscription usage
description: See how much of your Claude and Codex subscriptions' usage limits you have used, and when each limit resets.
sidebar:
  order: 25
---

Subscription usage shows how much of your Claude and Codex subscriptions' usage limits you have used. It shows the same windows as Claude Code's `/usage` view: Claude's 5-hour and weekly limits, and the weekly limit of a model when your plan has one. It also shows your Codex limits. Each window says when it resets. Use it to see which subscription has room before you start an agent.

Legato reads the usage on the machine it runs on, with the logins Claude Code and Codex already keep. You do not sign in to anything in Legato.

## Where it shows

- **Web UI:** a strip at the head of the agents list (under the agent picker on a phone) shows each subscription in brief, such as `Claude 5h 42% wk 91%`. Click it to open the Usage card:
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

Neither login is ever sent to an agent, shown in the UI or written to a log. An agent in Legato's sandbox can run `legato usage`, but it only gets the readings.

:::caution
Neither source is a published API: Claude's usage endpoint and Codex's rate-limit call can change without notice. When Legato cannot understand an answer, it shows the subscription as unavailable and says why. It never guesses.
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
| No subscription limits | The login is an API key or a plan without these limits. |
| The codex command is not installed | Install Codex on this machine, or use it once so its session logs have a reading. |
| Nothing reported yet | Codex has not written any limits yet. Use it once. |
| Its usage report changed | The service changed its answer. Update Legato. |
| Asked too often / could not reach it | Legato waits and tries again by itself. |

## Turning it off

Each subscription can be turned off in Settings › Agents › Subscription usage, or in `config.yaml`:

```yaml
usage:
  claude:
    enabled: true
  codex:
    enabled: false   # never run codex or read its logs
  poll_interval: 5m
```

Off, Legato never asks that service. See the [configuration reference](/legato/reference/configuration/#subscription-usage).
