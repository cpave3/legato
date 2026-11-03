---
title: Web server only
description: Run Legato's web server without the TUI, on a server or a machine with no terminal open.
sidebar:
  order: 20
---

`legato serve` runs Legato's web server and its agents without the TUI. Use it when nobody sits at a terminal on the machine: a server, a VM, a spare box, or your workstation when you only want the web UI and your phone.

It is the same Legato as the TUI. It uses the same configuration and database, starts agents in tmux, and serves the same web UI.

## Start it

```bash
legato serve
```

It prints the address and keeps running until you stop it with `ctrl+c`:

```
Legato web UI: http://localhost:3080
```

`legato serve` listens on `web.port` (3080 by default). `--port` overrides it for one run:

```bash
legato serve --port 4000
```

It reads the rest of the `web` settings: `web.tls.*` for HTTPS, `web.discovery` to announce itself on the local network, and `web.public_url` for the links in notifications. See [Your phone and other devices](/legato/running/devices/).

Do not run `legato serve` and a TUI with `web.enabled: true` on the same port. Whichever starts second cannot take the port.

## Sign in from a browser

The web server makes a token the first time it starts and checks it on every request. Open the address in a browser, and paste the token when the page asks:

```bash
legato auth token
```

To pair a phone instead, run `legato pair` while the server runs. See [Web access and tokens](/legato/security/web-auth/).

## Run it as a service

On Linux, a systemd user service keeps Legato running after you log out and starts it again when it stops. Save this as `~/.config/systemd/user/legato.service`:

```ini
[Unit]
Description=Legato
After=network-online.target

[Service]
ExecStart=%h/.local/bin/legato serve --port 3080
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin
Restart=on-failure
RestartForceExitStatus=75
KillMode=process

[Install]
WantedBy=default.target
```

- `PATH` must hold every agent tool and toolchain your agents use. A service does not read your shell's profile.
- `RestartForceExitStatus=75` lets Legato restart itself after an [update](/legato/running/updates/).
- `KillMode=process` stops only Legato. tmux and the agents in it keep running, and Legato finds them again when it starts.

Then start it:

```bash
loginctl enable-linger "$USER"     # run user services with nobody logged in
systemctl --user daemon-reload
systemctl --user enable --now legato
journalctl --user -u legato -f     # follow its log
```

For a whole machine set up for agents, with Tailscale, logins for the agent tools, GitHub and the sandbox, follow [Agents on a server of their own](/legato/running/agent-server/).

## Check that it runs

`GET /health` answers without a token, so a monitor can use it:

```bash
curl http://localhost:3080/health
```

```json
{"status":"ok","version":"v0.4.2"}
```

The version is the one `legato version` prints.
