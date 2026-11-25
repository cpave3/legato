---
title: Discord
description: Start workflows from Discord messages, mentions and button clicks, and post and react in Discord from a workflow step.
sidebar:
  order: 56
---

Legato can run a Discord bot. A workflow can start when the bot receives a message or is mentioned, and a workflow step can post a message as the bot, or react to a message with an emoji.

The bot connects to Discord's Gateway, an outbound connection from Legato to Discord. Legato needs no public address.

## Create the bot

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications) and create an application.
2. Under **Bot**, reset and copy the bot's token.
3. Under **Bot › Privileged Gateway Intents**, turn on **Message Content Intent**. Without it, Discord sends the text of a server message only when the message mentions the bot, so text filters do not match other messages.
4. Under **OAuth2 › URL Generator**, choose the `bot` scope and the **View Channels**, **Send Messages**, **Read Message History** and **Add Reactions** permissions. **Add Reactions** is for the reaction steps; to give it to a bot already in your server, turn it on for the bot's role in **Server Settings › Roles**. Open the generated address to add the bot to your server.

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

The triggers and steps are the same as Slack's, with `bot: discord`:

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

On Discord, `${{ event.thread_id }}` is the thread's channel ID when the message is in a thread, so the reply goes into the thread. Otherwise it is the message's own ID, and the step sends its message as a reply to that message. To send mentions to different workflows by what they ask, give each trigger a `text` filter, and let a trigger with `fallback: true` answer the rest. See [Workflows](/legato/using/workflows/#chat-messages-and-mentions).

Discord shows a bot's `:eyes:` as text, so Legato turns the common emoji names in a message (`:eyes:`, `:white_check_mark:`, `:tada:`, `:rocket:` and so on) into the emoji before sending. Names it does not know, a custom emoji such as `<:rocket:123…>`, and anything in `code` are sent as written. An emoji written as itself always works.

### Forwarding a message to the bot

Forward a message to the bot's direct messages, or into a channel it reads, and the run's `${{ event.text }}` is the forwarded message quoted under a `> Forwarded message from <link>:` line. `${{ event.forwarded }}` is `true` and `${{ event.forwarded_url }}` is the original's link. Discord does not say who wrote a forwarded message, so the quote names only its link.

A comment you add while forwarding reaches the bot as a message of its own, just after the forward, so it starts a run of its own. A forward to the bot's direct messages counts as a mention; in a server channel, a forward never does, since it carries no comment of yours. In a server, the forwarded text needs the Message Content intent, as any other message's text does. Only text is read, not attachments or embeds. See [Forwarded and shared messages](/legato/using/workflows/#forwarded-and-shared-messages).

### React to a message

```yaml
steps:
  - id: seen
    title: Show the request was picked up
    type: chat.reaction.add
    with:
      bot: discord
      channel: ${{ workflow.inputs.channel }}
      message_id: ${{ workflow.inputs.message }}
      emoji: eyes
```

Pass `${{ event.channel }}` and `${{ event.message_id }}` in from the trigger; for a message in a thread, `event.channel` is the thread. `emoji` is the emoji itself (`👀`), one of the common names (`eyes`, `white_check_mark`, `thumbsup`, ...), or a custom emoji as `name:id`. `chat.reaction.remove` removes the bot's reaction. The bot needs **Read Message History**, and **Add Reactions** for an emoji no one has reacted with yet; without them the step fails and says which. See [Workflows](/legato/using/workflows/#react-to-the-message-that-started-a-run).

## Read a thread

A `chat.thread.read` step gives a later step what the mention was about, as `${{ steps.<id>.outputs.transcript }}`:

```yaml
steps:
  - id: thread
    title: Read the thread
    type: chat.thread.read
    with:
      bot: discord
      channel: ${{ workflow.inputs.channel }}
      thread_id: ${{ workflow.inputs.thread }}
```

With `event.thread_id` from a trigger:

- **In a thread**, the step reads the thread: its starter message, which a thread started on a message keeps in the channel it was started in, then the thread's messages. Discord's system messages, such as the starter's stub inside the thread, are left out. A bot's response to a slash command or app command is a bot's message, kept with `include_bots: true`.
- **Anywhere else**, the step reads the chain of replies that ends at the message: the message, the one it replies to, and so on, up to 50 messages, root first. A message that replies to nothing gives that message alone.

People are named by their display name. The step needs the **Read Message History** permission and the Message Content intent, which the bot already has. See [Reading the thread](/legato/using/workflows/#reading-the-thread) for what the step keeps.

A comment added to a forward arrives as a message of its own and does not reply to the forward. A mention in that comment therefore reads only the comment. Mention the bot in a thread, or in a reply to the forward, to include the forward.

## Buttons

A `chat.message.send` step can end its message with up to five buttons, in one row, and a `chat.button` trigger starts a workflow when someone clicks one:

```yaml
    with:
      bot: discord
      text: Found the cause.
      buttons:
        - {label: "🛠 Fix it", value: "fix:${{ workflow.task.id }}", style: primary}
        - {label: "✖ Dismiss", value: "dismiss:${{ workflow.task.id }}", style: danger}
```

Clicks come over the bot's Gateway connection and need no intent or other setting. Legato answers each click at once, so Discord does not show "This interaction failed". When a workflow takes the click, Legato greys out the message's buttons and adds a line such as **🛠 Fix it — chosen by @alice** under its text. A click no workflow takes changes nothing. A button's value is at most 100 characters on Discord. See [Chat buttons](/legato/using/workflows/#chat-buttons) for the trigger.

## Limits

- Slash commands, select menus and modals are not supported. Buttons are: see [Buttons](#buttons).
- A message longer than 2,000 characters is refused.
- Messages the bot sends never mention anyone, even if the text has `@everyone`.
- A direct message to the bot counts as a mention.
