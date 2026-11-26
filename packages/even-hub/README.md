# Legato on Even Realities G2 glasses

A proof of concept: an [Even Hub](https://hub.evenrealities.com/docs) app for Even Realities G2 glasses. It shows the agents running now, the ones that need you first, gives you a chat with the instance's Reeve for instructions, lets you allow or deny requests that need no signature, and answers the questions agents ask, one question and one option per screen, with your own answer by dictation. Tap an agent to see what it has been doing, and hold to dictate a message to it. Hold on the list to fold it into one row across the top (background mode).

It is a companion to the Legato Android app ([`mobile/`](../../mobile/README.md)) on the same phone: it reads Legato only through that app, paired once with a code, and works with every Legato saved there. The glasses hold no Legato address or token.

| | |
| --- | --- |
| ![The picker: the Legatos saved in the Legato app](docs/screenshots/picker.png) | |
| ![The active tab: agents that are not idle, needs-you first, and the way to the idle ones](docs/screenshots/agents.png) | ![An agent's page: what it asks for, then its conversation, newest first](docs/screenshots/agent.png) |
| ![One entry of the feed in full](docs/screenshots/entry.png) | ![Listening to a message for the agent, while held](docs/screenshots/listening.png) |
| ![The Reeve's chat: its state, what you said last, and its reply](docs/screenshots/reeve.png) | ![A message just sent to the Reeve, which is thinking](docs/screenshots/reeve-sent.png) |
| ![Background mode: one row across the top](docs/screenshots/background.png) | ![The approvals waiting](docs/screenshots/approvals.png) |
| ![An approval, with its options](docs/screenshots/approval.png) | ![Confirming the option chosen](docs/screenshots/approval-confirm.png) |
| ![A question, one per screen](docs/screenshots/questions.png) | ![One option of a question, with its place and what it means](docs/screenshots/question-option.png) |
| ![Choose any: an option ticked](docs/screenshots/question-choose-any.png) | ![Two answers reviewed before they are sent together](docs/screenshots/question-send.png) |

## What it shows

**The picker.** The Legatos saved in the Legato app, one line each, with any problem reaching the Legato app at the top. Tap one to open its agents. With exactly one saved, the app opens it straight away. With none, it says to add a Legato in the Legato app. The list is read again every 5 seconds while the picker is in front. If the Legato open is removed from the Legato app, the app comes back here and says so.

**The list.** Two tabs. **Active** is the default: the Reeve first (`◆ Reeve  ready`, `working`, `needs you`, or `asleep` when no session runs), then every agent that is not idle, the ones that need you first (`● needs you`, `▲ error`), then `▶ running` and `▷ background`. It ends with "Idle agents (N) →", which opens the **Idle** tab (`○`). Each line is the agent's task ID and as much of its title as fits. At the top right is when the list was last read (with how old it is first once that is a minute or more: `3m · 10:49`; looked at every 30 seconds), and beside the tabs is any problem reaching the Legato app or Legato. The list is read every 5 seconds while the app is in front. The Reeve is on neither tab's count.

**The Reeve.** Its chat: its state in the badge, "Reeve · <Legato>" beside it, what was said to it last on a dim line (`You: …`, or `Legato agent: …` for what Legato told it), and its reply in a frame under that, bright, scrolling when long. While it works with no reply yet, the frame says `Thinking…` and its newest tool call. Hold to dictate to it as to an agent; the message goes through `POST /api/reeve/message` marked as from the glasses, so the Reeve answers in a sentence or two of plain text, and it wakes a Reeve that is asleep. A message just sent shows as `You: …` with `Thinking…` until the conversation has it. Tap for its whole conversation, as an agent's feed; double tap from there comes back to the chat.

**Approvals.** While any wait, `● To approve (N) →` sits under the Reeve (under Questions when they wait too). It opens the list, a line each (`●` decidable here, `◇` signed, `○` nothing to choose). One the glasses may decide (options, no signature) shows its title in brief and its options in a frame; a tap on an option opens a page with the whole request and that option, and a tap there sends it (`allow` or `deny` with the option's ID); double tap goes back a step. A signed one shows in full with where to decide it. An agent's `● Asks:` line opens its approval. The list is read with the agents, and an approval decided elsewhere meanwhile is left.

**Questions.** While agents ask any (`AskUserQuestion`, which reaches Legato as an approval of source `agent_question`), `● Questions (N) →` sits under the Reeve. It opens them one per screen, every question of every agent in a row: the question framed, and under it, dim, its header, how many options and whether any may be chosen (or the answer chosen so far). A swipe moves to the next or the one before, round from the last to the first; a tap opens its options, one per screen: the question dim at the top, the option's label framed with its place (`2/4`, `★` for the one the agent recommends), and its description. A swipe moves between them; a tap chooses (or ticks `■`, when any may be chosen, then **Done** at the end); on the last, **Your own answer…**, hold to dictate (release, read the words, tap to use them; a tap there later drops them). Every part of a request is answered in turn, then `Send?` (or a review of all of them) sends them together on a second tap, and the next question shows. Answers wait while you swipe elsewhere. A request answered elsewhere, or whose agent stopped, while open says `○ gone` and that nothing was sent. An agent's `● Asks:` line for a question opens it. The swipes are the firmware's `SCROLL_TOP`/`SCROLL_BOTTOM` events on a text box whose text fits it (seen in the simulator; to be seen on real glasses).

**Background mode.** Hold on the list: the page folds into one row across the top. Left, bright: how many need you (`●`) and hit an error (`▲`), and the glasses' battery when it is low (20% or less, not charging). Right, dim: how many are running (`▶`), in the background (`▷`) and idle (`○`), and the battery (`G2 82%`, from the Even app's `getDeviceInfo` and `onDeviceStatusChanged`). It is read every 5 seconds, as the list is. Only a hold brings the list back.

**An agent.** Its state in a bordered badge at the top left, and its task and title beside it, dimmer. Under them, in a frame, the feed: what it asks for (a permission or a question, `● Asks: …`), then what it said and did, newest first, one line each (`Agent: …`, `You: …`, a tool call as `<tool>: <what>` marked `▶` running, `▲` failed or `·` done). Tap a line to read that entry in full. The page is read again every 5 seconds; once it is a minute old or more, how old shows at the end of the title (`· 3m`), and when a read fails the feed stays and the title says why. It works for agents Legato drives natively (Claude Code, Codex, native Chimera); one driven through its terminal has no conversation to read, and the page says so (as it does while a native one is still starting).

**A message.** On an agent's page, hold the touchpad and speak; the glasses' microphone records while you hold (at most a minute), and letting go stops it. Legato transcribes it (its Whisper, `voice.whisper_url`), and the page shows what it heard. Tap to send it to the agent, or double tap to drop it. A message the agent could not take (it is busy, say) stays on the page, and a tap sends it again. What the touchpad does is centred at the foot of each of these pages.

What Legato or another agent sent the agent shows as theirs (`Legato interrupted: …`, `Agent LEG-150: …`), not as yours: their messages open with a `[legato …]` tag.

| Where | Up, down | Tap | Double tap |
| --- | --- | --- | --- |
| The picker | Move through the list | Open that Legato's agents | The system's "exit?" dialog |
| Active tab | Move through the list | Open the Reeve's chat, the agent, or the Idle tab | Back to the picker; the "exit?" dialog when the Legato app has only one Legato |
| Idle tab | Move through the list | Open the agent | Back to Active |
| The Reeve's chat | Scroll the reply | Its whole conversation | Back to the list |
| An agent's feed | Move through the feed | Open the entry | Back to the list (to the chat, for the Reeve's) |
| Background mode | | | |
| An entry | Scroll | | Back to the feed |
| Recording | | Stop (as letting go does) | Drop it |
| The transcript | | Send it | Drop it |
| A problem | | Send again, for a message that failed to send | Back to the agent |

Hold, on the Reeve's chat, an agent's feed, an entry or a problem: record a message while held, and transcribe it when let go. Hold on the list: background mode; hold in background mode: the list again. Every page but background mode says at its foot what the touchpad does.

## Why `packages/even-hub/`

It is named for the platform it targets, as `streamdeck/` is: the Even Hub SDK, its `app.json` manifest and its `.ehpk` package. `packages/` is where Legato's apps are moving, one folder each, sharing one pnpm workspace (see [`../README.md`](../README.md)).

## Run it in the simulator

You need Node 22.13 or later and pnpm 11. The SDK, simulator and packer come from npm with the workspace; none needs an Even account.

```bash
task even-hub:dev     # terminal 1: the app at http://127.0.0.1:5173, on a fake phone
task even-hub:sim     # terminal 2: the Even Hub simulator window, open on it
```

Or, from `packages/even-hub/`: `pnpm dev` and `pnpm sim`. Arrow keys and clicks in the simulator window are the touchpad, and it records from your default microphone (`evenhub-simulator --list-audio-input-devices`, `--aid <device>`). Right-click its web view and choose Inspect Element to see the app's console.

`task even-hub:screenshots` runs both, walks the pages (the picker, both tabs, background mode, the Reeve's chat, an agent, a dictated message, one to the Reeve) saving what the glasses and the phone show to `docs/screenshots/`, and stops them. It fails if the app does not start, logs an error or draws nothing. Add `-- --headless` to run the simulator under `xvfb-run` with no window, recording silence from ALSA's null device.

## How it reaches Legato

Through the Legato Android app's **glasses link** on the same phone, at `http://127.0.0.1:8792`. In the Legato app, open **Glasses**, switch on **Even G2 glasses link**, and copy the code it shows into this app's page in the Even app ([the page](docs/screenshots/phone.png), as the simulator shows it). The code is kept in the Even app's storage for this app. **New code** in the Legato app revokes the old one at once.

The Legato app keeps each paired Legato's address, web token and pinned certificate, and passes on only these calls, to the Legato the glasses name:

| Call | Why |
| --- | --- |
| `GET /v1/instances` | The Legatos saved in the Legato app |
| `GET /v1/instances/{id}/api/agents?view=compact&status=running` | The list |
| `GET /v1/instances/{id}/api/agents/{task}/conversation?limit=20` | An agent's page (`limit=50` for the Reeve's chat) |
| `POST /v1/instances/{id}/api/voice/transcribe`, rebuilt as `{"transcription_only": true, "pcm": …}` | A dictated message |
| `POST /v1/instances/{id}/api/agents/{task}/chimera/send`, rebuilt as `{"content": …}` | Sending it; reaches only an agent already running natively, and never starts one |
| `POST /v1/instances/{id}/api/reeve/message`, rebuilt as `{"text": …, "from": "glasses"}` | A message to the Reeve, which starts or resumes it when none runs |
| `GET /v1/instances/{id}/api/approvals?status=pending` | The approvals waiting |
| `POST /v1/instances/{id}/api/approvals/{approval}/decision`, rebuilt as `{"decision": "allow"\|"deny", "option": …, "by": "glasses"}`, or `{"decision": "answer", "answers": {<question id>: [<string>, …]}, "by": "glasses"}` | A decision, or the answers to an agent's questions; Legato refuses to allow a signed approval without a signature |

What the glasses say when the Legato app answers for itself:

| The Legato app | The glasses |
| --- | --- |
| no code saved here (nothing is sent) | Not paired with the Legato app |
| not running, or its link is off | Can't reach the Legato app on this phone |
| `401` | The Legato app refused the pairing code |
| `404 unknown instance` | That Legato is no longer in the app |
| `404 not served to the glasses` (an older Legato app) | Update the Legato app on this phone: it can't pass this on yet. |
| `502` | its reason, such as Legato refusing the app's token, else "Can't reach Legato" |

**The fake phone (dev server).** `pnpm dev` serves the app with a fake phone at `/phone`, which answers the same protocol from `fixtures/` for two Legatos: **Desk** (`fixtures/agents.json`, or `LEGATO_FIXTURE`) and **Laptop** (no agents). It asks for no code, lists `fixtures/approvals.json` as Desk's approvals, questions among them (and logs a decision or answers), answers the Reeve's conversation from `fixtures/reeve.json` (for the agent marked `reeve` in the list), "transcribes" any recording to one sentence, and logs a message instead of sending it. Nothing reaches a real Legato.

## On real glasses

- **Sideload the dev server.** Run `VITE_LEGATO_PHONE=real pnpm dev --host 0.0.0.0`, then `pnpm exec evenhub qr --url http://<lan-ip>:5173` and scan the code in the Even Realities app (Even Hub tab, developer mode). The phone loads the page from your machine, and the app reads the Legato app on the phone, as a packed build does. Pair it on the app's page. Without `VITE_LEGATO_PHONE=real` it reads the dev server's fake phone. The Even app's WebView on Android has no DevTools, so a dev build also sends what it logs, and each call it makes to the phone, to the dev server, which prints them (`POST /__log`, `src/dev-log.ts`). A build leaves this out.
- **A packed build.** `task even-hub:pack` builds `dist/legato.ehpk`. Its network whitelist is `["http://127.0.0.1:8792"]`, so it can reach only the Legato app on the phone, and one package works with every Legato saved there. Upload it with `evenhub login` then `evenhub create`, or through the developer portal, and enter the pairing code in the app on the phone.
- **A beta build on your account.** After `pnpm exec evenhub login` (once; only you can run it), `task even-hub:beta -- -m "what changed"` (or `pnpm run beta -- -m …` here) puts the next build on your account's beta channel (`scripts/beta.mjs`): it raises `app.json`'s patch version (`--version x.y.z` to choose one), packs the app, runs `evenhub upload` and `evenhub promote <version> --to beta`. Without `-m`, the changelog is the subject of the latest commit to this folder. `--create` makes the app on the account first, with app.json's version as it is. A failed pack or upload puts `app.json` back. The Even app's Even Hub tab then offers the update. Commit `app.json` afterwards.

Nothing secret is built into the package. The pairing code admits only the calls above, and only from the phone itself (the link listens on `127.0.0.1`). Each Legato's web token stays in the Legato app.

## Why it polls

The app reads every 5 seconds rather than holding a stream open:

- On Android, the Even app's WebView can be suspended in the background, and an open connection is dropped with it.
- The Legato app's link answers one request at a time; it does not pass on Legato's `/ws`.
- A glance needs a list a few seconds old, not each change as it happens.

While the app is out of the foreground (the glasses' screen going off sends `FOREGROUND_EXIT`) it stops recording and reads every 30 seconds instead of 5 (the app's page on the phone has the choice: every 30 or 60 seconds, or not at all), so what the glasses show when they wake is at most that old, and a page they refused to draw is drawn again. It cannot wait for the wake instead: on real glasses waking the screen sends no event at all (`FOREGROUND_ENTER` comes when the G2 menu is opened, and reads at once). The glasses are sent only text that changed, so these reads cost Bluetooth nothing while nothing changes. A later version could hold a stream while in front and read on each `agents_changed`, with polling as the fallback.

## Layout and limits

These come from the SDK's [display docs](https://hub.evenrealities.com/docs/build/display), the simulator's changelog, and [`@evenrealities/pretext`](https://www.npmjs.com/package/@evenrealities/pretext), Even's measure of the firmware font:

- The canvas is 576×288 px, green on black, in one font that is not monospaced, with 27 px lines.
- The list page is five text containers (two tabs, a problem, the time, the touchpad's keys at the foot) and a list container. The firmware draws the list's highlight and moves it. A list holds at most 20 items of 63 bytes each, so long lists end with `+N more`, and every item is cut to fit with `…`.
- A list cannot change in place, so the list page is redrawn (a brief flicker on the glasses) only when its items change; the time and a problem change in place. A redraw puts the highlight back on the first item.
- The agent page is three text containers: title, body and hint. The body takes the input, so the firmware scrolls it. It is updated in place, at most 2000 characters (the newest entries that fit), each message cut at 360.
- The open tab has a border: the design guidelines' way to show a selection.

## Code

| File | What it is |
| --- | --- |
| `src/layout.ts` | The spacing scale every page uses, and centring |
| `src/picker.ts` | The picker page for the Legatos the Legato app lists |
| `src/glance.ts` | The list page for a list of agents: tabs, order, marks, cutting to fit |
| `src/agent.ts` | An agent's feed, one entry in full, and the pages of a dictated message |
| `src/reeve.ts` | The Reeve's chat page, and the pages of a message dictated to it |
| `src/background.ts` | Background mode's row: who needs you, the counts, the battery |
| `src/approvals.ts` | The approvals list, one approval with its options, and the steps of a decision |
| `src/questions.ts` | Agents' questions: one per screen, their options one per screen, your own answer, sending the answers |
| `src/legato.ts` | The calls to the Legato app's glasses link, and what their failures say |
| `src/app.ts` | The pages on the glasses (the picker, a Legato's list, an agent's), polling, the touchpad and the microphone |
| `src/settings.ts` | The pairing code, kept on the phone |
| `src/main.ts`, `index.html` | The entry point, and the phone-side page with the code |
| `src/dev-log.ts` | Dev builds only: what the app logs, sent to the dev server |
| `dev/legato-dev.ts` | The dev server's fake phone at `/phone`, on the fixtures, and `/__log` |
| `scripts/screenshots.mjs` | Runs the app in the simulator, walks its pages and saves screenshots |
| `scripts/pack.mjs` | Builds the `.ehpk`, checking the dev log relay is not in it |
| `scripts/beta.mjs`, `scripts/beta-plan.mjs` | Puts the next build on the beta channel of your Even developer account |

`pnpm test` runs the tests, which drive these through a fake bridge and a fake `fetch`. Agents' notes on this package are in [`docs/ai/even-hub.md`](../../docs/ai/even-hub.md).

## Not yet

Approving what needs a signature (that stays on the phone); notifications when an agent needs you. (Ask the Reeve to start agents.) Ideas are in [`docs/ai/even-hub.md`](../../docs/ai/even-hub.md).
