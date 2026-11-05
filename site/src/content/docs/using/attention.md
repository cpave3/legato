---
title: Attention and approvals
description: The one queue of things that need you, and the requests from agents that wait for your decision.
sidebar:
  order: 40
---

Attention is one queue of everything that needs you right now: a plan to decide, a review to read, a pull request that cannot merge, an agent waiting for you, a workflow that failed. Approvals are the requests from agents that wait for your answer, such as "may I run this command?". Approvals appear in Attention too, and also in a tray and on your phone, so you can answer from wherever you are.

## Attention

Attention lists only what you can act on. Each item comes from the part of Legato that owns it, and goes away by itself when that part no longer needs you. The full history of plans, reviews and workflows stays on their own pages.

| Source | When it needs you |
| --- | --- |
| Plans | A plan an agent proposed waits for your decision. See [Plans](/legato/using/plans/). |
| Reviews | A review tour is marked ready and still has changes you have not reviewed. See [Reviews](/legato/using/reviews/). |
| Pull requests | An open, non-draft pull request linked to a task needs a review, has changes requested, or failed CI. Each pull request of a stack has its own item. |
| Agents | An agent you started by hand waits for you or stopped on an error. Agents that a swarm or a workflow runs are left to those. |
| Workflows | A step waits for you to go on, a workflow agent waits for input, or a step failed. See [Workflows](/legato/using/workflows/). |
| Workflow triggers | A trigger needs input or confirmation, or failed. |
| Approvals | An agent's request waits for your answer. See [Approvals](#approvals). |
| Devices | Someone ran `legato device reset`. |
| Argus | [Argus](/legato/integrations/argus/) has an open case on one of your pull requests. |
| Updates | A new release of Legato is ready to install, or an install failed. See [Updates](/legato/running/updates/). |
| Integrations | In the last 7 days, a request from Argus did not reach an agent, a reply never reached Argus, or Argus refused the connection. |
| Handoff | On a main instance: a task [away on a worker](/legato/using/handoff/) is done there (Ready for review, with **Return**), failed (with **Recall**) or was lost (with **Take back**); a worker no longer accepts your main (**Pair again**) or has been unreachable for 15 minutes. A task whose agent needs you raises nothing on your main: the worker tells you. See [Attention and notifications](/legato/using/handoff/#attention-and-notifications). |

Some things never raise Attention: a draft, closed or merged pull request; CI that is still running; a comment count; an idle agent; a workflow that succeeded; and Learning's records.

### Acting on an item

Most items have a main action, such as approving a plan, allowing an agent's request, returning a task from a worker, or opening the review. An action that cannot be undone, such as taking a task back without its results, asks you to confirm first. Others take you to the page where you act. An item about a task, such as a pull request that cannot merge, opens the board with that task selected, in the Away lane when the task is [away on a worker](/legato/using/handoff/#pull-requests-while-away).

You can also dismiss an item. Dismissing hides that item as it is now. If its source changes (the pull request fails CI again, the agent asks something new), the item comes back.

### Where you see it

- **Web UI**: the **Attention** page, in three sections: **Needs intervention**, **Ready for review** and **Merge blockers**. Its count shows on **Attention** in the navigation.
- **TUI**: press `I` on the board. `j` and `k` select an item, `enter` runs its main action, and `d` dismisses it. The TUI lists the items of away tasks and workers but does not run their actions: return, recall and take back from the web UI.
- **Command line**:

  ```bash
  legato attention list
  legato attention list --urgency high --json
  ```

  You can filter by `--producer`, `--category` and `--urgency`.
- **[Stream Deck](/legato/integrations/stream-deck/)**: the same count and list.

Every place shows the same items.

## Approvals

An approval is a request that waits for you:

- An agent asks for permission, such as to run a command or edit a file.
- An agent asks you a question.
- An agent asks for a credential that the [credential broker](/legato/security/credential-broker/) holds.

Legato keeps each request until someone decides it, and shows it everywhere you might be. The first decision wins: once you answer in one place, the request closes everywhere. If the agent settles the request itself (you answered in its terminal) or ends, the request closes too.

Legato raises approvals for agents it talks to natively: Claude Code and Codex through Legato's bridge, and Chimera. It finds requests that an agent raised while Legato was not running when Legato starts again.

### Answering in the web UI

While approvals wait, a tray at the bottom right of every page shows them as cards. Each says which agent asks, what it would do, and its options. **Deny** takes a note for the agent. A question shows its options, and you can type an answer of your own. The tray folds to a count and opens again when a new approval arrives.

### Focus mode

When agents ask questions, the button beside the tray's fold arrow opens **focus mode**: the questions one at a time, in the middle of the screen with nothing else on it. Each option has a number; the agent's recommendation is marked. An agent's questions are reviewed and sent together; a single question sends from the question itself.

| Key | Does |
| --- | --- |
| `1`–`9` | Choose an option (with several allowed, turn it on or off) |
| `↑` `↓` | Move between options |
| `Enter` | Next question, review, or send |
| `←` | Back |
| `/` | Type an answer in your own words |
| `⌘ Enter` or `Ctrl Enter` | Send |
| `Esc` | Leave focus mode |

On a phone focus mode fills the screen, with **Back** and **Next** at the bottom.

Focus mode stays on until you leave it. With no question left it fades away and says it is waiting, and the next question to arrive opens it again. It is for this page only: a reload turns it off.

When the Legato tab is not in front, the browser shows a notification for each new approval. Click it to open the tray on that request. Turn this off in **Settings › Security › Notify me of approvals**.

### Answering in the TUI

On every view, the key bar says how many approvals wait, such as `◆ 2 approvals need you · ! answer`. Press `!` to see them:

| Key | Action |
| --- | --- |
| `y` | Allow, with the first option |
| `1`–`9` | Allow with that option, such as `1 for 30 minutes · 2 once` for a credential |
| `n` | Deny, or dismiss a question |
| `j` / `k` | Choose a request |
| `esc` | Close |

Answer a question in the web UI.

### Answering on your phone

Each new approval sends one notification through ntfy. It names the task and the agent tool but never the command. Tapping it opens the request in the Legato app. To set up ntfy, see [Notifications](/legato/integrations/notifications/). To stop these notifications:

```yaml
notifications:
  approvals: false
```

### Approvals that need your phone

Some decisions are signed: only you, at an enrolled phone or with a passkey on your computer, can allow them. An agent that has the web token still cannot. The TUI and Attention can deny a signed approval but not allow it, and say to use your phone. Which credential requests need a signature is set by the credential's rules; see [The credential broker](/legato/security/credential-broker/).
