---
title: Discord
description: Start workflows from Discord messages and mentions, and post to Discord from a workflow step.
sidebar:
  order: 56
---

Legato can run a Discord bot. A workflow can start when the bot receives a message or is mentioned, and a workflow step can post a message as the bot.

The bot connects to Discord's Gateway, an outbound connection from Legato to Discord. Legato needs no public address.

## Create the bot

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications) and create an application.
2. Under **Bot**, reset and copy the bot's token.
3. Under **Bot › Privileged Gateway Intents**, turn on **Message Content Intent**. Without it, Discord sends the text of a server message only when the message mentions the bot, so text filters do not match other messages.
4. Under **OAuth2 › URL Generator**, choose the `bot` scope and the **View Channels**, **Send Messages** and **Read Message History** permissions. Open the generated address to add the bot to your server.

## Set it up in Legato

1. Store the token in Legato's secret store. Ask the Reeve to open a secure secret form, or read it from standard input:

   ```bash
   legato secret add discord-bot-token   # paste the token, then Ctrl-D
   ```

2. Open **Settings › Integrations › Discord** and set **Bot token** to `legato://discord-bot-token`.
3. Optionally set **Default channel** to a channel ID. In Discord, turn on **Developer Mode** (User Settings › Advanced), then right-click a channel and choose **Copy Channel ID**.
4. **Gateway intents** asks for all four by default: `guilds`, `guild_messages`, `direct_messages` and `message_content`. If you leave `message_content` out, turn it off in the Developer Portal too.
5. Turn on **Connect**, then restart Legato.

If the bot asks for an intent the Developer Portal has not allowed, Discord refuses the connection and the card shows **auth failed** with what to turn on. The other states are the same as [Slack's](/legato/integrations/slack/#set-it-up-in-legato).

```yaml
integrations:
  discord:
    enabled: true
    token: legato://discord-bot-token
    default_channel: "123456789012345678"
```

## Use it in workflows

The triggers and step are the same as Slack's, with `bot: discord`:

```yaml
on:
  chat.mention:
    bot: discord
    inputs:
      thread: ${{ event.thread_id }}
steps:
  - id: ack
    title: Acknowledge
    type: chat.message.send
    with:
      bot: discord
      text: On it.
      thread_id: ${{ workflow.inputs.thread }}
```

On Discord, `${{ event.thread_id }}` is the thread's channel ID when the message is in a thread, so the reply goes into the thread. Otherwise it is the message's own ID, and the step sends its message as a reply to that message. See [Workflows](/legato/using/workflows/#chat-messages-and-mentions).

## Limits

- Slash commands, buttons and other interactive components are not supported.
- A message longer than 2,000 characters is refused.
- Messages the bot sends never mention anyone, even if the text has `@everyone`.
- A direct message to the bot counts as a mention.
