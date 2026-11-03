---
title: Stream Deck
description: Show your agents and Attention items on Stream Deck keys through OpenDeck on Linux, and open them with a press.
sidebar:
  order: 90
---

:::caution
The Stream Deck plugin is experimental. It runs only in [OpenDeck](https://github.com/nekename/OpenDeck) on Linux, and you build it from source.
:::

The Stream Deck plugin puts Legato on the keys of a Stream Deck. One key shows each running agent, with its task's title and what it is doing. Another set of keys shows your [Attention](/legato/using/attention/) items. Press a key to open the agent or the item in the web UI.

## What the keys show

- **Agent tiles** show the task's title, in green while the agent works, amber while it waits for you and grey while it is idle. A press opens the agent at `/agents?task=<task-id>` in your browser.
- **Attention tiles** show one Attention item each: plans to approve, reviews an agent marked ready, pull requests that can't merge, agents waiting or in error, and workflows that need input. A press opens the item in your browser. When the item's main action is safe to run without asking, such as starting a workflow step, the press runs it instead. A press never approves a plan or marks a review done.

Each profile has 15 tiles. Agents and items past the fifteenth are not shown.

When Legato stops, the tiles show `OFFLINE`. The plugin tries again, waiting longer each time up to 30 seconds, and refreshes the tiles within a second or two of Legato coming back.

## Build and install

You need Go, OpenDeck, and a Legato [web server](/legato/running/web-server/) that the plugin can reach.

1. In the Legato repository, build the plugin folder:

   ```bash
   task streamdeck:package
   ```

   This makes `streamdeck/dist/com.cpave3.legato.sdPlugin/`.

2. Copy the folder into OpenDeck's plugins folder. For OpenDeck installed from Flatpak:

   ```bash
   cp -r streamdeck/dist/com.cpave3.legato.sdPlugin \
     ~/.var/app/me.amankhanna.opendeck/config/opendeck/plugins/
   ```

   For an OpenDeck installed another way, the folder is `~/.local/share/opendeck/plugins/`.

3. Restart OpenDeck, or reload its plugins from **Settings › Plugins**.

`task streamdeck:build` builds only the binary. `task streamdeck:dev` builds it into an installed Flatpak plugin and restarts the plugin.

To update, package again and copy the new files over the installed folder. OpenDeck keeps your key layouts in its `profiles` folder, apart from the plugin, so they survive the update. Keep the plugin folder's name the same.

## Connect it to Legato

The plugin needs Legato's address and its web token. Print the token with:

```bash
legato auth token
```

Put both in a `streamdeck.yaml` file:

```yaml
legato_url: "http://localhost:3080"
token: "your-auth-token"
```

The plugin reads the first of these it finds:

1. `streamdeck.yaml` in the installed `.sdPlugin` folder.
2. `streamdeck.yaml` in the folder the plugin starts in.
3. `~/.config/legato/streamdeck.yaml`.

The environment variables `LEGATO_URL` and `LEGATO_TOKEN` override the file. `legato_url` is `http://localhost:3080` when you leave it out.

:::note
OpenDeck from Flatpak runs plugins in its sandbox, where they can't read your home folder. Put `streamdeck.yaml` in the installed `.sdPlugin` folder. If you create `streamdeck/plugin/streamdeck.yaml` before you package, `task streamdeck:package` copies it into the folder for you.
:::

## Set up the profiles

Make three profiles in OpenDeck:

1. **Legato**, with two keys that use OpenDeck's **Switch Profile** action: one to **Legato Agents** and one to **Legato Attention**.
2. **Legato Agents**: put the **Legato › Agent Tile** action on keys 1 to 15, and a **Switch Profile** back to **Legato** on key 16.
3. **Legato Attention**: put the **Legato › Attention Tile** action on keys 1 to 15, and a **Switch Profile** back to **Legato** on key 16.

The plugin fills the tiles in order and leaves the spare ones blank.

## With the browser extension

Each address the plugin opens carries `legato_source=streamdeck`. With the [browser extension](/legato/integrations/browser-extension/) in Chrome, a press reuses your last Legato tab in the same Chrome window instead of opening a new one. Without it, the address opens in a new tab as usual.
