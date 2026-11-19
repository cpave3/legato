---
title: Scope
description: Pin comments on elements of the site you are building, in Chrome, and send them to Legato for an agent to fix.
sidebar:
  order: 75
---

:::caution
Scope is experimental. It is part of the [browser extension](/legato/integrations/browser-extension/), which runs in Chrome only and which you build from source.
:::

Scope lets you point at what is wrong on a page of your own site and say it in place. You click an element of a dev server or a staging site, write what should change, and send your comments to Legato as one batch. An agent then gets each comment with what it needs to find the element in your code: the page, ways to select the element, its styles and a screenshot. When the agent has dealt with a pin, the page shows it as fixed, or as won't fix, with the agent's note.

Use Scope for visual and layout fixes, where "the button under the price is too close to the border" is quicker to show than to describe.

## Allow a site

Scope does nothing on a site until you allow it. You allow one origin at a time, such as `http://localhost:5173`:

1. [Build and load the extension](/legato/integrations/browser-extension/#build-and-load-it), and connect it to Legato.
2. Open a page of your site.
3. Click the extension's button in Chrome's toolbar, or press `Alt+Shift+P`.
4. Chrome asks for access to that one site. Allow it.

Pin mode turns on at once. From then on Scope runs on every page of that site, including after Chrome restarts. You can allow several sites.

To change the shortcut, open `chrome://extensions/shortcuts`. To stop Scope on a site, open the extension's options and click **Remove** beside it under **Scope sites**. Your unsent comments on that site are kept.

## Pin comments

1. On an allowed site, click the toolbar button or press the shortcut to turn pin mode on.
2. Move the pointer over the page. The element under it is outlined.
3. Click the element. A numbered marker appears on it, with a comment box.
4. Write what should change, and click **Save** (or press Ctrl+Enter, ⌘+Enter on a Mac).
5. Press **Esc**, or the button again, to leave pin mode.

In pin mode, your clicks do not reach the page: a link does not open and a button does not act, so you can pin a menu item or a submit button. Leave pin mode to use the page again.

Your comments are drafts until you send them. They are kept in Chrome, so they survive a reload and moving between pages of the site, and every tab of the site shows them. The **Scope** panel at the bottom right lists the drafts of the whole site, with the page each is on. Edit a comment there, or click **Delete**. On the page, click a marker to change or delete its draft. Collapse the panel with its arrow.

A marker stays on its element as you scroll and resize. After a reload, Scope finds the element again. When it cannot, because the page changed, the marker stays where the element was, dashed and grey.

## What a pin records

When you place a pin, Scope records:

- the page: its address, title, window size, scroll position and pixel ratio;
- the element: several ways to select it, the most stable first (a test ID such as `data-testid`, its `id`, its role and accessible name, a class combination, then its position in the page), its text, and where it is on the page;
- its styles: the computed styles that matter for visual problems, where they differ from the browser's defaults, its classes and inline style, and how its parent lays it out;
- a screenshot of the element and the area around it, with the element outlined, at most 1024 pixels wide and 500 KB.

The Scope panel is hidden while the screenshot is taken, so it is not in the picture. Chrome lets Scope take a screenshot of a page only after you click the toolbar button or press the shortcut on that page. When you turned pin mode on from the panel instead, a pin may have no screenshot; the panel says so. Use the button or the shortcut, then pin again.

## Send

Click **Send** in the Scope panel. Your drafts go to Legato as one batch, and the drafts are cleared. A batch holds at most 20 pins; with more drafts, the first 20 go, and the panel says how many are left to send.

When no agent is subscribed to the site yet, the panel says so: the pins wait in Legato until an agent subscribes to it.

## How an agent gets your pins

An agent gets the pins of a site when it **subscribes** to the site's origin, such as `http://localhost:5173`. Agents are told to subscribe to the dev server they start or know of, so you rarely need to ask; if one has not, ask it to subscribe to the address.

- One agent holds a site at a time. When another agent subscribes to it, the new one takes it over, and the first is told.
- When you send a batch, the subscribed agent gets it as one message that starts with `[legato scope]`, one line per pin: the page, your comment, and where the element is. The web UI shows it as Legato's message, not yours. The agent then reads each pin in full with its [Legato tools](/legato/agent-tools/mcp-tools/), the screenshot included, fixes what it can, and marks each pin fixed or won't fix with a note.
- A subscription belongs to the task, so it survives a reload or restart of the agent, and an agent interrupted by a crash keeps it until you resume or dismiss it. It ends when the agent is stopped or exits, or the task is archived or deleted.
- When no agent is subscribed, or the subscribed agent is not running, the batch waits in Legato. The agent gets it when it subscribes, lists its pins or starts again.
- Sites, pins and subscriptions belong to the Legato the extension is connected to. Nothing is passed on to worker instances.

## Follow the sent pins

Sent pins show on their pages as round markers, and in the Scope panel under **Sent**, with their state and the agent's note:

| State | Marker | Meaning |
| --- | --- | --- |
| Sent | ↑ | Legato has the pin. No agent has taken it yet. |
| Delivered | → | An agent has the pin. |
| Fixed | ✓ | The agent fixed it. Its note says how. |
| Won't fix | ✕ | The agent will not change it. Its note says why. |

The page updates as the agent works; you do not need to reload it.

- To send a fixed or won't-fix pin back, click it, write a new comment and click **Reopen**. It goes back to Sent.
- To take back a pin no agent has taken yet, click **Withdraw**.
- To hide the fixed and won't-fix pins from the pages, click **Clear resolved**. Only your browser forgets them; Legato keeps them.

## When something goes wrong

The Scope panel shows what went wrong:

- **Could not reach Legato**: check that Legato is running and that the Legato URL in the extension's options is right. With HTTPS, Chrome must trust Legato's certificate; the message says how.
- **Legato refused the extension's token**: open the extension's options, check the bearer token and save.
- **Too large**: a batch takes at most 20 pins, and a screenshot at most 500 KB.
- **No screenshot**: click the toolbar button or press the shortcut on the page, then pin the element again.
- **The toolbar button shows `!`**: hover over it to see why. Scope works on `http` and `https` pages only, and only where you allowed access.
