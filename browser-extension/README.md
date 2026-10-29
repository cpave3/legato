# Legato Chrome extension

A local Manifest V3 extension that:

- adds **Prepare Legato review**, **View review** and **Discuss PR** actions to GitHub pull-request pages;
- adds an **Import to Legato** action to `urbanx-development.atlassian.net` Jira ticket pages;
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

It uses a user workflow with the same three inputs as the review one, which the extension finds by the **Discussion workflow name**. The one in use is `~/.config/legato/workflows/discuss-pr.yaml`: `github.pr.prepare`, then `agent.dispatch` on the workflow's own task (an agent the workflow does not wait on), then an `agent.stop` step with `trigger: agent_ended` and a `worktree.remove` step. See `docs/workflows.md` for `agent_ended`.

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

## Jira ticket import

On `https://urbanx-development.atlassian.net/browse/<ISSUE-KEY>`, a bottom-right action imports the issue through Legato's existing `POST /api/remote/import` API. The request omits `workspace_id`, so the Legato server's configured default import workspace applies. After import, the extension opens `/board?highlight_task=<local-task-id>`.

The board selects the imported task wherever its Jira state placed it, scrolls it into view, and gives it a distinct cyan highlight. After eight seconds, the board removes both the highlight and the `highlight_task` parameter with a history replacement; unrelated query parameters are preserved.

The Jira content script sends only the parsed issue key to the service worker. The service worker owns the authenticated Legato request. Duplicate clicks are suppressed while an import is active, and a Chrome navigation failure is reported as an imported ticket rather than inviting another import.

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
- **Button missing**: confirm the URL is either `https://github.com/<owner>/<repo>/pull/<positive-number>` or `https://urbanx-development.atlassian.net/browse/<ISSUE-KEY>`, then rebuild and reload the extension.
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
9. Import a Jira ticket and verify the board scrolls to its cyan-highlighted card, then removes the highlight and query parameter after eight seconds.
10. Make a Legato tab active, switch to another desktop application if desired, press each Stream Deck navigation type, and verify the prior tab is reused without a duplicate.
11. Make a non-Legato tab active and verify Stream Deck leaves the new Legato tab.
12. Disable the extension and verify Stream Deck destinations still open normally.
