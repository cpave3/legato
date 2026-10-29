# Agents on a server of their own

Legato can run on an always-on machine (a VM, a spare box) with nobody at its terminal. Agents work there while your laptop is closed: you start them from the web UI or the phone app, or by API, and you watch and answer them from either. This guide sets one up.

The steps are written from Legato's code and have not yet been run end to end on a fresh VM (task 1.8 of `openspec/changes/sandboxed-cloud-agents`). Where a step is wrong, fix it here.

## What protects what

There are two ways to keep agents on the server from doing harm, and you can use both.

| | The VM is the boundary | Legato's sandbox |
| --- | --- | --- |
| What an agent can reach | Everything the VM's user can: its files, its tokens | Its work, its tool's login, and Legato's sockets for it (see [Sandbox](ai/sandbox.md)) |
| GitHub | A fine-grained token on the VM, narrowed to the repositories agents work on, plus branch protection | A GitHub App credential: read tokens, and pushes Legato checks branch by branch (see [Setting up credentials](credentials.md) § 5. GitHub) |
| Permission prompts | Off: agents run with their tools' skip-permission flags | Can stay off too; the sandbox and the credential rules are the check |
| Setup | Short | Needs bubblewrap and a GitHub App |

Start with the VM as the boundary: give the VM nothing you would mind an agent using. Turn the sandbox on when the server holds more than agents should see.

## 1. The machine

- Any Linux with a user of its own for Legato (`agent` below). Put nothing on it that is not for agents: no personal SSH keys, no browser logins, no cloud credentials.
- Install `tmux`, `git`, the agent tools you want (`claude`, `codex`, `chimera`), and the toolchains your projects build with.
- Install `legato` (`task build` on a checkout, or copy a binary to `~/.local/bin/legato`).
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

- **Claude Code.** `claude setup-token` prints a long-lived token for your subscription. Put it in the service's environment file (step 5) as `CLAUDE_CODE_OAUTH_TOKEN`. Or run `claude`, choose to log in, open the address it prints on any device, and paste the code back.
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

repositories:                       # what the spawn dialogs offer as places to work
  - name: Legato
    repo: cpave3/legato
    primary_dir: /home/agent/src/legato
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

Turn the sandbox on (`agents.sandbox.enabled: true`) and give agents GitHub through a GitHub App instead of a token on the VM: [Setting up credentials on a host](credentials.md) § 5. GitHub. Agents then get read tokens for the listed repositories, push only branches the rules allow, and never hold the App's key. Remove the personal token from the VM (`gh auth logout`) so nothing on it outranks the credential.

With the sandbox on, the skip-permission flags in step 4 are reasonable: an agent cannot read the VM's other files or Legato's state, and every push and credential use goes through Legato's rules.

## 7. Adding the server to your apps

The web UI on your laptop and the phone app each keep a list of servers.

- **Phone.** On the VM, `legato pair --host legato-vm.example.ts.net`. It prints a QR code and four words; scan the code in the Legato app, or type the words. The phone gets the server's address, its token and its certificate, and the command ends when it is paired.
- **Web UI elsewhere.** Settings › Connection › Servers: add `https://legato-vm.example.ts.net:3080` with a name. When you switch to it, the UI asks for its token: on the VM, `legato auth token`.
- **Web UI on the server itself.** Open `https://legato-vm.example.ts.net:3080` in a browser on your tailnet and give it the token.

The server switcher at the foot of the rail (and in **More** on a phone) moves between your laptop's Legato and this one.

## 8. Starting a job

From the web UI or the phone: **Spawn agent**, pick the agent tool and a preset, and write the job in **First message**. The agent starts on it as soon as it is ready.

By API, with the token from `legato auth token`:

```bash
SERVER=https://legato-vm.example.ts.net:3080
TOKEN=…   # legato auth token, on the VM

# A job with a run record: legato-job. It shows under Workflows, and the
# agent stays running when it reports its summary, so you can read it and
# go on talking to it.
curl -sS -X POST "$SERVER/api/workflows/legato-job/runs" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"inputs": {"prompt": "Find why TestSync is flaky and open a PR with the fix.",
                  "working_dir": "/home/agent/src/legato", "agent": "claude-code"}}'

# Or a plain agent, with no run record:
curl -sS -X POST "$SERVER/api/agents/spawn" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"title": "Flaky sync test", "agent_kind": "claude-code",
       "working_dir": "~/src/legato", "prompt": "Find why TestSync is flaky."}'
```

- `legato-job` takes `prompt` (required), `agent` (a configured agent tool; another name is refused before anything starts), and `working_dir`. `repo` and `ref` will check a repository out for the job; until that is built, a non-empty `repo` fails the run, so clone the repository on the VM and name the clone as `working_dir`. See [Workflows](workflows.md) § The agent job workflow.
- `GET $SERVER/api/workflow-runs?workflow_name=legato-job` lists the jobs; `GET $SERVER/api/workflow-runs/<run id>` shows one, with the agent's `summary` once it has reported.
- From a script on the VM itself: `legato workflow run legato-job --input prompt="…" --input working_dir=… --json`.

Then open the agent in the web UI or the phone app to watch it, answer its questions and approve what it asks.

## 9. Keeping it running

- **Updates.** Replace the binary and `systemctl --user restart legato`. Agents keep running (`KillMode=process`); Legato binds their native panels again when it starts.
- **Disk.** Each agent's checkout is yours to clean: Legato does not remove folders it did not make.
- **Logins.** Agent tools' logins expire. When an agent stops at a login prompt, attach to it (`tmux attach -t legato-<task>` over SSH, or **Open terminal** in the web UI) and log in again.
- **Backups.** The database and the data folder are under `~/.local/share/legato`.
