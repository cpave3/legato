---
title: Even Realities G2 glasses
description: See which agents are running and which need you on Even Realities G2 smart glasses, read what one has been doing, and dictate a message to it.
sidebar:
  order: 95
---

:::caution
This is a proof of concept. It runs in the Even Hub simulator; it has not yet run on real glasses. You build it from source.
:::

The glasses app puts your agents on the display of Even Realities G2 glasses. The ones that need you come first, so a glance tells you whether to go back to your desk. Tap an agent to see what it has been doing, and hold to dictate a message to it.

![The active tab: agents that are not idle, needs-you first, and the way to the idle ones](../../../assets/even-g2-agents.png)

## What it shows

The list has two tabs. **Active**, where the app opens, holds every agent that is not idle, the ones that need you first. It ends with **Idle agents**, which opens the **Idle** tab. Each line shows an agent's state, task ID and as much of its title as fits.

| Mark | State |
| --- | --- |
| `●` | needs you: it is waiting on a permission or a question |
| `▲` | error |
| `▶` | running |
| `▷` | background: its turn ended, and something it started still runs |
| `○` | idle |

The time at the top right is when the list was last read; the app reads it every 5 seconds while it is open. When Legato can't be reached, a note beside the tabs says so, and the last list stays on screen.

Open an agent to see its state in a badge at the top, then its feed: what it asks you for, then what it said and did, newest first, one line each. Tap a line to read it in full. A message that Legato or another agent sent it is shown as theirs, not as yours.

![An agent's page: its state in a badge, then its feed, one line an entry](../../../assets/even-g2-agent.png)

This works for agents that Legato drives natively, such as Claude Code and Codex. For an agent driven through its terminal, the page says there is no conversation to read.

## Dictate a message

1. Open the agent.
2. Hold the touchpad and say your message. The app records from the glasses' microphone while you hold, for up to a minute.
3. Let go. Legato transcribes what you said, and the glasses show it.
4. Tap to send it to the agent, or double tap to drop it.

If the agent can't take the message, because it is busy for example, the glasses say so and keep the message. Tap to send it again.

Legato does the transcription, so it needs `voice.enabled` and `voice.whisper_url` set (see the [configuration reference](/legato/reference/configuration/)). A message reaches only an agent that is running natively; the app never starts an agent.

## The touchpad

| Where | Up, down | Tap | Double tap |
| --- | --- | --- | --- |
| Active tab | Move through the list | Open the agent, or the Idle tab | Ask to leave the app |
| Idle tab | Move through the list | Open the agent | Back to Active |
| An agent's feed | Move through the feed | Open the entry | Back to the list |
| An entry | Scroll | | Back to the feed |
| Recording | | Stop | Drop it |
| The transcript | | Send it | Drop it |
| A problem | | Send it again, if it failed to send | Back to the agent |

On an agent's feed or an entry, hold to record a message; letting go stops the recording.

## Try it in the simulator

You need Node 22.13 or later and pnpm 11. In the Legato repository:

```bash
task even-hub:dev   # serves the app on a sample list of agents and a sample conversation
task even-hub:sim   # opens the Even Hub simulator on it
```

On the sample data, any recording "transcribes" to one sentence, and a sent message goes nowhere.

To use your own Legato instead, set its address and [web token](/legato/security/web-auth/) before `task even-hub:dev`:

```bash
export LEGATO_URL=https://localhost:3080
export LEGATO_TOKEN="$(legato auth token)"
export NODE_EXTRA_CA_CERTS=~/.local/share/legato/certs/ca.pem   # when Legato makes its own certificate
```

The token stays in the dev server. It passes on the app's four calls to Legato and nothing else, and it refuses to send a message unless you also set `LEGATO_ALLOW_SEND=1`. Turn that on only for a Legato whose agents you mean to message.

## On your glasses

There are two ways to get the app onto real glasses. Neither has been tried yet.

- **From your machine.** Run the dev server where your phone can reach it, and scan the QR code that `evenhub qr` prints in the Even Realities app's developer mode. The phone loads the app from your machine.
- **As a private build.** `LEGATO_URL=https://<your Legato> task even-hub:pack` builds `dist/legato.ehpk`, which can reach only that Legato. Upload it to the Even Hub developer portal, install it from the Even Realities app, and enter the web token in the app on your phone.

:::danger
The dev server checks no one. Anyone who can reach it can read every running agent's conversation through it, and with `LEGATO_ALLOW_SEND=1` send any of your agents instructions. Run it where others can reach it only on a network you trust, such as your tailnet, and never with `LEGATO_ALLOW_SEND=1` for a Legato whose agents matter.
:::

The web token gives the whole of Legato's API, not only what the glasses use. In a private build it is kept in the Even Realities app's storage on your phone. To revoke it, run `legato auth regenerate`; that also signs out every device paired with the old token.

The phone must reach Legato over HTTPS with a certificate it trusts. Legato's own certificate is not one, unless you install its CA on the phone (see [Devices](/legato/running/devices/)).

The package's [README](https://github.com/cpave3/legato/blob/main/packages/even-hub/README.md) has the details, and its internals are in [`docs/ai/even-hub.md`](https://github.com/cpave3/legato/blob/main/docs/ai/even-hub.md) on GitHub.
