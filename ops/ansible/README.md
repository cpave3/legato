# A Legato worker, with Ansible

`worker.yml` sets up a fresh Ubuntu LTS machine (24.04 or later, amd64 or arm64) as a Legato worker: an always-on Legato that runs agents in its sandbox, for tasks you hand off from your main instance or start from its web UI. It follows the walk-through in [Agents on a server of their own](https://cpave3.github.io/legato/running/agent-server/) and the files in `docs/examples/cloud-vm/`.

It puts no secret on the machine. Logins, tokens and keys are steps for you afterwards (below).

## Run it

You need Ansible on your own machine (`pipx install ansible-core`, or `uvx --from ansible-core ansible-playbook …`) and SSH to the machine as a user that can `sudo`.

```bash
cd ops/ansible
cp inventory.example.yml inventory.yml     # name your machine; inventory.yml is not committed
ansible-playbook worker.yml                # add -K if sudo asks for a password
```

Running it again is safe: what is there is left alone, and Legato, which updates itself, is not replaced (see `legato_force_download`).

Every setting is a variable in [`roles/legato/defaults/main.yml`](roles/legato/defaults/main.yml), with what it does. Set them per host in `inventory.yml`.

## What it sets up

- **Packages**: build tools (`build-essential`, `clang`, `mold`, `pkg-config`, `libssl-dev`), `tmux`, `git`, `git-lfs`, `bubblewrap`, `zsh`, `jq`, `ripgrep`, `fd-find`, `sqlite3`, Python, the libraries for building Tauri apps, the QEMU guest agent, and GitHub CLI and Tailscale from their own repositories. Neovim from its latest release.
- **The sandbox**: Ubuntu blocks unprivileged user namespaces through AppArmor, so an AppArmor profile lets only `/usr/bin/bwrap` make them, and a check runs bubblewrap once. A worker refuses handoffs without the sandbox.
- **The user**: zsh as the login shell with Oh My Zsh, `~/.profile` putting `~/.local/bin`, mise's shims and pnpm's tools on `PATH` (read by zsh through `~/.zprofile`), and lingering, so user services run with nobody logged in.
- **Toolchains** from mise (`~/.config/mise/config.toml`): Node 24, Python 3.12, Go, Rust, Bun, pnpm, Yarn 1, pipx, Lua, sccache, .NET 10 (and 9), plus `task` and `golangci-lint`, and uv. Rust builds share crates through sccache and link with mold.
- **Agent tools**, not logged in: Claude Code and Codex with their installers, Chimera built from source and linked with pnpm, and Staccato's `st`.
- **Legato**: the latest release, checked against its `SHA256SUMS` (or a binary of yours: `legato_binary_src`), `~/.config/legato/config.yaml` written once from the template, `~/.config/legato/env` (mode 0600) with build settings and no tokens, and the systemd user service, started and enabled. The board's columns are synced, and Chimera's hooks installed.
- **Metrics**: Prometheus node exporter, and a timer that writes Legato's health, its unit's state and its agents to node exporter's textfile collector every 15 seconds (`legato_metrics: false` to leave out).

## Then, on the machine

Over SSH as the Legato user, inside `tmux` so a long login survives a dropped connection:

1. **Agent tools.** Log each in and start it once in a folder to answer its first questions: `claude` (or put a `claude setup-token` token in `~/.config/legato/env` as `CLAUDE_CODE_OAUTH_TOKEN`), `codex login --device-auth`, and Chimera's `~/.chimera/config.json` with your providers.
2. **Tailscale.** `sudo tailscale up`.
3. **GitHub.** Make a GitHub App and add its key to Legato's store (`legato secret add github-app --type keypair --from <app>.private-key.pem`), then uncomment the `credentials` block in `config.yaml`. For review passes, a second App and the `review_passes` block. See [Setting up credentials](https://cpave3.github.io/legato/security/credentials-setup/) and [Review passes](https://cpave3.github.io/legato/using/review-passes/).
4. **Restart Legato** after changing its configuration: `systemctl --user restart legato`.
5. **Pair it.** On your main instance, Settings › Handoff › Workers › **Add a worker**; see [Main and worker instances](https://cpave3.github.io/legato/running/instances/). For your phone, `legato pair --host <its address>`.

## Not included

- Secrets of any kind: the GitHub Apps' keys, a 1Password token, agent tools' logins and API keys, an ntfy topic of yours (`legato_ntfy_topic`).
- Your own checkouts and private tools (`~/Projects`, a skills registry, koru): copy those over if agents need them.
- Shipping metrics anywhere: node exporter listens on port 9100 for your Prometheus, Grafana Alloy or similar.
