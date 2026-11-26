# LEG-665 — throwaway human/agent edit reconciliation proof

**Question:** can genuine direct text/property/drag edits share a revision-aware transaction engine with structural agent-style patches, without losing stable identity, layout, feedback or undo?

**Verdict:** yes for this deliberately constrained scene graph. This does NOT prove lossless editing of arbitrary HTML/JS, generation quality, multi-user collaboration, full Claude Design parity, or production integration.

The evidence is **automated UI-driven interaction**, not human usability sign-off. Playwright types into the actual contenteditable canvas, fills the property inspector and performs a real pointer drag. It does not invoke an edit-fixture button or replace model state. Agent patches are explicitly **authored/simulated JSON fixtures**; no model/provider/agent integration is called.

## Run the demo

Node 22.13+ (tested 24.22.3), existing repo web dependencies (TypeScript 5.9.3):

```sh
scratch=$(mktemp -d /tmp/PROTOTYPE-editor-XXXXXX)
node docs/research/design-editor-spike/run.mjs --state "$scratch/PROTOTYPE-state.json"
```

Open the printed loopback URL. `/embedded.html` mounts the SAME UI/core in a differently labeled **harness**, not the real Legato application. No running Legato service, token, database, account or provider is needed.

`run.mjs` compiles the TS source then starts a Node stdlib scratch host. TypeScript is a build-time tool, not a Legato runtime dependency. For a copy outside this repository, install `typescript@5.9.3` in scratch tooling and set `TSC_PATH=/absolute/path/to/typescript/bin/tsc`; all source and runtime code remain confined to this folder. The default compiler location is the existing repo web toolchain. Disposable Node host is not a production stack decision; production recommendation remains TypeScript core + Go services.

### Guided interaction

1. In the initial state, choose Wrap details and **Capture agent base** (r0).
2. Turn on Pin mode, click Continue, add a comment; turn Pin mode off.
3. Double-click the heading, type new text, and Tab out to commit. Select `cta` in Properties, change Fill to `#245e9b`, and Tab out. Drag its Move handle.
4. **Apply authored patch** refuses the stale base. **Rebase nonconflicting** explicitly inserts a group and reparents body/cta while preserving their CURRENT world positions and human properties/text.
5. Undo latest transaction removes the grouping but keeps preceding human edits. Redo restores it. These are GLOBAL LINEAR transactions, not selective per-actor undo.
6. Save session → reload page (or restart host with same state path). Document, revisions, pins, audit and undo/redo survive.
7. Choose Rename action, Capture agent base, edit the action text directly, then Rebase nonconflicting: same-node/same-property conflict is refused, with base/current/proposed values shown.
8. Choose Remove action, Capture agent base, Apply authored patch: its old pin is explicitly Orphaned. Undo restores the ORIGINAL node identity and historical pin target. IDs cannot be recycled into unrelated new nodes.

New unsaved sessions require Save session for disk persistence. Load saved session discards local unsaved edits. Host CAS rejects a stale save from another client; it does not merge remote sessions. Stop only this scratch host with Ctrl+C. Keep source and evidence for review; no public deployment.

## Tests and evidence

From repository root:

```sh
node docs/research/design-editor-spike/build.mjs
node --test --test-concurrency=1 docs/research/design-editor-spike/check_core.mjs docs/research/design-editor-spike/check_storage.mjs
```

Six focused core/storage tests cover real `Editor.apply`, undo/redo, pin and serialization entrypoints plus HTTP persistence/CAS and host restart. Initial red results: missing core module, then missing serialize/pin/remove support, and missing storage host before implementations.

For browser evidence start a **fresh unsaved state** with the run command, then:

```sh
# Use existing scratch Playwright install, or install a pinned one outside the repo:
npm install --prefix /tmp/legato-design-browser --cache /tmp/legato-design-npm playwright@1.65.1 --ignore-scripts --no-audit --no-fund
# If needed, install matching Chromium with Playwright's install command in scratch.
SPIKE_URL=<printed-loopback-URL> \
PLAYWRIGHT_MODULE=/tmp/legato-design-browser/node_modules/playwright \
CHROMIUM_PATH=/absolute/path/to/installed/chromium \
node docs/research/design-editor-spike/check_browser.cjs
```

