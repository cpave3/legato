---
title: Agents on a server of their own
description: Run Legato on an always-on machine, so agents work while your laptop is closed.
sidebar:
  order: 40
---

Legato can run on an always-on machine (a VM, a spare box) with nobody at its terminal. Agents work there while your laptop is closed: you start them from the web UI or the phone app, or by API, and you watch and answer them from either. This guide sets one up.

:::caution
These steps are written from Legato's code, and nobody has yet run them end to end on a fresh VM. If a step is wrong, [open an issue](https://github.com/cpave3/legato/issues) or edit this page.
:::

A complete example of the files this guide makes is in [`docs/examples/cloud-vm/`](https://github.com/cpave3/legato/tree/main/docs/examples/cloud-vm): `config.yaml`, `legato.service` and `env.example`. A test loads that configuration, so it stays valid.

On Ubuntu LTS, an Ansible playbook does steps 1, 4 and 5 for you, with the sandbox on: [`ops/ansible/`](https://github.com/cpave3/legato/tree/main/ops/ansible). It installs the packages, toolchains and agent tools, the latest Legato as a service, and a configuration without secrets; logging the agent tools in, Tailscale and GitHub stay yours to do.

## What protects what

There are two ways to keep agents on the server from doing harm, and you can use both.

| | The VM is the boundary | Legato's sandbox |
| --- | --- | --- |
| What an agent can reach | Everything the VM's user can: its files, its tokens | Its work, its tool's login, and Legato's sockets for it (see [Sandbox](/legato/security/sandbox/)) |
| GitHub | A fine-grained token on the VM, narrowed to the repositories agents work on, plus branch protection | A GitHub App credential: read tokens, and pushes Legato checks branch by branch (see [Setting up credentials](/legato/security/credentials-setup/) § 5. GitHub) |
| Permission prompts | Off: agents run with their tools' skip-permission flags | Can stay off too; the sandbox and the credential rules are the check |
| Setup | Short | Needs bubblewrap and a GitHub App |

Start with the VM as the boundary: give the VM nothing you would mind an agent using. Turn the sandbox on when the server holds more than agents should see.

## 1. The machine

- Any Linux with a user of its own for Legato (`agent` below). Put nothing on it that is not for agents: no personal SSH keys, no browser logins, no cloud credentials.
- Install `tmux`, `git`, the agent tools you want (`claude`, `codex`, `chimera`), and the toolchains your projects build with.
- Install `legato` from the latest release, which keeps itself up to date (see § 5 and [Releases and updates](/legato/running/updates/)):

  ```bash
  mkdir -p ~/.local/bin
  curl -fL -o ~/.local/bin/legato https://github.com/cpave3/legato/releases/latest/download/legato-linux-amd64   # or legato-linux-arm64
  chmod +x ~/.local/bin/legato && legato version
  ```

  A binary built from a checkout (`task build`) works too, but says it is `dev` and never updates itself.
- For agents that build stacks of branches, install [Staccato](/legato/integrations/staccato/)'s `st` the same way. Legato keeps it up to date with its own updates:

  ```bash
  curl -fL -o ~/.local/bin/st https://github.com/cpave3/staccato/releases/latest/download/st-linux-amd64   # or st-linux-arm64
  chmod +x ~/.local/bin/st && st version
  ```

  If your login shell is zsh, it does not read `~/.profile`, where `~/.local/bin` is usually added to `PATH`. Add `emulate sh -c '. $HOME/.profile'` to `~/.zprofile`.
- For the sandbox later: `bwrap` (`dnf install bubblewrap`, `apt install bubblewrap`). It needs unprivileged user namespaces, which most distributions allow.

## 2. Reaching it: Tailscale

Put the machine on your tailnet (`tailscale up`) and reach Legato by its tailnet name (`legato-vm.example.ts.net`). Nothing is opened to the internet.

A browser treats a page as secure only over HTTPS at a host name, and passkeys and the installable web app need that. Tailscale gives the machine a real certificate:

```bash
sudo tailscale cert --cert-file ~/.config/legato/tls.crt --key-file ~/.config/legato/tls.key legato-vm.example.ts.net
sudo chown agent: ~/.config/legato/tls.*
chmod 600 ~/.config/legato/tls.key
```

The certificate lasts 90 days; run the command again from a monthly timer. Legato reads the files when it starts.

## 3. Logging the agent tools in without a browser

Log in over SSH, as the `agent` user, inside `tmux` so a long login survives a dropped connection.

- **Claude Code.** Run `claude`, choose to log in, open the address it prints on any device, and paste the code back. The login is kept in `~/.claude`, as on a desktop, and every agent Legato starts uses it. Or run `claude setup-token`, which signs in the same way and prints a token for your subscription that lasts about a year, and put it in the service's environment file (step 5) as `CLAUDE_CODE_OAUTH_TOKEN`. The token never needs renewing, so an unattended VM never finds its agents stopped at a login prompt.
- **Codex.** `codex login --device-auth` prints an address and a code to enter on any device. With an API key: `printenv OPENAI_API_KEY | codex login --with-api-key`.
- **Chimera.** Log in as its own documentation says; its state is in `~/.chimera`.

Start each tool once by hand in a project folder to answer its first-run questions (theme, trust for the folder), which an agent started by Legato would otherwise stop at.

## 4. Configuration

`~/.config/legato/config.yaml`:

```yaml
web:
  port: "3080"
  public_url: https://legato-vm.example.ts.net:3080   # the address in notification links
  tls:
    cert: /home/agent/.config/legato/tls.crt
    key: /home/agent/.config/legato/tls.key

board:
  columns:
    - name: Backlog
    - name: Doing
    - name: Review
    - name: Done

repository_cache:
  enabled: true                     # offer "Clone" in the spawn dialog
  keep_idle: 7d                     # keep a clone this long for the next agent

# More can be added in the web UI: Settings › Agents › Repositories.
repositories:                       # what jobs and the workflow form offer
  - name: Legato
    repo: cpave3/legato             # no primary_dir: each job gets a fresh clone
    base_branch: main

adapters:
  claude-code:
    launch_args: ["--dangerously-skip-permissions"]
  codex:
    launch_args: ["--dangerously-bypass-approvals-and-sandbox"]

notifications:
  ntfy:
    topic: legato-vm-<something hard to guess>   # finished agents, agents that need you, approvals
```

Then, once: `legato columns sync`. A database with no columns has nowhere to put a task.

A clone Legato makes for an agent is set up before the agent starts, from the repository's own `.legato.toml` (or its `.yggdrasil.toml`'s `post_create` hooks): its dependencies installed in the agent's sandbox, with the package caches the sandbox shares. Files a repository copies or links from its main checkout (`.env.local`) come from the repository's `primary_dir`, which a worker usually has none of; give agents such files another way (a resource, or a credential). See [Configuration](/legato/reference/configuration/) § Setting up a fresh clone.

The skip-permission flags are what let agents work while nobody watches. Use them only where the VM, or the sandbox, is the boundary. Leave them out and every permission request waits for you in the web UI, the phone app and ntfy.

## 5. Running it: a systemd user service

`~/.config/systemd/user/legato.service`:

```ini
[Unit]
Description=Legato
After=network-online.target

[Service]
ExecStart=%h/.local/bin/legato serve --port 3080
EnvironmentFile=-%h/.config/legato/env
Environment=PATH=%h/.local/bin:%h/.local/share/mise/shims:/usr/local/bin:/usr/bin
Restart=on-failure
# An update makes Legato exit with 75 to be started again on the new binary.
RestartForceExitStatus=75
# Stop only Legato: the tmux server it starts, and the agents in it, keep
# running across a restart, and Legato finds them again when it starts.
KillMode=process

[Install]
WantedBy=default.target
```

`~/.config/legato/env` (mode `0600`) holds what the agent tools read from the environment, such as `CLAUDE_CODE_OAUTH_TOKEN=…`. Everything in it reaches every agent.

```bash
loginctl enable-linger agent          # run user services with nobody logged in
systemctl --user daemon-reload
systemctl --user enable --now legato
journalctl --user -u legato -f        # its log
```

`PATH` must hold every agent tool and toolchain: a service does not read your shell's profile.

### The log file

Legato also writes its log to a file in its data folder, `~/.local/share/legato/logs/legato.log` unless `data_dir` says otherwise, whether it runs as `legato serve`, as the TUI, or under hot reload. `legato serve` still logs to stderr too, so `journalctl` shows the same lines; the TUI, which owns its terminal, writes only to the file. The file is rotated at 10 MB, and the three files before it are kept as `legato.log.1` (the newest) to `legato.log.3`.

### Updates

A release of Legato updates itself. Settings › Updates chooses how: **Ask** (the default) shows a bar and an Attention item with **Update and restart**; **Automatic** installs a new release once no agent is in the middle of a turn (after three hours, whatever they are doing); **Off** never looks. An update replaces `~/.local/bin/legato` (the old one is kept as `legato.prev`) and Legato exits with status 75, which this unit starts again. `KillMode=process` keeps tmux and the agents running across it. See [Releases and updates](/legato/running/updates/).

## 6. GitHub

### With the VM as the boundary

Make a [fine-grained personal access token](https://github.com/settings/personal-access-tokens/new) for the repositories agents work on only, with **Contents** read and write, **Pull requests** read and write, and **Metadata** read. On the VM:

```bash
gh auth login --with-token < token.txt && rm token.txt
gh auth setup-git                     # git pushes and fetches with the token
git config --global user.name "Legato agent"
git config --global user.email "agent@example.com"
```

A fine-grained token cannot be narrowed to branches. Protect what agents must not push to: in each repository's settings, a ruleset for the default branch that requires a pull request and blocks force pushes. Agents then push branches and open pull requests, and you merge.

Give Legato's own pull request tracking the same token with `github.auth: gh` (the default `auto` finds it).

### With Legato's sandbox

Turn the sandbox on (`agents.sandbox.enabled: true`) and give agents GitHub through a GitHub App instead of a token on the VM: [Setting up credentials on a host](/legato/security/credentials-setup/) § 5. GitHub. Agents then get read tokens for the listed repositories, push only branches the rules allow, and never hold the App's key. Remove the personal token from the VM (`gh auth logout`) so nothing on it outranks the credential.

With the sandbox on, the skip-permission flags in step 4 are reasonable: an agent cannot read the VM's other files or Legato's state, and every push and credential use goes through Legato's rules.

## 7. Adding the server to your apps

The web UI on your laptop and the phone app each keep a list of servers.

- **Phone.** On the VM, `legato pair --host legato-vm.example.ts.net`. It prints a QR code and four words; scan the code in the Legato app, or type the words. The phone gets the server's address, its token and its certificate, and the command ends when it is paired.
- **Web UI elsewhere.** Settings › Connection › Servers: add `https://legato-vm.example.ts.net:3080` with a name. When you switch to it, the UI asks for its token: on the VM, `legato auth token`.
- **Web UI on the server itself.** Open `https://legato-vm.example.ts.net:3080` in a browser on your tailnet and give it the token.

The server switcher at the foot of the rail (and in **More** on a phone) moves between your laptop's Legato and this one.

## 8. Starting a job

From the web UI or the phone: **Spawn agent**, pick the agent tool, switch **Where** to **Clone** (`repository_cache.enabled: true` in step 4), pick the repository and **New clone**, give a branch (blank for `legato/<task id>`) and a base branch, and write the job in **First message**. The dialog closes at once and the agent is listed under **Starting** while its clone is made, with git's progress when the repository is still being copied; it starts as soon as the clone is ready. Legato copies every repository it offers when it starts, and one added in Settings at once, so a large repository's first copy (minutes, for a gigabyte) is usually done before you need it; Settings › Agents › Repositories shows how each copy stands. For a run record, run **Agent job** (`legato-job`) from **Workflows** instead.

**A stack of branches in one folder.** Work you would do as a stack (one branch on top of another, each its own task and agent) can stay in one clone, with its dependencies and build output. Start the first part in a fresh clone. For each next part, **Spawn agent** on its task, switch **Where** to **Clone**, pick the repository and, under **Clone**, the clone (named by its branch) instead of New clone, and give the new branch: it is made at the clone's current commit, stacked on the branch before it. Leave Branch empty to stay on the clone's branch. Restacking (`st restack`, a rebase on main) is the agents' work, in the same folder. A task's later agents (a restart, a new agent on the same task) work in its clone again by themselves.

Two agents can work in one clone at once, for example one reviewing while another builds. The second joins on the clone's branch (Legato will not move the branch under the first), and the two see each other as peers and can message each other. A clone no agent has worked in for `repository_cache.keep_idle` (7 days) is removed if everything in it is pushed; Settings › Agents › Clones lists them and removes one early.

By API, with the token from `legato auth token`:

```bash
SERVER=https://legato-vm.example.ts.net:3080
TOKEN=…   # legato auth token, on the VM

# A job with a run record: legato-job, in a fresh clone of the repository.
# It shows under Workflows, and the agent stays running when it reports its
# summary, so you can read it and go on talking to it.
curl -sS -X POST "$SERVER/api/workflows/legato-job/runs" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"inputs": {"prompt": "Find why TestSync is flaky and open a PR with the fix.",
                  "repo": "cpave3/legato", "agent": "claude-code"}}'

# Or a plain agent, with no run record:
curl -sS -X POST "$SERVER/api/agents/spawn" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"title": "Flaky sync test", "agent_kind": "claude-code",
       "repo": "cpave3/legato", "prompt": "Find why TestSync is flaky."}'
```

- `legato-job` takes `prompt` (required), `agent` (a configured agent tool; another name is refused before anything starts), and either `repo` (with `ref`, a branch, tag or commit; the default branch when empty) or `working_dir`. With `repo`, the agent works in a fresh clone of the repository made for it, from Legato's cache (`repository_cache.max_age`, 5 minutes by default), so every job starts from the current base and never from another job's leftovers. This needs the GitHub App credential (step 6) listing the repository. See [Workflows](/legato/using/workflows/) § The agent job workflow.
- `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent` take `repo` and `ref` the same way, instead of `working_dir`, or `clone_id` (from `GET $SERVER/api/job-clones`) to work in a clone that exists, with `branch` for a new branch stacked there.
- `GET $SERVER/api/workflow-runs?workflow_name=legato-job` lists the jobs; `GET $SERVER/api/workflow-runs/<run id>` shows one, with the agent's `summary` once it has reported.
- From a script on the VM itself: `legato workflow run legato-job --input prompt="…" --input repo=cpave3/legato --json`.

Then open the agent in the web UI or the phone app to watch it, answer its questions and approve what it asks.

## 9. Keeping it running

- **Updates.** Replace the binary and `systemctl --user restart legato`. Agents keep running (`KillMode=process`); Legato binds their native panels again when it starts.
- **Disk.** Legato keeps a job's clone for `repository_cache.keep_idle` (7 days) after an agent last worked in it, for the next agent of a stack, then removes it if everything in it is pushed; one with unpushed commits is kept seven days more with a note on the task. Build output stays with a clone (Rust's `target/` can be several GB), so lower `keep_idle` on a small disk, or remove clones in Settings › Agents › Clones.
- **Build caches.** Fresh clones start without build output. Shared caches make their first build warm: Go's is shared already; for Rust set `RUSTC_WRAPPER=sccache` in `~/.config/legato/env` (the sandbox mounts `~/.cache/sccache`); pnpm and Gradle share theirs by default. The cache (`~/.local/share/legato/repos`) holds one bare clone per repository. Checkouts you made yourself are yours to clean.
- **Logins.** Agent tools' logins expire. When an agent stops at a login prompt, attach to it (`tmux attach -t legato-<task>` over SSH, or **Open terminal** in the web UI) and log in again.
- **Backups.** The database and the data folder are under `~/.local/share/legato`.
