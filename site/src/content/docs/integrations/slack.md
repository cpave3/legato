---
title: Slack
description: Start workflows from Slack messages and mentions, and post to Slack from a workflow step.
sidebar:
  order: 55
---

Legato can run a Slack bot. A workflow can start when the bot receives a message or is mentioned, and a workflow step can post a message as the bot. Use it to start a triage run from a channel, or to report a run's result back to the thread that started it.

The bot connects with Slack's Socket Mode, an outbound connection from Legato to Slack. Legato needs no public address, so it works on a laptop or a machine on your network.

## Create the Slack app

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and create an app from scratch in your workspace.
2. Under **Socket Mode**, turn Socket Mode on. Slack asks you to make an app-level token: give it the `connections:write` scope. The token starts `xapp-`.
3. Under **OAuth & Permissions › Bot Token Scopes**, add `chat:write`, `channels:history`, `groups:history`, `im:history` and `app_mentions:read`.
4. Under **Event Subscriptions**, turn events on and subscribe to the bot events `message.channels`, `message.groups` and `message.im`.
5. Install the app to your workspace. Copy the **Bot User OAuth Token**, which starts `xoxb-`.
6. In Slack, invite the bot to each channel it should read or post in: `/invite @your-bot`.

## Set it up in Legato

The tokens go in Legato's secret store. The settings hold only the secrets' names.

1. Store both tokens. Ask the Reeve to open a secure secret form, or read each token from standard input:

   ```bash
   legato secret add slack-app-token   # paste the xapp- token, then Ctrl-D
   legato secret add slack-bot-token   # paste the xoxb- token, then Ctrl-D
   ```

2. Open **Settings › Integrations › Slack**. Set **App-level token** to `legato://slack-app-token` and **Bot token** to `legato://slack-bot-token`.
3. Optionally set **Default channel** to a channel ID, such as `C0123456789`. A step that names no channel posts there. To find a channel's ID, open the channel's details in Slack; the ID is at the bottom.
4. Turn on **Connect**, then restart Legato. Chat bot settings apply at restart.

The card shows the bot's state: **connected**, **connecting**, **backing off** (Legato tries again after a growing wait), **auth failed** (Slack refused a token; fix it and press **Reconnect**, or restart after changing a token), **disabled**, or connected **in another Legato process** when a TUI and `legato serve` share a data directory. Only one process holds the connection.

The same settings in `config.yaml`:

```yaml
integrations:
  slack:
    enabled: true
    app_token: legato://slack-app-token
    bot_token: legato://slack-bot-token
    default_channel: C0123456789
```

## Start a workflow from Slack

```yaml
on:
  chat.mention:
    bot: slack
    channel: C0123456789
    text: {contains: deploy}
    inputs:
      request: ${{ event.text }}
      thread: ${{ event.thread_id }}
```

`chat.message` starts a run for each message the bot can see; `chat.mention` only for messages that mention the bot, and for direct messages to it. The bot's own messages never start a run, and other bots' messages start one only with `include_bots: true`. See [Workflows](/legato/using/workflows/#chat-messages-and-mentions) for every filter and the values a trigger can pass on.

## Post from a workflow

```yaml
steps:
  - id: reply
    title: Reply in the thread
    type: chat.message.send
    with:
      bot: slack
      channel: ${{ workflow.inputs.channel }}
      text: Deploy started.
      thread_id: ${{ workflow.inputs.thread }}
```

With `thread_id`, the message goes into that thread. `${{ event.thread_id }}` of a Slack trigger is the thread of the message that started the run, or a new thread under it. Without a `channel`, the step posts to the default channel. The step fails at once if the bot is not set up, is switched off, or Slack refused its token.

## Limits

- Slash commands, buttons and other interactive messages are not supported.
- A message's text is kept with its trigger request, cut to 8 KiB.
- Channels and users are named by their IDs, not their names.
- A message longer than 40,000 characters is refused.