Omit CHROMIUM_PATH when using Playwright's matching installed browser. This investigation reused an existing Chromium 155 binary with a fresh headless profile, not a running user browser/Legato. Only one browser suite ran at a time.

`evidence/validation.txt` records actual versions, six passing core/storage tests and TWO fresh-state UI passes after fixing a preview-readiness race. The race allowed input into an old iframe during srcdoc navigation; current render/mode acknowledgement now gates visibility/editability instead of sleeps/retries/raised timeouts.

- `human-edits.png`: real inline text, fill and pointer move applied (r3).
- `structural-rebase.png`: wrapper inserted; IDs/text/styles and world layout retained (r4).
- `conflict.png`: proposed action rename refused after direct text edit (r7).
- `orphaned.png`: action deletion orphaned its original r0 pin (r8).
- `trace.json`: compact complete before/after document snapshots, feedback states, final state, accepted/rejected patch audit and applied transaction list. This is the substantive portable evidence; no screenshots are needed to inspect the retained data.
- Larger per-phase `.json` files also retain the full serialized sessions locally for diagnosis. They duplicate snapshots/history and are reproducible from the check script; they are not necessary to understand/reconstruct the compact evidence.

Test audit: six unit/storage cases retained for distinct contracts plus one UI scenario. No source-constant assertions or production test changes. Storage test owns/removes its temporary files; no retained deliverables or running Legato are touched. Browser checks would fail if real edit/patch/undo/pin/storage entrypoints were disconnected.

## Boundary and limits

- `core.ts`: pure owned scene graph, stable document/artboard/node IDs, snapshots, transactions, conservative three-way checks, global undo/redo, identity-bound pins and serialization. No DOM, Legato imports, auth, network or database.
- `ui.ts`: renderer and actual edit controls. Preview `sandbox=allow-scripts` ONLY, CSP BEFORE markup, no parent credentials/network/forms/child frames. Parent accepts only the current iframe/render token; render/mode acknowledgement prevents edits in obsolete previews. Text is escaped into preview markup; host messages/comments use textContent.
- `serve.mjs`: unauthenticated loopback JSON file host with serialized CAS writes and atomic rename. No multi-process lock, fsync/power-loss guarantee, ACL, user identity, public sharing, CSRF/Host/DNS-rebinding hardening, abuse controls or production retention. Do not deploy.
- SAME-property changes conflict, even if a patch would coincidentally choose the current value. Parent child-list changes are conservatively conflicting. Deletion checks the entire subtree so it cannot erase concurrent human edits.
- Reparent preserves live WORLD POSITION by changing local x/y. It does not preserve arbitrary CSS flex/grid/rotation/constraint semantics, which are outside this graph.
- Revisions `rN` are session-scoped monotonic IDs, NOT content hashes or globally unique fork-safe revisions. All snapshots are retained, which also reserves deleted IDs; production GC needs durable identity tombstones.
- Preview content here is a projection of constrained data, not imported arbitrary HTML/JS. An active malicious preview script could spoof messages from its own window/token; these checks are not proof of trusted user intent or a safe general write-capability bridge. Production needs a stronger trusted-editor interaction/capability boundary.
- No general HTML parser, rich text, components/logic editing, images/assets, responsive layout, cross-frame navigation, pan/zoom, real agent/Scope/MCP integration or public permissions. Full capability parity is still the target, not established by this proof.

See `REPORT.md` for conclusions, remaining unknowns and effort/risk update. Research predecessor and complete exact-file archive: LEG-664, inventory Note c701e4f1-cb17-4cbe-863b-321bf4d0f5bb. Originals remain on the local throwaway branch; findings/source/evidence are also preserved in LEG-665 Notes, with binary screenshots explicitly Legato-held rather than Burnlite attachments.
