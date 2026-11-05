---
title: Browser extension
description: Start a Legato review or discussion from a GitHub pull request, import tickets from Jira and Burnlite, and talk to the agent in Chrome's side panel.
sidebar:
  order: 100
---

:::caution
The browser extension is experimental. It runs in Chrome only, and you build it from source and load it unpacked.
:::

The Legato extension for Chrome adds Legato's actions to the pages you already read. On a GitHub pull request you can start a review or a discussion with an agent and read the result in Chrome's side panel, without going to Legato. The extension is a client of Legato's API: it starts Legato [workflows](/legato/using/workflows/) and makes no changes on GitHub.

## What it adds

On a GitHub pull request page:

- **Prepare Legato review** starts your review workflow on the pull request.
- **View review** opens the side panel on the review. The newest run is shown first, and while it runs the panel checks every four seconds.
- **Discuss PR** starts an agent in a worktree checked out at the pull request's head. It reads the change and waits for your questions in the side panel's **Discussion** tab. While a discussion is open, the button reads **Open discussion**.
- **Ask Legato**, beside **Cancel** in each inline comment form of the diff, puts the file and the lines you selected into the discussion's message box.

In the side panel, a file and line that the agent names are buttons. A click scrolls the pull request page to that line in **Files changed**.

On a Jira ticket or a Burnlite task, a button imports the ticket into Legato and opens the board with its card picked out. See [Import tickets](#import-tickets).

Before it starts a run, the extension shows the workflow, the repository, the PR number and your local checkout, and waits for **Confirm and start**.

On Stream Deck presses, the extension reuses your last Legato tab in the same Chrome window instead of opening a new one; see [Stream Deck](/legato/integrations/stream-deck/).

## Build and load it

You need Node.js 24 or later, pnpm, and Chrome 116 or later.

1. Build the extension:

   ```bash
   cd browser-extension
   pnpm install
   pnpm build
   ```

2. Open `chrome://extensions` and turn on **Developer mode**.
3. Click **Load unpacked** and choose `browser-extension/dist`.

After you build again, press the extension's reload button on `chrome://extensions`.

## Connect it to Legato

Open the extension's **Options** page and fill in:

| Field | What to enter | Default |
| --- | --- | --- |
| **Legato URL** | Your Legato web server's origin, such as `http://localhost:3080`. | none |
| **Bearer token** | Legato's web token, from `legato auth token`. | none |
| **Review workflow name** | The workflow that reviews a pull request. | `review-pr` |
| **Discussion workflow name** | The workflow that starts a discussion. | `discuss-pr` |
| **Input names** | The workflow inputs for the repository, the PR number and the checkout. | `repo`, `pr_number`, `primary_dir` |
| **Review artifact output key** | The workflow output that holds the review. | `review` |

Saving asks Chrome for access to that one Legato origin and tests the connection. With TLS on, Chrome must trust Legato's certificate; see [Web server](/legato/running/web-server/).

The extension keeps the token in Chrome's local storage. Only the extension's own pages read it. It never sends the token to GitHub or puts it in a page or an address. **Clear token** removes it.

## Import tickets

The extension adds an import button to the pages of the ticket providers Legato uses. It works for:

- **Jira**, when Legato is set up with your Jira site; see [Jira](/legato/integrations/jira/). The button shows on a ticket page, `/browse/<KEY>`.
- **Burnlite**, when Legato is connected to it; see [Burnlite](/legato/integrations/burnlite/). The button shows on a task's own page and when you open a task on a project page.

The extension learns each provider's site from Legato, so it has no site of its own. You choose which sites it may use:

1. Open the extension's **Options** page.
2. Under **Ticket sites**, find the provider. Each line shows its site and **Allowed** or **Not allowed**.
3. Click **Allow** and accept Chrome's prompt.
4. Reload the provider's page. The button shows at the bottom right.

The extension asks Legato for its providers when Chrome starts, when you save the options, and when you click **Ask Legato again**. When Legato moves a provider to a new site, allow the new site.

The button imports the ticket into the workspace Legato imports to by default, then opens the board with the new card picked out. When Legato is not connected to that provider, or the ticket is already a card from another provider, the button shows Legato's message.

## What Legato needs

The extension needs two things in Legato.

**The workflows.** Each must take the three mapped inputs, and every other required input must have a default:

```yaml
name: review-pr
inputs:
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
  primary_dir: {type: string, required: true}
```

The discussion workflow takes the same inputs. The **Discussion** tab follows the agent over Legato's live connection, so it needs an agent tool that has one: Chimera, or Claude Code or Codex through their bridges. See [Workflows](/legato/using/workflows/).

**One repository preset for the repository.** Exactly one entry in `repositories` must name the pull request's `owner/repo`, with your local checkout as `primary_dir`:

```yaml
repositories:
  - name: Legato
    repo: cpave3/legato
    primary_dir: /home/you/Projects/legato
```

The extension never guesses a checkout. When no preset or more than one matches, it says so.

## Troubleshooting

- **Permission was not granted**: save the options again and accept access to the Legato origin.
- **Unauthorized**: enter the token again and click **Save and test connection**.
- **Workflow was not found** or **input has wrong type**: make the workflow and input names in the options match your workflow.
- **No repository preset** or **Multiple presets match**: fix your `repositories` list as above.
- **Multiple Markdown artifacts**: set **Review artifact output key** to the output that holds the review.
- **Button missing on GitHub**: check that the address is a pull request (`https://github.com/<owner>/<repo>/pull/<number>`), then build and reload the extension.
- **Button missing on a ticket**: open the options and check that **Ticket sites** lists the provider as **Allowed**. When it is not listed, check that Legato is connected to Burnlite or set up with Jira, then click **Ask Legato again**. Reload the ticket's page.
