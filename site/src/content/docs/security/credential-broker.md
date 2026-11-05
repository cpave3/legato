---
title: How credentials reach agents
description: How Legato lets agents use SSH keys, API keys and GitHub without holding them, under rules you set and approvals you give.
sidebar:
  order: 20
---

Legato holds credentials for your agents, so you never paste a secret into an agent's prompt, environment or files. An agent uses a key through Legato. Your rules decide whether a use goes ahead at once, after you approve it, or never. Legato records every request, decision and use.

To set it up on a machine, follow [Set up credentials on a host](/legato/security/credentials-setup/). This page explains how it works.

## How an agent gets a credential

Each credential reaches agents through one channel. Most channels keep the secret in Legato: the agent can use it, but cannot read it.

| Credential | The agent gets | The agent sees the secret |
| --- | --- | --- |
| An SSH key | An SSH agent socket of its own. Legato signs each login you allow. | No |
| An API key with a proxied profile (`elevenlabs`, `api-key`) | A placeholder key and the address of Legato's proxy. Legato puts the real key into each request it allows. | No |
| GitHub, through a GitHub App you create | Short-lived tokens for one repository, given to `git` and `gh`. Pushes go through Legato's proxy. | Read tokens only |
| Any other secret (profile `secret`) | The value, through `legato credential exec`, after you allow it | Yes |

### SSH keys

Every agent session gets its own SSH agent socket in `SSH_AUTH_SOCK`. It lists only the keys of your `ssh` credentials, and signs a login only for the session it belongs to. The agent runs `ssh` and `git` as usual. When a login needs your approval, `ssh` waits for you.

Legato names the host by the key the server proves it holds, looked up in your `known_hosts`, so a rule can allow `ssh://deploy@build-box` and nothing else.

### API keys through the proxy

When a session starts, Legato gives it a placeholder key and a base address for each proxied credential, for example `ELEVENLABS_API_KEY` and `ELEVENLABS_BASE_URL`. The agent sends plain requests to Legato's proxy on a loopback port. Legato checks each request against your rules, swaps the placeholder for the real key, and sends the request on to the real service over TLS. A placeholder works only for its credential and only while its session runs.

A tool can use the proxy only when it lets you set its base address.

### GitHub

With a `github` credential, Legato acts as a GitHub App you create:

- `git` fetches with read tokens for the one repository it asks for.
- `git push` goes through Legato's proxy. Legato reads which branches the push changes, checks each against your rules, and pushes with a token it makes for that push and then revokes. The agent never holds a token that can write code.
- `gh` runs through a wrapper that asks Legato for a token for the command: a read token, or a token for pull requests or issues once you allow it.
- Opening a draft pull request (`gh pr create --draft`, or the API with `"draft": true`) is its own operation, `pr.create-draft`, decided for the branch it is opened from. Any other pull request asks as before.
- Pushes to the default branch, pushes of tags and branch deletions are refused by default.

### Secrets handed over

For a credential with the `secret` profile, the agent runs one command with the value in an environment variable:

```bash
legato credential exec openai --env OPENAI_API_KEY -- ./script.sh
```

The value never passes through the agent's own output. `legato credential get <name>` prints it. Both check the use against your rules first, and wait for you when they need to.

## Asking for access

An agent can ask for a scope ahead of time with Legato's `request_access` tool, or with `legato credential request`. More often it simply uses the channel: an SSH login, a request to the proxy, a push. Legato then checks the use against the rules.

Agents are told about each credential and how to use it when they start. They are told to wait for your decision, and never to look for another way in.

## Rules

Each credential has rules. A rule names an operation, a resource (a pattern, where `**` matches across `/`) and how to decide:

| `approve` | You are asked | The grant lasts |
| --- | --- | --- |
| `auto` | never | until the agent ends |
| `session` | once | until the agent ends |
| `window` | once | the rule's `window`, such as `30m` |
| `request` | for each use | one use |
| `deny` | never; the use is refused | — |

When several rules match, the most specific one decides. A use that no rule matches is refused. Each profile comes with sensible default rules, and `policy` replaces them:

```yaml
credentials:
  - name: elevenlabs
    secret: legato://elevenlabs-key
    profile: elevenlabs
    policy:
      - { operation: tts.speak, approve: auto, limit: { characters: 20000/day } }
      - { operation: tts.speak, approve: window, window: 1h }   # past the limit
```

A `limit` counts requests, bytes or characters per minute, hour, day or week. When a use would pass the limit, the next matching rule decides instead: above, speech is automatic up to 20,000 characters a day, then needs your approval once an hour. Counts survive a restart.

See the [configuration reference](/legato/reference/configuration/) for every credential setting.

## Grants

When you allow a use, you choose how far it goes. The approval offers the rule's mode and every stricter one: a `session` rule offers **For this session**, **For 30 minutes** and **Once**.

