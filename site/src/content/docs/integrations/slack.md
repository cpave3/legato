---
title: Slack
description: Start workflows from Slack messages and mentions, and post and react in Slack from a workflow step.
sidebar:
  order: 55
---

Legato can run a Slack bot. A workflow can start when the bot receives a message or is mentioned, and a workflow step can post a message as the bot, or react to a message with an emoji. Use it to start a triage run from a channel, or to report a run's result back to the thread that started it.

The bot connects with Slack's Socket Mode, an outbound connection from Legato to Slack. Legato needs no public address, so it works on a laptop or a machine on your network.

## Create the Slack app

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and create an app from scratch in your workspace.
2. Under **Socket Mode**, turn Socket Mode on. Slack asks you to make an app-level token: give it the `connections:write` scope. The token starts `xapp-`.
3. Under **OAuth & Permissions › Bot Token Scopes**, add `chat:write`, `reactions:write`, `channels:history`, `groups:history`, `im:history` and `app_mentions:read`. `reactions:write` is for the reaction steps; if you add it to an app that is already installed, reinstall the app (step 5) so the bot token gets it.
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

### Forwarding a message to the bot

To have a workflow look at a message, such as an alert or a colleague's bug report, forward it to the bot: in Slack, choose **Forward message** and send it to the bot's direct messages, or share it into a channel the bot reads. Add a note if you like. The run's `${{ event.text }}` is your note, then the forwarded message quoted under a `> Forwarded from <author> in #<channel>:` line, and `${{ event.forwarded_url }}` is its link. A forward to the bot's direct messages counts as a mention; in a channel, it counts only if your note mentions the bot. See [Forwarded and shared messages](/legato/using/workflows/#forwarded-and-shared-messages).

Forwarded messages come with the message events the app already subscribes to, so they need no other scope. Only their text is read, not their files or images. Users and channels in the forwarded text are written as Slack writes them, such as `<@U0123456789>`.

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

The text can carry emoji, as `:eyes:` names or as the emoji itself: Slack shows both.

## React to a message

```yaml
steps:
  - id: seen
    title: Show the request was picked up
    type: chat.reaction.add
    with:
      bot: slack
      channel: ${{ workflow.inputs.channel }}
      message_id: ${{ workflow.inputs.message }}
      emoji: eyes
```

Pass `${{ event.channel }}` and `${{ event.message_id }}` in from the trigger to react to the message that started the run, or give an earlier `chat.message.send` step's `channel` and `message_id` outputs to react to a message the run posted. `emoji` is a name without colons (`eyes`, `white_check_mark`, your workspace's custom emoji), or the emoji itself for the common ones. `chat.reaction.remove` takes the same fields and removes the bot's reaction. A reaction already there, or already gone, is fine. Without the `reactions:write` scope the step fails and says to add it and reinstall the app. See [Workflows](/legato/using/workflows/#react-to-the-message-that-started-a-run).

## Limits

- Slash commands, buttons and other interactive messages are not supported.
- A message's text is kept with its trigger request, cut to 8 KiB, a forwarded message's part first.
- A link to a message pasted into a message is not read as a forward: Slack adds its preview later, as an edit, and edits do not start runs.
- Channels and users are named by their IDs, not their names.
- A message longer than 40,000 characters is refused.
