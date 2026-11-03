---
title: Updates
description: How a release of Legato keeps itself up to date, and how to choose when it does.
sidebar:
  order: 60
---

A Legato you installed from a GitHub release updates itself to the newest stable release. You choose whether it asks first, updates on its own, or never looks. A Legato built from source never updates itself.

## Which version you run

```bash
legato version
```

```
legato v0.4.2
```

A build from source says `legato dev`. `legato version --json` also says whether this is a release build.

## Choose how it updates

Open Settings › Updates in the web UI. It shows this server's version and the latest release, and the mode:

| Mode | What happens |
| --- | --- |
| **Ask** (the default) | When a release is out, a bar along the top of the web UI says so, with **Update and restart** and **Later**. Attention lists it too. Nothing installs until you choose. |
| **Automatic** | Legato installs a new release once no agent is in the middle of a turn. An agent that waits for you does not hold it back. After three hours it installs whatever the agents are doing. |
| **Off** | Legato never looks for a release. |

The mode is saved in Legato's database, not in `config.yaml`, so you set it in the web UI only. **Check now** looks at once. **Later** hides the bar in that browser until the next release.

Legato looks for a release a minute after it starts, then every six hours, and at once when you change the mode. It asks GitHub for the latest release of `cpave3/legato`, which is never a draft or a prerelease.

:::note
The updater runs in the process that runs the web server: `legato serve`, or a TUI with `web.enabled: true`. A TUI without a web server does not look for updates.
:::

## What an update does

1. Legato downloads the binary for your system and processor into the folder the running binary is in. When `legato` is a link, it follows the link.
2. It checks the download against the release's checksums.
3. It runs the new binary and checks that it reports the release's version.
4. It keeps the running binary as `legato.prev` and puts the new one in its place.

If any step fails, the running binary stays as it was. The web UI says why, and Attention offers **Try again**. Legato must be able to write to the folder its binary is in.

## Restarting

A new binary takes effect when Legato starts again. How that happens depends on how Legato runs:

- **As a systemd service**: Legato stops by itself a second later and exits with status 75. A unit with `Restart=on-failure` and `RestartForceExitStatus=75` starts it again on the new binary. With `KillMode=process`, tmux and your agents keep running and Legato finds them again. See [Web server only](/legato/running/web-server/) for the unit.
- **In a terminal**, the TUI or `legato serve` you started by hand: Legato does not quit from under you. The bar says the new version is installed and asks you to restart Legato.
- **Under `--hot-reload`**, the development mode: the supervisor sees the new binary and restarts Legato.

Agents keep running across the restart in every case, since they live in tmux.

## Going back

If a new release does not start, put the old binary back:

```bash
mv ~/.local/bin/legato.prev ~/.local/bin/legato
```

Use the path your `legato` is at. This brings back the binary only. A new release may have changed the database, and an older binary cannot undo that.

## Builds from source

A binary you built yourself (`task build`) says it is `dev` and never updates itself. Settings › Updates says so. This lets a checkout you develop in run beside a machine that runs releases. See [Building Legato](/legato/contributing/build/).

## Staccato's `st`

Legato also keeps [Staccato](/legato/integrations/staccato/)'s `st` up to date, when the `st` on Legato's `PATH` is a release (`st version` says a `vX.Y.Z`). It checks `st` whenever it checks for itself, in the same mode:

- **Automatic** runs `st update` as soon as a newer `st` is out. It doesn't wait for agents: `st` is replaced in place, and nothing restarts.
- **Ask** shows the release in Settings › Updates, with **Update st to …**, and in Attention.
- **Off** never looks.

`st` downloads and checks its own release; Legato only runs `st update`. An `st` built from source is left alone, and Legato never installs `st` where there is none. A Legato built from source still keeps a release of `st` up to date.

## Limits

- Agents also run the `legato` binary, for their hooks and Legato's tools. After an update, new calls use the new binary while processes already running keep the old one, until the agent is reloaded.
- The TUI's own Attention queue does not list an update. The web UI's does.