A grant covers what you approved, for that one agent session. It ends when its time runs out, after its one use, when the agent ends, or when you revoke it. The **Standing grants** list on the web UI's Audit page shows what agents may still do without asking, with **Revoke** on each.

### Handed-off tasks

Handing a task off to a worker to make a change counts as your approval to push its work. When the worker starts the task's agent, it grants that session, for its GitHub credential that lists the repository:

- pushes to `legato/<task id>` and to `legato/<task id>-<a few words>`, where the task ID is the one on your main instance;
- opening draft pull requests from those branches.

Nothing wider. Merging, the default branch, tags, other branches and pull requests that are not drafts are refused or ask, as for any agent. A handoff to find something out is granted nothing. The grant shows in **Standing grants** on the worker, recorded in the audit log as decided by the handoff, and you can revoke it there. Your rules still come first: a rule that refuses a use refuses it whatever is granted, and a credential with its own `policy` needs a rule for `pr.create-draft` before drafts can be opened at all.

The agent is told its branches are named after your main's task ID, not the worker's, and that these pushes and drafts are already allowed.

## The 60-second wait

A use that needs you waits up to 60 seconds for your decision. Decide in that time, and the use goes ahead as if nothing happened.

After 60 seconds the agent is told the request is pending, and carries on with other work or stops its turn. The approval stays open. When you decide, Legato sends the agent a message, `[legato approvals] … approved …`, and the agent tries again.

A `git push` cannot wait. Legato refuses it at once, asks you, and tells the agent to push again after your decision.

## Where you approve

An approval reaches you wherever you are:

- **Web UI**: the approval tray, at the bottom right of every page. A browser notification appears when the Legato tab is not in front.
- **Attention**: each approval is an item, with its options as actions.
- **TUI**: the key bar says how many approvals wait. Press `!` to open them: `y` allows with the first option, `1` to `9` with that option, `n` denies.
- **Phone**: with ntfy set up, each approval sends a notification that opens the Legato app on it. See [Notifications](/legato/integrations/notifications/).

The first decision counts. To deny, you can add a note that the agent reads.

## Signed approvals

Some decisions should need you in person, not just someone who has the web token. A signed approval can be allowed only with a signature from a device you enrolled:

- **A phone** running the Legato app keeps a key in its secure hardware and unlocks it with your fingerprint or PIN for each signature.
- **A security key in the [desktop app](/legato/running/desktop-app/)**, enrolled on every server the app has paired with. You touch the key to sign. On a Mac, Touch ID can be the key: you sign with your fingerprint.
- **A passkey** on a computer, made in Settings › Security in the web UI. It needs a secure page: `localhost`, or HTTPS at a host name. A security key such as a YubiKey can sign with a touch alone; your browser or password manager may still ask for a PIN or fingerprint. Each decision records which it was: a touch, or a verification.

A rule asks for a signature with `signed: true`. Once you enroll any device, rules with `approve: request` ask for one by default. Adding or removing a device always needs one. Denying never does.

The TUI and Attention cannot allow a signed approval; they tell you to use your phone or passkey. The first device you enroll is accepted at once, and each later one needs a signature from an enrolled device. If you lose your phone, run `legato device reset` on the machine Legato runs on to remove every device.

## The audit log

Legato records each request, grant, denial, use, refusal and revocation, with the time, the agent, the task, the credential, the operation and the resource. It never records a secret. Each row holds a hash of the row before it, so a changed or missing row shows.

- In the web UI, open **Audit** in the side rail (**More › Audit log** on a phone).
- In a terminal:

  ```bash
  legato audit                 # newest first
  legato audit --credential github --limit 20
  legato audit verify          # check the chain of hashes
  ```

## Where secrets are stored

A credential's `secret` names where its value lives:

| Reference | Store |
| --- | --- |
| `legato://name` | Legato's own store, sealed with a master key. Add a value with `legato secret add <name>`. |
| `op://vault/item/field` | 1Password, read as a service account |
| `systemd-creds://name` | An encrypted credential of a systemd unit |
| `keychain://service/account` | The macOS Keychain |
| `env://NAME` | An environment variable of the Legato process |
| `file:///path` | A file that other users cannot read |

Legato reads a value when it is used, and never caches or logs it.

## What it does not stop

:::caution
Without the sandbox, an agent runs as you. It can read what you can read: Legato's configuration, the 1Password token file, `~/.ssh`, and your own `gh` login. It can undo the session's settings and push with your own keys.
:::

The broker makes the approved way the one agents use and are told to use, and it records every use. It does not make the other ways impossible. To take them away, run agents in Legato's [sandbox](/legato/security/sandbox/), which hides your credentials and Legato's own files from them.

The broker also does not filter the network. An agent can reach any service that needs no credential.
