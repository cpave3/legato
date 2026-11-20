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
| Pull requests not reviewed | A task you worked on here (not handed off) reached Done or the Review column with open pull requests, and no review loop reviews them. **Start review passes** starts one [review loop](/legato/using/review-passes/#an-agent-as-the-author) on the whole stack with the task's agent as the author. With review passes off, the item says so. Dismiss it and it comes back when someone pushes to one of them. |
| Interrupted agents | Agents were running when Legato stopped, and their tmux sessions were gone when it started again: the machine or tmux went down. **Resume all** resumes those that can be; **Show them** opens the Agents page. See [interrupted agents](/legato/using/agents/#after-a-crash-interrupted-agents). |
| Workflows | A step waits for you to go on, a workflow agent waits for input, a step failed, or a run's [review loop](/legato/using/review-passes/#review-any-pull-request) is stuck: **Run another pass**, or **End** it as stuck so the run goes on (in the TUI, Enter and `x`). See [Workflows](/legato/using/workflows/). |
| Workflow triggers | A trigger needs input or confirmation, failed, or was stopped to prevent a loop (**Run anyway**, **Pause trigger**). |
| Approvals | An agent's request waits for your answer. See [Approvals](#approvals). |
| Devices | Someone ran `legato device reset`. |
| Argus | [Argus](/legato/integrations/argus/) has an open case on one of your pull requests. |
| Settings | You changed settings that take effect only when Legato restarts. **Restart Legato** restarts it. |
| Moving settings | Once, after the upgrade: settings set in `config.yaml` that can move into the database, so they can be changed from Settings. **Choose what to move** opens the list. |
| Engrams | An agent was asked to start as an [engram](/legato/agent-tools/engrams/), and the engram did not boot, so the agent runs as a normal session. The item says why, and goes when the agent ends. For the Reeve, the item is **The Reeve's engram did not boot** and leads to its engram in Settings. |
| Restarts | A restart of Legato failed (its configuration did not load, or it was not answering in time) and was rolled back to the configuration from before. See [Updates](/legato/running/updates/#restarting-legato-yourself). |
| Updates | A new release of Legato is ready to install, or an install failed. See [Updates](/legato/running/updates/). |
| Integrations | In the last 7 days, a request from Argus did not reach an agent, a reply never reached Argus, or Argus refused the connection. |
| Agents' items | An agent posted something for you with its [`for_operator` tool](/legato/agent-tools/mcp-tools/): an action only you can do, a question, finished work to look at, or anything else you must see. See [Operator items](#operator-items). |
| Handoff | On a main instance: a task [away on a worker](/legato/using/handoff/) is done there (Ready for review, with **Return**), failed (with **Recall**) or was lost (with **Take back**); its review passes are stuck (with **Return as is**, **Run another pass**, **Message the author** and **Recall**); a worker no longer accepts your main (**Pair again**) or has been unreachable for 15 minutes. A task whose agent needs you raises nothing on your main: the worker tells you. See [Attention and notifications](/legato/using/handoff/#attention-and-notifications). |

Some things never raise Attention: a draft, closed or merged pull request; CI that is still running; a comment count; an idle agent; a workflow that succeeded; and Learning's records.

### Operator items

An agent often finishes its turn while it still needs you: to type a slash command, test a build in a browser, merge a pull request, answer a question it asked in plain words, or look at what it built. An idle agent raises nothing by itself, so agents are given a tool, `for_operator`, to say so. Each such item names its kind (Action, Question, Done or Other), what is needed, and what it blocks. **Open agent** takes you to the agent.

You can act on an item without opening the agent. **Copy** copies the command or slash command it gives, and **Open** opens a link. **Done** closes the item and tells the agent you did it. A question has a button for each answer it offers and a box for your own; your answer reaches the agent as your message, so it closes the agent's other items too.

An agent that posts the same title again updates its item, which comes back if you had dismissed it. Items raise no notification. An agent's items go away:

- when you next type to that agent, in its terminal or from Legato (a slash command and a macro count; messages from other agents, the Reeve and Legato itself do not). A Chimera agent's items close only on a message sent through Legato, since Chimera does not say what was typed in its own terminal.
- when the agent clears them because they no longer apply.
- when the agent ends. An agent [interrupted](/legato/using/agents/#after-a-crash-interrupted-agents) by a crash keeps them until you resume it or dismiss it.

### The needs-you rail

In the web UI, the column on the right of every page shows Attention as you choose: Settings › Preferences › **Needs-you rail** has a switch for each kind of item, under **From agents** (actions, questions, finished work, anything else) and **From Legato** (approvals, agents, workflows, plans, ready for review, merge blockers, Legato's own). Ready for review and merge blockers are off at first. The settings are kept by Legato, so every browser shows the same rail. See [Web UI](/legato/using/web-ui/#getting-around).

### Acting on an item

Most items have a main action, such as approving a plan, allowing an agent's request, returning a task from a worker, or opening the review. An action that cannot be undone, such as taking a task back without its results, asks you to confirm first. Others take you to the page where you act. An item about a task, such as a pull request that cannot merge, opens the board with that task selected, in the Away lane when the task is [away on a worker](/legato/using/handoff/#pull-requests-while-away).

In the web UI each item also has **Ask the Reeve**, which starts the [Reeve](/legato/using/reeve/) with what the item is about, so it can find out what is wrong.

You can also dismiss an item. Dismissing hides that item as it is now. If its source changes (the pull request fails CI again, the agent asks something new), the item comes back.

### Where you see it

- **Web UI**: the **Attention** page, in three sections: **Needs intervention**, **Ready for review** and **Merge blockers**. Its count shows on **Attention** in the navigation.
- **TUI**: press `I` on the board. `j` and `k` select an item, `enter` runs its main action, and `d` dismisses it. On an item an agent posted for you, `enter` opens the agent and `x` marks it done; `c` copies an item's command. The TUI lists the items of away tasks and workers but does not run their actions: return, recall and take back from the web UI.
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

The tray also lists the [Reeve](/legato/using/reeve/)'s change sets that wait on instances this one is paired with, marked with the instance. They are decided there, with a device enrolled there: **◈ Approve on legato-1** opens that instance's page on the change set, and **Open legato-1 ↗** opens its page.

### Answering in the desktop app

The [desktop app](/legato/running/desktop-app/) shows every server's approvals in the feed of its Legato widget, a line each, and you allow, deny or sign them there with a click and your security key, without opening the main window. A deny with a note, a question and a secret open the main window on the approval. See [Approvals in the widget's feed](/legato/running/desktop-app/#approvals-in-the-widgets-feed).

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

The Reeve's change sets are approvals too: one per step, at the level of its riskiest change. A set you let run without asking never waits in the tray: it shows for a few seconds at the corner of the screen as already done. See [The Reeve](/legato/using/reeve/#running-without-asking).

Some decisions are signed: only you, at an enrolled phone or with a passkey on your computer, can allow them. An agent that has the web token still cannot. The TUI and Attention can deny a signed approval but not allow it, and say to use your phone. Some also need a verification: a fingerprint or a PIN on the device, not only a touch of a security key; such an approval refuses a touch alone. Which credential requests need a signature is set by the credential's rules; see [The credential broker](/legato/security/credential-broker/).
