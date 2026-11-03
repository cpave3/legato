---
title: Argus
description: Let Argus find the task that owns a pull request, hand its agent a case, hear back, and show its cases in Attention.
sidebar:
  order: 50
---

Argus is an operations service that watches your work, for example for failing CI, and opens a case when something needs doing. With the Argus integration, Argus can:

- **Look up the owner** of a pull request, branch or commit: the Legato task and its agents.
- **Delegate** a case to that task's agent, with a brief that says what to fix or find out.
- **Hear back** from the agent, which replies with the `reply_case` tool.
- **Show its cases** in Legato's [Attention](/legato/using/attention/) on the task that owns the pull request.

A delegation is Argus's request for work. It is not a handoff, which is a task sent to a worker instance.

## Set it up

1. Add Argus to `config.yaml`:

   ```yaml
   integrations:
     argus:
       url: https://argus.lan   # https, or http to a loopback address
       delivery: auto           # auto or ask
       agent: claude-code       # claude-code, codex or chimera
   ```

2. Restart Legato. Argus reaches Legato through its [web server](/legato/running/web-server/), so make sure it runs.
3. Make a token for Argus and give it to Argus:

   ```bash
   legato integration token create argus --scope owner:read,delegate,attention
   ```

   Legato prints the token once and keeps only a hash of it.

4. Connect Legato to Argus, so that agents' replies can reach it:

   ```bash
   legato integration connect argus
   ```

   Legato opens Argus in your browser and asks for your consent. On a machine with no browser, add `--no-browser`: Legato prints the address to open elsewhere, and you paste the address the browser was sent to back into the command.

5. Check the connection:

   ```bash
   legato integration status argus
   ```

| Setting | What it does | Default |
| --- | --- | --- |
| `integrations.argus.url` | Argus's address: where replies go, and where `connect` signs in. | none, required |
| `integrations.argus.delivery` | `auto` delivers a delegation at once. `ask` waits for you to approve it. | `auto` |
| `integrations.argus.agent` | The agent tool Legato starts for a delegation when the task has no agent. | `claude-code` |

If Argus uses Legato's own TLS certificate, Argus must trust Legato's certificate authority, at `certs/ca.pem` in Legato's data directory.

:::note
`legato integration connect` refuses when Legato's secret store is unlocked by a passphrase, because Legato must store a new refresh token while nobody is there to type it.
:::

## Tokens

An integration token starts with `lgi_` and works only on Legato's integration API. Each token has scopes:

| Scope | Lets Argus |
| --- | --- |
| `owner:read` | Look up who owns a pull request, branch or commit. |
| `delegate` | Hand cases to agents and send them follow-up messages. |
| `attention` | Put its cases in Attention. |

```bash
legato integration token list          # names, scopes and dates, never the tokens
legato integration token revoke argus  # stops the token at once
```

Without the `integrations.argus` block, owner lookup and tokens still work, but Legato takes no delegations or cases, offers no `reply_case` tool and sends nothing.

## Owner lookup

Argus asks which task owns a repository's pull request, commit or branch. Legato looks at tasks that are not archived and whose code context or linked pull request names the repository. It matches by PR number, then by head commit, then by branch, and puts tasks with a running agent first.

The answer names the task and its agents and their states. It never includes working directories, descriptions or notes. A link to the task is included only when you set `web.public_url`.

## Delegations

A delegation carries a brief and a mode: **fix** or **find out**. Legato decides where it goes before it answers:

- **The task has a running agent:** Legato sends the brief to it as a message.
- **The task's agent stopped and its conversation was saved:** Legato resumes it with the brief as its first message.
- **The task has no agent:** Legato starts `integrations.argus.agent` in the task's worktree or working directory.
- **Nobody owns the pull request:** Legato makes a new local task and starts an agent for it. With [Yggdrasil worktrees](/legato/integrations/yggdrasil/) on and a branch to work on, it makes a worktree. Otherwise a find-out delegation starts in the repository's `primary_dir`, and a fix is refused.

When Argus names a repository, it must be in your `repositories` list with a `primary_dir`. A new task goes in your Doing (or In Progress) column.

The agent sees the brief under a line that names the case, and a note that it comes from Argus, not from you.

### Find out means read only

For a find-out delegation that starts or resumes an agent, Legato runs the agent in its [sandbox](/legato/security/sandbox/) when you configured one, with the checkout mounted read only. The [credential broker](/legato/security/credential-broker/) refuses it pushes and pull request and issue changes, whatever your rules say. A message to an agent that is already running does not change what it can do.

### Ask first

With `integrations.argus.delivery: ask`, each delegation waits for your approval: **Deliver** or **Refuse**. It waits with no time limit.

When a delegation can't reach its agent, Attention shows it on the task.

## Replies

Once `integrations.argus` is set, every agent session has the `reply_case` tool. The agent calls it with the delegation's ID, an outcome (`done`, `finding`, `question` or `cannot`) and its text. Legato queues the reply and sends it to Argus, and tries again when Argus can't be reached. A reply Argus has not taken within 24 hours fails, and Attention says so.

If Argus stops accepting Legato's tokens, Legato marks the connection broken, keeps the replies, and raises an Attention item that tells you to connect again.

```bash
legato integration disconnect argus   # forget the connection and its token
```

## Cases in Attention

Argus puts one Attention item for each open case. Legato attaches the item to the task that owns the case's pull request, and moves it when a task takes the pull request later. The web dashboard puts plain cases under **Merge blockers** and cases that need you under **Needs intervention**, with **Open in Argus**. In the TUI, `enter` opens the case in your browser. You act on a case in Argus, not in Legato.

While an open case covers a pull request, Legato leaves "CI failing" out of that pull request's own Attention item, so you don't see the same problem twice.

The **Integrations** section of the web UI's Settings shows the Argus connection and the tokens. You make and revoke tokens, and connect, with the commands on this page.
