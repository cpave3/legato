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

Then ask Legato what else it needs:

```bash
legato doctor
```

It checks tmux, git and your agent tools, says whether you are signed in to them, and gives a one-line fix for anything missing. A failed **required** check means part of Legato cannot work until you fix it; an optional one only limits a feature. See [`legato doctor`](/legato/reference/cli/#running-legato).

:::tip
Each release binary also has a build provenance attestation. With the GitHub CLI you can check that GitHub Actions built it from this repository: `gh attestation verify legato-linux-amd64 --repo cpave3/legato`.
:::

## 2. Run the setup

Start Legato in a terminal:

```bash
legato
```

The first time Legato starts, it has an empty database, and it runs a short setup in the terminal. It works over SSH too. Type `s` to skip a step, and press `Enter` to keep what is shown in brackets.

1. It makes the board's columns: Backlog, Ready, Doing, Review and Done.
2. **Checks.** It runs the checks of [`legato doctor`](/legato/reference/cli/#running-legato). When something required is missing, such as tmux, it shows the fix and waits: fix it and press `Enter` to check again, or type `q` to go on to the board without it.
3. **Default agent tool.** It lists the agent tools it found and whether you are signed in to each. Choose the one Legato starts when you name none. For Codex and Chimera, it offers to install their hooks (see step 3 below).
4. **Repositories.** It lists the git repositories in `~/Projects`, `~/src`, `~/code` and the folder you started it in, with those you used in the last two weeks ticked. Type numbers to tick or untick, then press `Enter`: the ticked ones are saved as [repositories](/legato/using/agents/#where-the-agent-starts), offered when you start agents and workflows. Those saved before are shown and kept.
5. **Web UI and phone.** It offers to turn on the web UI: it adds `web.enabled: true` to `config.yaml`, keeping the rest of the file, and prints the address. Then it offers to pair your phone: on the first run, Legato shows the four words and a QR code as soon as its web server starts, before the board opens. A `web.enabled` your file already sets is left as it is.
6. **Ticket tracker.** None keeps the board local. Jira asks for its address, your email and an API token, which is not shown as you type and goes to Legato's secret store; a mistake asks again rather than ending the setup. For Burnlite, it gives the link to connect it in the web UI.

Run `legato setup` at any time to go through it again. Each step shows what is set now, and `Enter` keeps it. A setting your `config.yaml` sets is shown as pinned and skipped, and setup never removes anything.

Then the board opens. Press `?` at any time to see the keys.

Legato keeps its settings in `~/.config/legato/config.yaml` and its data in `~/.local/share/legato/`. The file does not need to exist: without it, Legato uses its defaults. The [configuration reference](/legato/reference/configuration/) lists every setting.

## 3. Let your agent tools report to Legato

Legato shows on each card what its agent is doing: running, waiting for you, or idle. The agent tools report this through hooks. Claude Code needs nothing from you: Legato loads its own plugin into every Claude Code session it starts. Install the hooks of the others once.

Codex reads hooks from the project, so run this in each repository where agents work:

```bash
cd ~/code/my-project
legato hooks install --tool codex
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

The web UI shows the same board and agents in a browser, and lets you talk to agents from your phone. If you turned it on during setup, it is already running. Otherwise, to start it with the TUI, add this to `~/.config/legato/config.yaml`:

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
