# Legato on Even Realities G2 glasses

A proof of concept: an [Even Hub](https://hub.evenrealities.com/docs) app for Even Realities G2 glasses. It shows the agents running now, the ones that need you first. Tap one to read what it has been doing, and tap again to dictate a message to it.

| | |
| --- | --- |
| ![The active tab: agents that are not idle, needs-you first, and the way to the idle ones](docs/screenshots/agents.png) | ![An agent's page: what it asks for, then its conversation, newest first](docs/screenshots/agent.png) |
| ![Listening to a message for the agent](docs/screenshots/listening.png) | ![The transcribed message, before it is sent](docs/screenshots/message.png) |

## What it shows

**The list.** Two tabs. **Active** is the default: every agent that is not idle, the ones that need you first (`● needs you`, `▲ error`), then `▶ running` and `▷ background`. It ends with "Idle agents (N) →", which opens the **Idle** tab (`○`). Each line is the agent's task ID and as much of its title as fits. At the top right is when the list was last read, and beside the tabs is any problem reaching Legato. The list is read every 5 seconds while the app is in front.

**An agent.** Its state and title, then what it asks for (a permission or a question), then what it said and did, newest first. A failed tool call is marked `▲`. The page is read again every 5 seconds. It works for agents Legato drives natively (Claude Code, Codex, native Chimera); one driven through its terminal has no conversation to read, and the page says so.

**A message.** Tap on an agent's page to start recording from the glasses' microphone (at most a minute). Tap again to stop. Legato transcribes it (its Whisper, `voice.whisper_url`), and the page shows what it heard. Tap to send it to the agent, or double tap to drop it. A message the agent could not take (it is busy, say) stays on the page, and a tap sends it again.

What Legato or another agent sent the agent shows as theirs (`Legato interrupted: …`, `Agent LEG-150: …`), not as yours: their messages open with a `[legato …]` tag.

| Where | Up, down | Tap | Double tap |
| --- | --- | --- | --- |
| Active tab | Move through the list | Open the agent, or the Idle tab | The system's "exit?" dialog |
| Idle tab | Move through the list | Open the agent | Back to Active |
| An agent | Scroll | Record a message | Back to the list |
| Recording | | Stop, and transcribe | Drop it |
| The transcript | | Send it | Drop it |
| A problem | | Try again (send again, for a message that failed to send) | Back to the agent |

## Why `packages/even-hub/`

It is named for the platform it targets, as `streamdeck/` is: the Even Hub SDK, its `app.json` manifest and its `.ehpk` package. `packages/` is where Legato's apps are moving, one folder each, sharing one pnpm workspace (see [`../README.md`](../README.md)).

## Run it in the simulator

You need Node 22.13 or later and pnpm 11. The SDK, simulator and packer come from npm with the workspace; none needs an Even account.

```bash
task even-hub:dev     # terminal 1: the app at http://127.0.0.1:5173, on the fixtures
task even-hub:sim     # terminal 2: the Even Hub simulator window, open on it
```

Or, from `packages/even-hub/`: `pnpm dev` and `pnpm sim`. Arrow keys and clicks in the simulator window are the touchpad, and it records from your default microphone (`evenhub-simulator --list-audio-input-devices`, `--aid <device>`). Right-click its web view and choose Inspect Element to see the app's console.

`task even-hub:screenshots` runs both, walks the pages (both tabs, an agent, a dictated message) saving what the glasses and the phone show to `docs/screenshots/`, and stops them. It fails if the app does not start, logs an error or draws nothing. Add `-- --headless` to run the simulator under `xvfb-run` with no window, recording silence from ALSA's null device.

## Which Legato it reads

The app makes four calls, and only these:

| Call | Why |
| --- | --- |
| `GET /api/agents?view=compact&status=running` | The list |
| `GET /api/agents/{task}/conversation?limit=20` | An agent's page |
| `POST /api/voice/transcribe` with `transcription_only: true` | A dictated message |
| `POST /api/agents/{task}/chimera/send` | Sending it; reaches only an agent already running natively, and never starts one |

- **Fixtures (default).** With no `LEGATO_URL`, the dev server answers from `fixtures/agents.json` and `fixtures/conversation.json` (the same conversation for any agent). It "transcribes" any recording to one sentence, and takes a message without sending it anywhere, printing it in its own log. `LEGATO_FIXTURE=fixtures/empty.json` picks another list.
- **A Legato.** Set `LEGATO_URL` and `LEGATO_TOKEN` (the web token, `legato auth token`) in your environment or a gitignored `.env.local` (copy `.env.example`). The dev server passes the four calls on to that Legato and adds the token itself, so the token never reaches the page. It answers nothing else. It does not pass on a message unless `LEGATO_ALLOW_SEND=1` is set: use that only with a scratch Legato. If Legato uses its own generated certificate, also set `NODE_EXTRA_CA_CERTS=<data_dir>/certs/ca.pem`.
- **On the phone, packed.** The phone-side page of the app has a form for Legato's address and web token. It keeps them in the Even app's storage on the phone, and the app then calls that Legato directly. Nothing secret is built into the package. ([The page](docs/screenshots/phone.png), as the simulator shows it.)

To run it against your own Legato from a token in Legato's credential store, without the token passing through your shell:

```bash
LEGATO_URL=https://127.0.0.1:3080 NODE_EXTRA_CA_CERTS=~/.local/share/legato/certs/ca.pem \
  legato credential exec <credential> --env LEGATO_TOKEN -- pnpm dev
```

## On real glasses

Two ways, both untested here (no hardware):

- **Sideload the dev server.** Run `pnpm dev --host 0.0.0.0`, then `pnpm exec evenhub qr --url http://<lan-ip>:5173` and scan the code in the Even Realities app (Even Hub tab, developer mode). The phone loads the page from your machine, and the dev server calls Legato for it with your token. The dev server checks no one: anyone who can reach it can read every running agent's conversation, and with `LEGATO_ALLOW_SEND=1` send any of them instructions. Do this only on a network you trust, such as your tailnet, and never with `LEGATO_ALLOW_SEND=1` against a Legato whose agents matter.
- **A private build.** `LEGATO_URL=https://<your legato> task even-hub:pack` builds `dist/legato.ehpk` with that Legato's origin in the manifest's network whitelist. The whitelist is fixed when the package is made, so the package calls only that Legato. Upload it with `evenhub login` then `evenhub create`, or through the developer portal, and enter the web token in the app on the phone.

The package has that Legato's address built in, never the token; the form on the phone needs only the token. The web token is Legato's whole API (starting agents and shells, changing settings), and here it is kept in the Even app's storage on the phone. `legato auth regenerate` revokes it, and every device paired with it.

For a packed build, the phone must reach Legato over HTTPS with a certificate it trusts. The form refuses a plain `http://` address other than this device's own, and drops the saved token when the address moves to another server. Legato's own generated certificate is not one, unless you install its CA on the phone. Over Tailscale, `tailscale serve` can put Legato behind a `*.ts.net` certificate. Legato already answers CORS for any origin and allows the `Authorization` header.

## Why it polls

The app reads every 5 seconds rather than holding Legato's `/ws` stream open:

- On Android, the Even app's WebView can be suspended in the background, and an open WebSocket is dropped with it.
- `/ws` takes its token in the URL.
- A glance needs a list a few seconds old, not each change as it happens.

The app stops reading, and recording, while it is in the background, and reads at once when it comes back. A later version could hold `/ws` while in front and read on each `agents_changed`, with polling as the fallback.

## Layout and limits

These come from the SDK's [display docs](https://hub.evenrealities.com/docs/build/display), the simulator's changelog, and [`@evenrealities/pretext`](https://www.npmjs.com/package/@evenrealities/pretext), Even's measure of the firmware font:

- The canvas is 576×288 px, green on black, in one font that is not monospaced, with 27 px lines.
- The list page is four text containers (two tabs, a problem, the time) and a list container. The firmware draws the list's highlight and moves it. A list holds at most 20 items of 63 bytes each, so long lists end with `+N more`, and every item is cut to fit with `…`.
- A list cannot change in place, so the list page is redrawn (a brief flicker on the glasses) only when its items change; the time and a problem change in place. A redraw puts the highlight back on the first item.
- The agent page is three text containers: title, body and hint. The body takes the input, so the firmware scrolls it. It is updated in place, at most 2000 characters (the newest entries that fit), each message cut at 360.
- The open tab has a border: the design guidelines' way to show a selection.

## Code

| File | What it is |
| --- | --- |
| `src/glance.ts` | The list page for a list of agents: tabs, order, marks, cutting to fit |
| `src/agent.ts` | An agent's page, and the pages of a dictated message |
| `src/legato.ts` | The four calls to Legato, and what their failures say |
| `src/app.ts` | The pages on the glasses, polling, the touchpad and the microphone |
| `src/settings.ts` | Legato's address and token, kept on the phone: HTTPS only, the token never following an address elsewhere |
| `src/main.ts`, `index.html` | The entry point, and the phone-side page with the form |
| `dev/legato-dev.ts` | The dev server's `/legato` path: the fixtures, or a pass-through of the four calls that adds the token. It rebuilds what it passes on from the fields each call needs, and refuses a request from another site or a POST that is not JSON |
| `scripts/screenshots.mjs` | Runs the app in the simulator, walks its pages and saves screenshots |
| `scripts/pack.mjs` | Builds the `.ehpk` for one Legato |

`pnpm test` runs the tests, which drive these through a fake bridge and a fake `fetch`. Agents' notes on this package are in [`docs/ai/even-hub.md`](../../docs/ai/even-hub.md).

## Not yet

Approving or denying what an agent asks for from the glasses; starting agents; notifications when an agent needs you. Ideas are in [`docs/ai/even-hub.md`](../../docs/ai/even-hub.md).
