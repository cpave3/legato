# Legato Chrome extension

A local Manifest V3 extension that:

- adds **Prepare Legato review**, **View review** and **Discuss PR** actions to GitHub pull-request pages;
- adds an **Import to Legato** action to the ticket pages of the providers Legato syncs (Jira and Burnlite), on the sites Legato reports;
- lets you pin comments on elements of your own site (**Scope**) and send them to Legato for an agent to act on;
- resolves a configured user workflow and local repository preset;
- displays the resolved details for confirmation before starting the run; and
- reuses Chrome's last active Legato tab for marked Stream Deck navigation when it is in the same Chrome window.

The extension is an external API client. It does not contain PR-review logic and does not make GitHub API changes.

## Build and install

Requirements: Node.js 24+ and pnpm.

```bash
cd browser-extension
pnpm install
pnpm test
pnpm build
```

Then open `chrome://extensions`, enable **Developer mode**, select **Load unpacked**, and choose `browser-extension/dist`.

After rebuilding, use the extension page's reload button. This project does not automate Chrome or OpenDeck acceptance testing.

## Configure

Open the extension's **Options** page and enter:

- **Legato URL**: one HTTP(S) origin, such as `http://localhost:3080`;
- **Bearer token**: the token from Legato's data directory when server authentication is enabled;
- **Review workflow name**: defaults to `review-pr`;
- **Discussion workflow name**: defaults to `discuss-pr` (see Discussing a pull request);
- **Input names**: default to `repo`, `pr_number`, and `primary_dir`;
- **Review artifact output key**: defaults to `review`.

Saving requests access to that exact Legato origin and tests authenticated workflow discovery. Chrome displays broad HTTP/HTTPS capability because Manifest V3 optional host permissions must declare the origins that may later be requested; the extension requests and retains only the configured origin at runtime.

Below the form, **Ticket sites** lists the ticket providers Legato reports and whether the extension may use each one's site; see Ticket import.

The token is stored in `chrome.storage.local`, not sync storage. Only extension-owned contexts—the options page for connection testing and the service worker for actions—read it. GitHub content scripts and page code cannot access it. It is never sent to GitHub, inserted into the page, included in navigation URLs, or logged. Use **Clear token** to remove it.

## Workflow and repository prerequisites

The default mapping expects a discovered user workflow equivalent to:

```yaml
name: review-pr
inputs:
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
  primary_dir: {type: string, required: true}
```

The workflow may have additional optional/defaulted inputs, but every required input without a default must be one of the three mapped inputs.

Legato workflow configuration must also contain exactly one repository preset whose `repo` matches GitHub's `owner/repo` (case-insensitively):

```yaml
workflows:
  repositories:
    - name: Legato
      repo: cpave3/legato
      primary_dir: /home/user/Projects/legato
```

The GitHub action first discovers and validates the workflow and preset. It then displays the workflow, repository, PR number, and local checkout. The run starts only after **Confirm and start**. The worker repeats discovery and validation immediately before POSTing to Legato.

### Discussing a pull request

**Discuss PR** starts a conversation about the pull request instead of a review: Legato checks out the exact PR head in a worktree of its own and starts an agent there that reads the change set and then waits for your questions. The action resolves and shows the workflow, repository and checkout first, as the review does; **Confirm and start** opens the side panel on its **Discussion** tab and starts the run. The panel picks the agent up as soon as it starts.

While a discussion of the PR is going, the action reads **Open discussion** and opens the side panel on it instead of starting another.

