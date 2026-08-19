# Legato Chrome extension

A local Manifest V3 extension that:

- adds **Prepare Legato review** and **View review** actions to GitHub pull-request pages;
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
- **Workflow name**: defaults to `review-pr`;
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

### Review side panel

**View review** opens Chrome's native side panel for the current PR. The extension checks for matching review runs when a PR page opens: when one exists, the floating action offers only **View review**; otherwise it offers the review-start workflow as well. The panel has an always-available **Refresh review** action and uses the active tab URL plus the configured workflow/input names and canonical repository preset to locate matching runs, newest first.

The newest run is selected by default; prior matching runs remain selectable. A review is considered ready when a `text/markdown` artifact exists, even if the workflow is still `waiting_manual` for cleanup. The configured output key wins; if it is absent, the only Markdown artifact is accepted, while multiple Markdown artifacts produce an explicit configuration error.

An active newest run refreshes every four seconds while the panel is open. Polling stops when the artifact appears, the run fails/cancels, a historical run is selected, or the panel unloads. The panel follows GitHub tab changes and never places the bearer token in its URL or page content.

Markdown is treated as untrusted agent output. It is rendered with bundled dependencies, sanitized before insertion, strips scripts/forms/frames/remote media, and permits only hardened HTTP(S) links. No renderer or script is loaded from a CDN.

The extension requires Chrome 116+ for user-gesture `sidePanel.open()`. After upgrading from an older extension build, reload it in `chrome://extensions`; the packaged `sidepanel.html` is registered through the manifest's `side_panel.default_path`.

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
4. Click **View review** on PRs with no run, active runs, failed runs, `waiting_manual` reviews, and multiple historical runs; verify the panel state and selection.
5. Verify a malicious Markdown artifact cannot create scripts, forms, frames, remote images, or unsafe links.
6. Verify invalid token, missing workflow, missing/ambiguous presets, and ambiguous Markdown artifacts show actionable errors.
7. Import a Jira ticket and verify the board scrolls to its cyan-highlighted card, then removes the highlight and query parameter after eight seconds.
8. Make a Legato tab active, switch to another desktop application if desired, press each Stream Deck navigation type, and verify the prior tab is reused without a duplicate.
9. Make a non-Legato tab active and verify Stream Deck leaves the new Legato tab.
10. Disable the extension and verify Stream Deck destinations still open normally.
