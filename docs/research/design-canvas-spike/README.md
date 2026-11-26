# Throwaway design boundary spike

Question: can the same renderer run embedded and independently, with durable feedback on immutable revisions, without letting document JavaScript control its host?

This is **not** completed Legato integration or a Claude Design replacement. Both harnesses import `core.js`; Python's stdlib host imports no Legato code and needs no Legato service, token, database, provider login or model. Generation and the conversation are fixtures. No `.dc.html` runtime is implemented or imported.

## Run

From repository root (Python 3.10+):

```sh
scratch=$(mktemp -d /tmp/PROTOTYPE-design-XXXXXX)
python docs/research/design-canvas-spike/host.py --state "$scratch/PROTOTYPE-state.json"
```

Open the loopback URL printed by the command. `/standalone.html` is standalone; `/embedded.html` is a **Legato-like harness only**. Pin mode → select the design button → enter Comment → Save comment. Reload or restart with the same state file: feedback remains. Open the second harness, Simulate agent revision, then save a pin from the unchanged first tab: the API refuses it (409). Reload current to continue. View pinned revision retrieves old content without moving old feedback to new content. Frame and Viewport selectors explore two frames; viewport width fits available host space, not a pixel-perfect device emulator. No infinite canvas, layer editor, inter-frame prototype links or live push transport.

`Simulate agent revision` replaces the action label using authored fixture code. It does not read feedback or call an agent. A real agent loop is a separate unproved step.

Stop this scratch host with Ctrl+C. Remove its named state file and empty scratch directory when done. Do not run this host publicly.

## Focused reproducible checks

```sh
python docs/research/design-canvas-spike/check_api.py
```

This HTTP lifecycle test owns a temporary directory and server: save pin → read → host restart → pin still present → advance revision → old pin refused → old document still retrievable. The initial red result was `ModuleNotFoundError: host` before the host existed.

For browser evidence use a **new scratch state** (the script expects version 1), start the host above, then in another terminal:

```sh
npm install --prefix /tmp/legato-design-browser --cache /tmp/legato-design-npm playwright@1.65.1 --ignore-scripts --no-audit --no-fund
# With an installed matching Playwright Chromium; otherwise install one into scratch:
PLAYWRIGHT_BROWSERS_PATH=/tmp/PROTOTYPE-design-browsers /tmp/legato-design-browser/node_modules/.bin/playwright install chromium
SPIKE_URL=<printed-loopback-URL> \
PLAYWRIGHT_MODULE=/tmp/legato-design-browser/node_modules/playwright \
PLAYWRIGHT_BROWSERS_PATH=/tmp/PROTOTYPE-design-browsers \
node docs/research/design-canvas-spike/check_browser.cjs
```

`CHROMIUM_PATH=/absolute/path/to/existing/chromium` can reuse an installed browser instead. This investigation used that override; see `evidence/validation.txt` for actual versions. No live Legato browser was touched. The local distrobox route was unavailable (read-only `/run/user/1000/libpod`, wrapper absent); a fresh headless browser profile used an existing Chromium binary instead.

Screenshots in `evidence/` show standalone, embedded harness, stale rejection and phone layout. The browser check covers actual document script interaction, opaque-origin host-DOM denial, CSP network denial, API feedback saved/reloaded, second harness reading it, stale-tab refusal, pinned-revision viewing, and frame navigation. These are independent of the HTTP restart test, which covers disk persistence rather than DOM integration. Test audit: 2 checks retained, no redundant source/constant assertions; no production tests changed.

## Limits / threat model

- Only synthetic HTML/JS fixture input, one document, two frames, one allowed element anchor. No import/assets/runtime compatibility claim.
- `sandbox=allow-scripts` without same-origin plus CSP **before** document markup. Messages require this iframe window and its render token. Render tokens reject delayed messages from previous documents, not malicious code in the active document. HTML is never inserted into the host DOM; comments use `textContent`.
- DOM denial and network denial are demonstrated, **not** a complete sandbox audit. Scripts can still mutate their own document, spoof their own pin hints, navigate their own frame, or consume CPU. No resource quotas, iframe-navigation containment proof, cross-browser tests, supply-chain policy or hostile-bundle validation. No top-navigation/popups permissions are granted. Production should use a separate content origin and enforce response-header CSP/sandbox, not rely solely on `srcdoc`.
- The loopback host has **no authentication or authorization**. Foreign/opaque Origin writes are refused, but native local clients can write. No production CSRF/DNS-rebinding/Host-header defense or rate limits. Host inline-script policy is permissive to let `srcdoc` execute inline fixture scripts; do not deploy it.
- State is file-backed JSON, single-process, atomic replace but not a transactional/locked multi-writer store or fsync durability guarantee. All revisions retained; no quotas or GC. Feedback must target the current revision; historical pins remain readable. A production product may permit explicit comments on historical revisions instead.
- No agent delivery/resolution, real extension integration, auto-refresh, permissions, invitations, share-link revocation, public host, asset packaging, download/export controls, WYSIWYG editing or generation evaluation.

The capability-parity investigation and recommended next phases are in `../design-canvas-report.md`.