A finished review counts as a discussion. The review workflow keeps its agent once the review is written (`keep_agent: true`) and waits for it to end, so while that agent runs, **Discuss PR** reads **Open discussion** and talks to it: it has read the change set closely already. The extension takes a review run for one when the run is `waiting` on a step with `trigger: agent_ended` (read from the run's `definition_json`); otherwise it looks for a discussion run, and starts one if there is none. Killing the review agent removes the review's worktree, as for a discussion.

The **Discussion** tab follows the agent over Legato's WebSocket, as the web UI's native panel does: it reads the session's snapshot once (`/api/agents/<task>/chimera/snapshot`), subscribes to its live events (`subscribe_agent`), and reduces them with the web UI's own reducer (`web/src/lib/nativeAgentSync.ts`, bundled from the web app). What you type is sent as `agent_message` (queued while the agent works), and a permission request can be allowed or denied there (`agent_permission`). It shows what you sent, the agent's replies as sanitized Markdown and a line for each tool call; a question the agent asks is answered in Legato (**Open in Legato**). The socket reconnects by itself and picks up from the last event it saw. It needs an agent with a native endpoint (Chimera, or Claude Code and Codex through their bridges). With TLS on, the socket is `wss:`, so Chrome must trust Legato's certificate as it must for the API. A discussion ends when its agent does: **Kill agent** in Legato (or **End discussion** on the run page) stops the agent and removes the worktree. **Reload agent** does not end it.

It uses a user workflow with the same three inputs as the review one, which the extension finds by the **Discussion workflow name**. The one in use is `~/.config/legato/workflows/discuss-pr.yaml`: `github.pr.prepare`, then `agent.dispatch` on the workflow's own task (an agent the workflow does not wait on), then an `agent.stop` step with `trigger: agent_ended` and a `worktree.remove` step. See [Workflows](https://cpave3.github.io/legato/using/workflows/) for `agent_ended`.

### Asking about lines of the diff

Each inline comment form in a pull request's diff (click a line number, or drag across several) gets an **Ask Legato** button beside **Cancel**. It opens the side panel on **Discussion** and puts the file and the lines in the message box, ready for the question: ``In `path/to/file.cs`, lines 104–109: ``. Lines on the base side say so (`of the base (before this PR)`). The file comes from the diff table's `aria-label`; the range from the address GitHub keeps for a selection (`#diff-<file>R104-R109`), or the form's own line when the address names no range ending there. The text waits in the tab's session storage until a discussion shows, so it also works when the panel was closed or the discussion is still starting. The comment form is left as it was.

### Review side panel

**View review** opens Chrome's native side panel for the current PR. The extension checks for matching review runs when a PR page opens: when one exists, the floating action offers only **View review**; otherwise it offers the review-start workflow as well. The panel has an always-available **Refresh review** action and uses the active tab URL plus the configured workflow/input names and canonical repository preset to locate matching runs, newest first.

The newest run is selected by default; prior matching runs remain selectable. A review is considered ready when a `text/markdown` artifact exists, even if the workflow is still `waiting_manual` for cleanup. The configured output key wins; if it is absent, the only Markdown artifact is accepted, while multiple Markdown artifacts produce an explicit configuration error.

An active newest run refreshes every four seconds while the panel is open. Polling stops when the artifact appears, the run fails/cancels, a historical run is selected, or the panel unloads. The panel follows GitHub tab changes and never places the bearer token in its URL or page content.

### Going to a file the agent cites

In the review and in the agent's replies on **Discussion**, a file with a line is a button: `` `path/to/file.cs:104` ``, a range (`:47-53`, `:59–60`), GitHub's `#L12`, or the same in plain text. A file in inline code with a folder and no line is one too. A click shows that place in the PR tab, as the PR Review Briefings extension does: the page goes to **Files changed** (by GitHub's own tab, or by loading it), the file is revealed through the file tree, expanded or loaded when collapsed or large, and the line is scrolled to the middle and flashed. Agents often cite a file by its name alone, or by its full path in their worktree: the page matches either against the PR's files, and says so when a name fits more than one. A file the PR does not change, or a line outside the diff, gets a message at the foot of the panel (for the line, the file is shown). The jump code is `src/content/pr-jump.ts`; the extension's `scripting` permission lets the panel put the page script into a PR tab that was open before the extension was reloaded.

Markdown is treated as untrusted agent output. It is rendered with bundled dependencies, sanitized before insertion, strips scripts/forms/frames/remote media, and permits only hardened HTTP(S) links. No renderer or script is loaded from a CDN.

The extension requires Chrome 116+ for user-gesture `sidePanel.open()`. After upgrading from an older extension build, reload it in `chrome://extensions`; the packaged `sidepanel.html` is registered through the manifest's `side_panel.default_path`.

## Look

The two widgets and the side panel follow the web app's new look: its `lw-` colours (a solid surface, one violet accent, green and red for success and failure) and Geist. The extension carries its own copy of Geist (`public/fonts`, SIL Open Font License) and adds it to the page under its own names (`Legato Geist`), so a site's own type is never changed; where a page refuses the fonts, the widgets fall back to the system's. The action waiting for confirmation carries the accent; the others are outlined.

## Ticket import (Jira and Burnlite)

The extension has no ticket site of its own. It asks Legato (`GET /api/ticket-providers`) for the connected providers that have a site: each one's `name`, `label`, `web_origin` and `key_pattern`. It stores the list in `chrome.storage.local` (`ticketProviders`) and, for each provider whose origin the person has allowed, registers the content script `tickets.js` there with `chrome.scripting.registerContentScripts` (id `legato-tickets-<name>`, `persistAcrossSessions`). A script whose provider moved is updated; one whose provider is gone or no longer allowed is unregistered. The service worker does this when it is installed or Chrome starts, when a host permission is added or removed (from the stored list, without asking Legato), and when the options page asks: on load, after a save, and from **Ask Legato again**. `src/ticket-sites.ts` holds the logic.

The options page lists the sites under **Ticket sites** with **Allowed** or **Not allowed**. **Allow** asks Chrome for that one origin (`chrome.permissions.request`, from the click) and then re-registers. `optional_host_permissions` already covers any http(s) origin.

On a page, `tickets.js` finds the provider whose `web_origin` is the page's origin and reads the key:

- **Jira**: `/browse/<KEY>`, uppercased.
- **Burnlite**: the open task or milestone drawer's `[data-testid="task-drawer"] [data-testid="item-key"]` or `[data-testid="milestone-drawer"] [data-testid="item-key"]` (a key link `/<KEY>` redirects to a project page with the drawer open). A milestone's key is from the same series as task keys, and Legato imports it as a card that lists the milestone's tasks. The drawer is a Radix modal that closes on a press or focus outside it, so on Burnlite there is no bubble: the script puts a **♩ Import to Legato** button (`#legato-ticket-inline`) in the drawer's header, before **Copy reference** (`copy-task-reference` or `copy-milestone-reference`) and with its classes, and follows the drawer as it opens another task or closes. The bubble used elsewhere keeps its own `pointerdown`, `mousedown`, `touchstart` and `focusin` from the page, and is not removed while its import runs.

A key counts only when it matches the provider's `key_pattern`. Other provider names get no button.

The bottom-right action imports the ticket through `POST /api/remote/import` with `{ticket_id, provider}`. The request omits `workspace_id`, so the Legato server's configured default import workspace applies. After import, the extension opens `/board?highlight_task=<local-task-id>`. Legato answers `503` when that provider is not connected and `409` when the key is already a task from another provider; the action shows Legato's message and offers a retry.

The board selects the imported task wherever its state placed it, scrolls it into view, and gives it a distinct cyan highlight. After eight seconds, the board removes both the highlight and the `highlight_task` parameter with a history replacement; unrelated query parameters are preserved.

The content script sends only the provider and the parsed key to the service worker (`importTicket`). The service worker checks both, owns the authenticated Legato request, and suppresses duplicate clicks per provider and key while an import is active. A Chrome navigation failure is reported as an imported ticket rather than inviting another import.

The widgets' fonts are web-accessible from any site (`use_dynamic_url`), since a provider's site is not known when the extension is built.

With **Open ticket imports in the Legato desktop app** (`openImportsInDesktop`, off unless chosen), the service worker does not call Legato: it answers `importTicket` with `desktopLink`, `legato-desktop://import?provider=<provider>&key=<KEY>`, and the content script opens it with a link click, which needs the page's user gesture. The desktop app reads it, asks which server, and imports there (see `docs/ai/desktop.md` § Links from the system).

## Scope: pins on your own site

Scope lets you point at elements of a site you are building (a dev server such as `http://localhost:5173`, or a staging site), write what should change, and send the comments to Legato as one batch for an agent to act on. Each pin carries what an agent needs to find and judge the element: the page, selectors for it, its styles and a screenshot.

### Allowing a site

Scope is off everywhere until you allow a site, one origin at a time:

1. Open a page of the site.
2. Click the extension's toolbar button, or press its shortcut (`Alt+Shift+P` unless you change it at `chrome://extensions/shortcuts`).
3. Chrome asks for access to that one origin (`http://localhost:5173/*`). Allow it.

The extension stores the origin in `chrome.storage.local` (`scopeOrigins`) and registers the content script `scope.js` there with `chrome.scripting.registerContentScripts` (id `legato-scope-<origin>`, `persistAcrossSessions`), so it runs on every page of the site from then on; a tab that was already open gets the script at once. The registrations are reconciled as the ticket sites' are (`src/scope-origins.ts`): when the extension is installed or Chrome starts, and when a host permission is added or removed. An origin whose access was removed in Chrome stays listed but gets no script. Several sites may be allowed. The first click on a new site also turns pin mode on. Chrome's match patterns ignore the port, so a script registered for `http://localhost` also lands on `http://localhost:3000`; there it stays idle, and the first click on the button there allows that origin and starts pin mode without a reload.

The options page lists the sites under **Scope sites**, each with **Remove**: Remove unregisters the script and gives Chrome's access back, unless the extension still needs that origin for Legato itself or for a ticket site. A site's drafts stay in storage.

The toolbar button acts for Scope only because nothing else is bound to it: the extension has no popup, and the side panel opens from the GitHub widgets (`chrome.sidePanel.open`), not from the button (no `openPanelOnActionClick`). A side-panel behaviour on the button would stop `chrome.action.onClicked` from firing. When the button cannot act (the page is not http(s), access was refused, the page could not be reached) its badge shows `!` and its tooltip says why.

### Pin mode

On an allowed site, the button (or shortcut) toggles pin mode. **Esc** leaves it. In pin mode:

- the element under the pointer is outlined;
- a click places a numbered marker on that element and opens a comment box; **Save** (or Ctrl/⌘+Enter) keeps the pin as a draft, **Cancel** drops it;
- the page's own handlers do not hear the presses and clicks: the overlay listens on `window` in the capture phase and calls `preventDefault` and `stopImmediatePropagation` on `pointerdown`, `mousedown`, `pointerup`, `mouseup`, `click`, `dblclick` and `auxclick`. A page listener added to `window` in the capture phase before the overlay's still runs first.

The overlay lives in a closed shadow root (`<legato-scope>`), so the site's CSS does not reach it, and is drawn in the web app's `lw-` colours and Geist, like the other widgets. Its own presses are kept from the page, so a site's modal does not close under it. While a pin's context is captured, the overlay is hidden, so it is not in the screenshot.

A marker follows its element as the page scrolls and resizes. After a reload, a draft's element is found again from its selectors and text (`locateTarget`: a selector whose element still has the pin's text, else the one element with that text, else the first selector that matches one element). When none is found, the marker stays at the pin's saved place on the page, dashed and grey, marked lost; it is looked for again whenever the page changes.

The **Scope** tray (bottom right) lists the site's drafts across its pages, with the page of each; edit a comment there, or **Delete** a draft. Click a marker to edit or delete its draft on the page. The tray collapses with its arrow. It shows while pin mode is on, or while there are drafts or sent pins.

### What a pin records

- **Page**: address, title, viewport, scroll, device pixel ratio.
- **Target**: selector candidates, stablest first, each checked to match exactly that one element: a test ID (`data-testid`, `data-test-id`, `data-test`, `data-cy`, `data-qa`), the `id`, role and accessible name (written `role=button[name="Save"]`), a class combination unique on the page (`button.btn-primary`), and last the `nth-child` path from `body`. Also a text snippet (200 characters at most) and the element's box in page coordinates.
- **Styles**: about thirty computed properties that explain most visual bugs (box, layout, colour, type), keeping only values that differ from the browser's default for that tag, read from a fresh element of the same tag in an empty iframe of the overlay's own; the class list; the inline style; the parent's layout (`display`, `position`, flex and grid properties, `gap`, `overflow`, ...).
- **Screenshot**: the service worker captures the visible tab (`chrome.tabs.captureVisibleTab`), crops it around the element with a 24-pixel margin, outlines the element, scales it to at most 1024 pixels wide and encodes a JPEG, lowering the quality and then the size until it is within 500 KB. The crop reads the ratio from the picture, so the pixel ratio and page zoom both count, and it stops at the edges of the visible page. Without a visible part of the element, the pin has no screenshot. Chrome lets an extension capture a tab only with the `activeTab` permission (or access to every site, which the extension does not ask for): the access you gave the site is not enough. `activeTab` is given for a page when you use the toolbar button or its shortcut there, which is how pin mode starts. When Chrome refuses (pin mode was turned on from the tray of a page you reached later, say), the pin is kept without a screenshot and the tray says why: use the button or the shortcut on the page, then pin again.
- **Source**: the component that drew the element, on a React 18 or 19 development build served by Vite: `{name, file, line, column, owners, confidence}`, with confidence `exact` (file and line), `file-only`, `name-only` (a production build: names only, often minified) or `none`, which is left out of the pin. When the pin is placed, the content script marks the element with `data-legato-scope-probe` and asks the service worker, which runs `scope-source.js` in the page's MAIN world (`chrome.scripting.executeScript`, `world: "MAIN"`); that script reads the element's React fibre with [bippy](https://github.com/aidenybai/bippy)'s helpers (no DevTools hook is installed, and no script runs at `document_start`): the component's name, its owners (nearest first, at most six), and raw frames: React 19's `_debugStack` (the stack when the JSX ran) or React 18's `_debugSource` (the file and line the JSX transform wrote), then the same for each owner. The service worker takes the script's value as executeScript's result, rejects it unless it has the expected shape and bounds (at most 20 frames, names that are names), keeps only frames on the page's origin, and resolves the first project frame: it fetches the module from the origin (no cookies, no redirects, 1.5 s, 8 MB) and maps the frame through the module's inline source map with `@jridgewell/trace-mapping`. Vite's URL prefixes are stripped (`/@fs/`, the origin, `?t=`, `?v=`), so a project file is named from the root (`src/App.tsx`) and a file outside it by its path on disk; dependencies (`node_modules`) and Vite's own modules are skipped, so an element a library's component drew is placed where the project used that component. React 18 gives a file on disk; its project path comes from the modules the page loaded (resource timing), else the full path. The content script waits at most 1.5 s, then saves the pin without a source.

Drafts are kept per origin in `chrome.storage.local` (`scopeDrafts:<origin>`, each screenshot under its own `scopeShot:<origin>:<id>`), so they survive reloads and navigation within the site, and every tab of the site shows the same ones. A tab reads them itself but changes them through the service worker (`scopeDraftAdd`, `scopeDraftUpdate`, `scopeDraftRemove`, `scopeClearedAdd`), which makes one change at a time: `chrome.storage` has no transactions, and two tabs rewriting the list at once would lose a draft or bring a deleted one back. **Send** first waits for comment edits still being saved, and saves one still being typed, so it never sends an older comment. The extension asks for `unlimitedStorage`, since twenty screenshots of 500 KB would fill Chrome's default 10 MB.

### Sending

**Send N pins** in the tray posts the drafts as one batch (`POST /api/scope/batches`) through the service worker, which alone reads the token. A batch takes at most 20 pins; with more drafts, the oldest 20 go and the tray says how many are left. Once Legato accepts the batch, its drafts are deleted. When Legato answers `subscribed: false`, the tray says the pins wait in Legato's pool until an agent subscribes to the site.

### Sent pins and their states

On a page of the site, the content script asks the service worker for the site's pins (`GET /api/scope/pins?origin=`). Each sent pin shows on its page as a round marker with its state, and in the tray under **Sent** with its comment and the agent's note:

| State | Marker | Means |
| --- | --- | --- |
| Sent | ↑ | Legato has it; no agent has taken it yet. |
| Delivered | → | An agent has it. |
| Fixed | ✓ | The agent fixed it; its note says how. |
| Won't fix | ✕ | The agent will not change it; its note says why. |

While a tab of the site is open, the service worker keeps Legato's WebSocket (`/ws?token=…`) open; on `scope_changed` for the site it reads the pins again and tells the site's tabs. It reconnects after a drop and reads every watched site's pins again. Chrome stops an idle service worker after 30 seconds, and an open socket alone does not keep it running; so while the socket is open the worker sends `{"type": "ping"}` every 20 seconds, which Legato answers with `pong`. A tab watches again when it comes back into view, in case the worker was stopped anyway.

- **Reopen** a fixed or won't-fix pin with a new comment (`POST /api/scope/pins/{id}/reopen`); it goes back to Sent.
- **Withdraw** a pin no agent has taken yet (state Sent; `POST /api/scope/pins/{id}/withdraw`).
- **Clear resolved** hides the fixed and won't-fix pins from the site's pages. Only this browser forgets them (`scopeCleared:<origin>`); Legato keeps them. A cleared pin that is reopened shows again.

Errors show in the tray: Legato unreachable (with the same advice about running Legato and trusting its certificate as the other widgets), the token refused (`401`), and a batch over Legato's limits (`413`). A failure to read the sent pins does not open the tray by itself.

## Stream Deck integration

Current Stream Deck builds append `legato_source=streamdeck` immediately before opening browser destinations. When Chrome sees that marker, the extension reuses the immediately preceding active tab only if:

- both tabs are in the same normal Chrome window;
- the preceding tab and destination exactly match the configured Legato origin; and
- the history snapshot predates the marked navigation.

Per the selected behavior, Chrome does not need to be the focused desktop application: its last active Legato tab can be reused. Unmarked links are never intercepted. Missing history, mismatched origins/windows, or failed tab operations leave the newly opened tab in place. The marker is removed after handling where Chrome permits it.

Without the extension, Stream Deck links remain valid ordinary Legato URLs; Legato ignores the marker.

## Troubleshooting

- **Permission was not granted**: save options again and accept access to the exact configured origin.
- **Unauthorized**: update the bearer token and use **Save and test connection**.
- **Workflow was not found / input has wrong type**: update the configurable workflow/input names to match the installed YAML definition.
- **Side panel file path must exist**: rebuild with `pnpm build`, confirm `dist/sidepanel.html` exists, then reload the unpacked extension from `chrome://extensions`.
- **Multiple Markdown artifacts**: set **Review artifact output key** to the intended artifact's workflow output key.
- **No repository preset**: configure one whose `repo` is canonical `owner/repo` and whose `primary_dir` is the local primary checkout.
- **Multiple presets match**: remove the ambiguity; the extension never guesses a checkout.
- **Button missing on GitHub**: confirm the URL is `https://github.com/<owner>/<repo>/pull/<positive-number>`, then rebuild and reload the extension.
- **Button missing on a ticket**: open the options and check that **Ticket sites** lists the provider as **Allowed**. When it is missing, check that Legato is connected to Burnlite or configured with Jira (`GET /api/ticket-providers` lists only those with a site), then click **Ask Legato again**. Reload the ticket's page. The script is registered only for the exact origin Legato reports.
- **A Scope pin has no screenshot** and the tray says Chrome did not let Scope take one: click the toolbar button or press the shortcut on that page, then pin again (see [What a pin records](#what-a-pin-records)).
- **Stream Deck leaves a new tab**: confirm the extension URL matches the Stream Deck server origin exactly and that the prior Legato tab was active in the same Chrome window.

## Manual acceptance checklist

1. Configure URL/token and confirm workflow discovery succeeds.
2. Open a GitHub PR by full load and client-side navigation; verify exactly one action appears.
3. Prepare the action, verify all resolved details, confirm, and check that the returned run opens.
4. Click **Discuss PR**, confirm, and check that the side panel opens on **Discussion**, picks the agent up, shows its orientation and answers a question typed there; reload the PR page and check the action reads **Open discussion** and opens the panel; kill the agent and check the run finishes and its worktree is gone.
5. Open a comment form on one line and on a range, click **Ask Legato** on each, and check the panel's message box gets the file and the right lines.
6. Click **View review** on PRs with no run, active runs, failed runs, `waiting_manual` reviews, and multiple historical runs; verify the panel state and selection.
7. Verify a malicious Markdown artifact cannot create scripts, forms, frames, remote images, or unsafe links.
8. Verify invalid token, missing workflow, missing/ambiguous presets, and ambiguous Markdown artifacts show actionable errors.
9. In the options, check **Ticket sites** lists Jira and Burnlite as Legato has them, **Allow** each, and check each turns to **Allowed**.
10. Import a Jira ticket and verify the board scrolls to its cyan-highlighted card, then removes the highlight and query parameter after eight seconds.
11. Import a Burnlite task, and a milestone, with the **Import to Legato** button in its open drawer (the button follows the drawer as you open another task or close it, and the drawer stays open when you press it), and check a task already imported from Jira under the same key shows Legato's refusal.
12. Make a Legato tab active, switch to another desktop application if desired, press each Stream Deck navigation type, and verify the prior tab is reused without a duplicate.
13. Make a non-Legato tab active and verify Stream Deck leaves the new Legato tab.
14. Disable the extension and verify Stream Deck destinations still open normally.
15. On a page of a dev server, click the toolbar button: Chrome asks for that origin only; allow it, and check pin mode turns on and the options page lists the site under **Scope sites**. With `http://localhost` allowed, open `http://localhost:<another port>` and click the button: check it asks for that origin and pin mode starts without a reload.
16. In pin mode, hover and click elements that have click handlers (a link, a button, a menu): check each is outlined, gets a numbered marker and a comment box, and the page does not react. Press **Esc** and check the page reacts again.
17. Save three drafts on two pages of the site; reload, navigate within the site, and open a second tab: check the tray lists all three, each marker sits on its element and follows it as you scroll and resize, and a draft whose element you removed shows dashed at its old place. Check each draft sent later carries its screenshot; on a page reached after turning pin mode on, turn it off and on from the tray, pin, and check the tray says there is no screenshot and why. Edit a draft in the tray of one tab while adding one in another, then click **Send** at once from the first: check no draft is lost and the edited comment is the one sent.
18. Press the shortcut on a site not yet allowed and refuse Chrome's prompt: check nothing is registered and the button's badge shows `!`.
19. Send the drafts with no agent subscribed to the site: check the tray says they wait in Legato's pool and the drafts are gone; check the pins show as Sent.
20. Leave a page of the site visible and untouched for two minutes, then have an agent mark one pin fixed with a note and another won't fix: check both pages update without a reload; reopen the fixed one with a comment, withdraw a Sent one, and **Clear resolved**; reload and check cleared pins stay hidden.
21. Stop Legato and press **Send**: check the tray shows the unreachable message with its advice; set a wrong token and check the token message.
22. Remove the site in the options and reload a page of it: check Scope no longer runs there.
23. On a React 19 app and a React 18 app served by `vite` (dev), pin an element your code renders and one a library component renders, send them, and check with `page_pin_get` that each names the component and the file and JSX line (`exact`), the library's at the line that uses the library component. Pin a page without React, and a `vite build && vite preview` build of the app: check the first has no component source and the second at most `name-only`, and that placing those pins does not wait.
