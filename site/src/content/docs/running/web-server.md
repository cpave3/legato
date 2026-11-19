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

The first time it starts, on an empty database, it asks nothing. It makes the board's columns and prints the checks of [`legato doctor`](/legato/reference/cli/#running-legato) with their fixes. It also prints four words to pair the Legato app with. Finish setting Legato up from your phone or a browser, in Settings.

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

A service starts `legato serve` for you and starts it again when it stops. Use systemd on Linux and launchd on macOS.

### Linux: a systemd user service

A systemd user service keeps Legato running after you log out. Save this as `~/.config/systemd/user/legato.service`:

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

### macOS: a launchd user agent

A launchd user agent starts Legato when you log in to your Mac and starts it again when it fails. It runs while you are logged in; it does not run before you log in, and it sleeps when the Mac sleeps.

launchd does not expand `~` or `$HOME` in a property list, so every path in it must be absolute. This command writes the file with your home folder filled in:

```bash
mkdir -p ~/Library/LaunchAgents ~/Library/Logs
cat > ~/Library/LaunchAgents/com.legato.serve.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.legato.serve</string>
  <key>ProgramArguments</key>
  <array>
    <string>$HOME/.local/bin/legato</string>
    <string>serve</string>
    <string>--port</string>
    <string>3080</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>AbandonProcessGroup</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$HOME/Library/Logs/legato-serve.log</string>
  <key>StandardErrorPath</key>
  <string>$HOME/Library/Logs/legato-serve.log</string>
</dict>
</plist>
EOF
plutil -lint ~/Library/LaunchAgents/com.legato.serve.plist
```

- `PATH` must hold tmux, every agent tool and every toolchain your agents use. A launchd agent does not read your shell's profile. Keep `/usr/bin` in it: Legato runs `security`, which is in `/usr/bin`, to reach the keychain.
- If your shell profile sets `LEGATO_CONFIG`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_RUNTIME_DIR` or `TMUX_TMPDIR`, add them to `EnvironmentVariables` too. Without them, the service reads a different configuration and database from the one your terminal uses, `legato restart` from a terminal does not reach it, or its agents run in a different tmux server.
- Variables your agent tools read from the environment, such as `CLAUDE_CODE_OAUTH_TOKEN`, also go in `EnvironmentVariables`: launchd has no environment file like systemd's `EnvironmentFile`. Every agent sees them. When the file holds a token, make it readable by you only with `chmod 600 ~/Library/LaunchAgents/com.legato.serve.plist`.
- `RunAtLoad` starts Legato when the agent is loaded, and so at every login.
- `KeepAlive` with `SuccessfulExit` set to false starts Legato again when it exits with an error or crashes. launchd waits at least 10 seconds between starts. When you stop the agent, or Legato exits cleanly, launchd leaves it stopped. `kill` without `-9` is a clean exit: Legato shuts down and exits with 0.
- `AbandonProcessGroup` stops launchd from killing the processes Legato started when Legato stops. tmux and the agents in it keep running, and Legato finds them again when it starts. This does what `KillMode=process` does on Linux.
- launchd writes Legato's output to `~/Library/Logs/legato-serve.log`. Legato also keeps its own log in its data folder, `~/.local/share/legato/logs/legato.log` unless `data_dir` says otherwise.

Load and start it, then manage it:

```bash
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.legato.serve.plist   # load and start it
launchctl kickstart -k gui/$(id -u)/com.legato.serve   # stop it and start it again
launchctl bootout gui/$(id -u)/com.legato.serve        # stop it and unload it
tail -f ~/Library/Logs/legato-serve.log                # follow its log
```

After you edit the property list, run `bootout`, then `bootstrap` again. launchd reads the file only when it loads the agent.

On a Mac, also note:

- **No sandbox.** Legato's sandbox is bubblewrap, which runs on Linux only. Leave `agents.sandbox.enabled` off. With it on, every agent that would start in the sandbox fails to start, saying that bwrap is not installed, and swarm and workflow agents always start in it. Without the sandbox, a Mac cannot run [the Reeve](/legato/using/reeve/), and as a [worker instance](/legato/running/instances/) it refuses every handoff. See [The sandbox](/legato/security/sandbox/).
- **The SSH agent.** macOS gives every process in your login session, launchd agents included, an `SSH_AUTH_SOCK` for its own ssh-agent. Legato uses 1Password's agent by itself only when `SSH_AUTH_SOCK` is empty and `~/.1password/agent.sock` exists, so on a Mac it keeps the macOS agent. To use 1Password's SSH agent, name its socket in `config.yaml`:

  ```yaml
  ssh_agent_socket: "${HOME}/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"
  ```

- **The secret store's key.** With `secrets.master_key: auto`, the default, Legato keeps the key of its secret store in your keychain on macOS. A launchd agent runs in your login session, so it reads the same keychain item as a Legato you start in a terminal. Legato makes the key the first time it uses its store and records where it put it, and from then on it reads the key from that place only. If `security` is not on `PATH` when the key is made, `auto` makes the key from `LEGATO_SECRETS_PASSPHRASE` when that is set, and otherwise keeps it in a file in the data folder. If `security` is missing later, Legato cannot read the secrets in its store. See `secrets.master_key` in the [configuration reference](/legato/reference/configuration/).
- **The TUI beside it.** The TUI and the service share the configuration, the database and the agents. Leave `web.enabled` off (the default) so the TUI does not take the service's port. If a TUI with `web.enabled: true` holds the port, `legato serve` exits with an error, and launchd tries again every 10 seconds until the port is free.
- **Updates.** Legato exits with status 75 to restart on an update only as a systemd service. Under launchd, an update installs the new binary and the web UI says "Legato v… is installed. Restart Legato to use it." This is so in the Automatic mode too. Choose **Restart Legato** in Settings › Updates, or run `legato restart`: Legato starts again in place on the new binary, in the same process, so launchd sees no change. `launchctl kickstart -k` also works. See [Updates](/legato/running/updates/).

## Check that it runs

`GET /health` answers without a token, so a monitor can use it:

```bash
curl http://localhost:3080/health
```

```json
{"status":"ok","version":"v0.4.2"}
```

The version is the one `legato version` prints.
