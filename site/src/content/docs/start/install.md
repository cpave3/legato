---
title: Install and first use
description: Download a Legato release, run the first-time setup, open the web UI and start your first agent.
sidebar:
  order: 20
---

This page takes you from nothing to an agent working on a task. It takes about ten minutes.

:::note
The first-run setup will change as Legato's configuration gets easier. If a step here does not match what you see, [open an issue](https://github.com/cpave3/legato/issues).
:::

## What you need

- **Linux** (x86-64 or ARM64) or **macOS** on Apple silicon. Releases have no build for Windows or for Intel Macs; on those, [build from source](/legato/contributing/build/).
- **tmux**. Legato runs each agent in its own tmux session.
- **git**.
- **At least one agent tool**, installed and logged in: [Claude Code](/legato/agent-tools/claude-code/), [Codex](/legato/agent-tools/codex/) or [Chimera](/legato/agent-tools/chimera/).

## 1. Download Legato

Each release has one binary for each platform and a `SHA256SUMS` file. The links below always give the newest release.

| Platform | File |
| --- | --- |
| Linux, x86-64 | `legato-linux-amd64` |
| Linux, ARM64 | `legato-linux-arm64` |
| macOS, Apple silicon | `legato-darwin-arm64` |

On Linux, download the binary, check it, and put it on your `PATH`. Change `legato-linux-amd64` to the file for your machine:

```bash
cd "$(mktemp -d)"
curl -fLO https://github.com/cpave3/legato/releases/latest/download/legato-linux-amd64
curl -fLO https://github.com/cpave3/legato/releases/latest/download/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
mkdir -p ~/.local/bin
install -m 755 legato-linux-amd64 ~/.local/bin/legato
```

On macOS, use `shasum` to check the file:

```bash
cd "$(mktemp -d)"
curl -fLO https://github.com/cpave3/legato/releases/latest/download/legato-darwin-arm64
curl -fLO https://github.com/cpave3/legato/releases/latest/download/SHA256SUMS
shasum -a 256 --check --ignore-missing SHA256SUMS
mkdir -p ~/.local/bin
install -m 755 legato-darwin-arm64 ~/.local/bin/legato
```

The macOS binary is signed ad hoc, but Apple has not notarized it. A binary that `curl` downloads does not get the quarantine flag, so it runs. If you download it with a browser instead, remove the flag:

```bash
xattr -d com.apple.quarantine ~/.local/bin/legato
```

Check that your shell finds it:

```bash
legato version
```

If the command is not found, add `~/.local/bin` to `PATH` in your shell profile.

:::tip
Each release binary also has a build provenance attestation. With the GitHub CLI you can check that GitHub Actions built it from this repository: `gh attestation verify legato-linux-amd64 --repo cpave3/legato`.
:::

## 2. Run the setup

Start Legato in a terminal:

```bash
legato
```

The first time Legato starts, it has an empty database, and it runs a short setup:

1. It makes the board's columns: Backlog, Ready, Doing, Review and Done.
2. It asks `Configure Jira integration? (y/N)`. Press `Enter` to keep the board local. You can [connect Jira](/legato/integrations/jira/) later.

Then the board opens. Press `?` at any time to see the keys.

Legato keeps its settings in `~/.config/legato/config.yaml` and its data in `~/.local/share/legato/`. The file does not need to exist: without it, Legato uses its defaults. The [configuration reference](/legato/reference/configuration/) lists every setting.

## 3. Let your agent tools report to Legato

Legato shows on each card what its agent is doing: running, waiting for you, or idle. The agent tools report this through hooks. Install them once.

Claude Code and Codex read hooks from the project, so run this in each repository where agents work:

```bash
cd ~/code/my-project
legato hooks install --tool claude-code,codex
```

Chimera reads hooks from your home folder, so run this once:

```bash
legato hooks install --tool chimera
```

`legato hooks uninstall --tool <tool>` removes only Legato's hooks.

## 4. Start an agent on a task

1. On the board, press `n` to make a task. Type a title, and a description of the work if you like. Press `Enter`.
2. With the task selected, press `a`.
3. Choose the agent tool and the folder the agent starts in. Press `Enter`.

Legato starts the agent in a tmux session and gives it the task. The card now shows the agent's state. Press `A` to see all your agents, and `Enter` on one to attach to its terminal. Press `Ctrl+]` to detach and go back to Legato.

## 5. Open the web UI

The web UI shows the same board and agents in a browser, and lets you talk to agents from your phone. To start it with the TUI, add this to `~/.config/legato/config.yaml`:

```yaml
web:
  enabled: true
```

Restart Legato, then open <http://localhost:3080>. The web UI asks for a token the first time. Print it with:

```bash
legato auth token
```

To use Legato on your phone, see [Your phone and other devices](/legato/running/devices/). `legato pair` pairs the Legato app with four words.

:::caution
The web UI controls your agents, and your agents can run commands on your machine. Do not open its port to the internet. To reach it away from home, use a private network such as Tailscale. See [Web access and tokens](/legato/security/web-auth/).
:::

## 6. Updates

A release build updates itself. By default it asks first: the web UI shows a bar when a new release is out, and an item in [Attention](/legato/using/attention/). You can change this to update automatically, or never, in the web UI under Settings › Updates. See [Updates](/legato/running/updates/).

## Next steps

- [Take the tour](/legato/start/tour/) of the board, the agents view and the web UI.
- [Choose how to run Legato](/legato/running/workstation/): on your workstation, as a web server, or on a server of its own.
- [Connect Jira](/legato/integrations/jira/) and [GitHub](/legato/integrations/github/).
- [Give agents credentials](/legato/security/credentials-setup/) without giving them the secrets.
