# Review handoff

Handoff is optional, AI-authored context for a normal review tour. It does not change `review ready`, review completion, or GitHub approval. Use **Diff review** for independent inspection and **Handoff** for behavior, decisions, evidence, and maintenance context.

## Author workflow

1. Finish tests and make clean, meaningful commits. Workers do not commit or submit packets; the conductor owns this step. Do not change Git history to repair review metadata.
2. Author chapters and source-linked narration, then run `legato review ready "Summary"`.
3. Inspect `legato review show --json` and `legato review handoff show --json`. Select the current ready pass and exact base/head SHAs. Do not guess a pass ID or use an abbreviated SHA.
4. Run `legato review handoff template > /tmp/handoff.json` for the complete editable schema with the current boundary. Replace all instructional text, paths, and the check timestamp before submission. The nested examples expose the full contract. Remove the example check and its concept reference when no result exists; do not invent a result. Optional scenarios can also be removed. The template works in any task repository; it does not require this documentation in the checkout. Normally use three to seven concepts, ordered by behavior and risk. Include the critical failure path, invariant, rejected alternative, and known verification gaps.
5. Submit with `legato review handoff submit --file packet.json`. For stdin, use `--file -`. Input is one strict JSON object, at most 256 KiB; unknown fields and trailing documents fail. Agent input cannot contain captured source text or human notes.
6. Inspect the stored packet, select its exact ID, and preview an export before sharing it.

All handoff commands accept `--task <id>` and `--name <name>`, with `LEGATO_TASK_ID` and `LEGATO_REVIEW_NAME` as fallbacks. Missing tours return an error; handoff inspection does not create a tour. Show and export read persisted data without scanning Git.

## Complete runnable packet example

Run this from an existing task repository with a **clean, ready review**, using Bash, Python 3, and an installed Legato CLI with handoff support. Set `LEGATO_TASK_ID` (and `LEGATO_REVIEW_NAME` for a named tour), or use explicit selectors on every Legato command. The example chooses a real tracked source file and the current pass. It reports **no test result**, rather than inventing a passing check. Replace its generic explanation with the actual change's behavior before sharing.

Temporary packet files go outside the repository so they do not make it dirty.

```bash
set -euo pipefail
: "${LEGATO_TASK_ID:?Set the existing task ID first}"
work=$(mktemp -d)
legato review show --json > "$work/review.json"
legato review handoff show --json > "$work/handoff.json"
python3 - "$work" <<'PY'
import json, pathlib, subprocess, sys
work = pathlib.Path(sys.argv[1])
review = json.loads((work / 'review.json').read_text())
handoff = json.loads((work / 'handoff.json').read_text())
tour = review['tour']
assert tour['status'] == 'ready', 'Run review ready after committing and testing'
passes = [p['pass'] for p in review['passes'] if p['pass']['status'] == 'ready']
current = max(passes, key=lambda p: p['number'])
head, base = tour['head_sha'], tour['base_sha']
assert current['head_sha'] == head
assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip() == head
assert not subprocess.check_output(['git', 'status', '--porcelain']), 'Commit your work first'
# Choose a normal text blob, never a symlink or submodule.
source_path = None
for record in subprocess.check_output(['git', 'ls-tree', '-rz', head]).split(b'\0'):
    if not record:
        continue
    meta, path = record.split(b'\t', 1)
    if meta.split()[0] not in (b'100644', b'100755'):
        continue
    path = path.decode('utf-8')
    content = subprocess.check_output(['git', 'show', head + ':' + path])
    try:
        text = content.decode('utf-8')
    except UnicodeDecodeError:
        continue
    if text and '\0' not in text and len(content) < 64000 and len(text.splitlines()[0].encode()) < 1000:
        source_path = path
        break
assert source_path, 'Choose a valid text source in this revision'
packet = {
    "schema_version": 1,
    "expected_revision": max((p['revision'] for p in handoff['packets']), default=0),
    "pass_id": current['id'],
    "base_sha": base,
    "head_sha": head,
    "overview": {
        "before": "The review diff had no portable behavior map.",
        "after": "This packet supplies an initial source-linked reading route.",
        "why": "Help the owner start an independent inspection.",
        "non_goals": "This example does not certify the implementation.",
        "decision": "Pin context to the exact ready revision.",
        "alternative": "Unpinned prose was rejected because it can drift.",
        "risk": "This generic example is incomplete until the author explains the actual change.",
        "verification_gaps": "No check result is attached. Inspect the full diff and run appropriate tests.",
        "review_question": "Which behavior and failure boundary need closer inspection?",
        "rollback": "Rollback has not been established; inspect deployment and data constraints first."
    },
    "concepts": [{
        "id": "reading-route",
        "title": "Start with pinned source",
        "claim": "The reference identifies source at this review revision, not proof of correct behavior.",
        "flow": ["Open the pinned source", "Trace its callers", "Inspect the full diff and relevant tests"],
        "invariant": "Context must identify the exact reviewed revision.",
        "failure_case": "A later push can make a static brief outdated.",
        "change_guide": "Replace this example with the actual entry point and observable outcome.",
        "source_ids": ["entry"],
        "evidence_ids": [],
        "chapter_ids": [],
        "routes": ["owner", "reviewer", "maintainer", "colleague"]
    }],
    "sources": [{
        "id": "entry", "path": source_path, "side": "head", "line_start": 1, "line_end": 1
    }],
    "evidence": [],
    "scenarios": [{
        "id": "next-push",
        "concept_id": "reading-route",
        "prompt": "What should you check after a new commit before sharing this brief?",
        "explanation": "Sync the review, inspect the new exact revision, rerun relevant checks, and author a replacement packet. Source existence alone is not a passing check.",
        "source_ids": ["entry"]
    }],
    "coverage": [],
    "changes": "Initial example; all omitted diff paths remain explicitly uncategorized."
}
(work / 'packet.json').write_text(json.dumps(packet, indent=2) + '\n')
print(work / 'packet.json')
PY
cat "$work/packet.json" # Inspect and edit this complete JSON before submission.
legato review handoff submit --file "$work/packet.json"
legato review handoff show --json > "$work/handoff.json"
packet=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["packets"][0]["id"])' "$work/handoff.json")
legato review handoff export --packet "$packet" --format github \
  --concept reading-route > "$work/github-brief.md"
cat "$work/github-brief.md" # Review all content and warnings before manual sharing.
```

The generated file has this complete JSON shape (the workflow above replaces the revision and source placeholders with real values):

```json
{
  "schema_version": 1,
  "expected_revision": 0,
  "pass_id": "REPLACE_WITH_READY_PASS_ID",
  "base_sha": "REPLACE_WITH_FULL_BASE_SHA",
  "head_sha": "REPLACE_WITH_FULL_HEAD_SHA",
  "overview": {
    "before": "The review diff had no portable behavior map.",
    "after": "This packet supplies an initial source-linked reading route.",
    "why": "Help the owner start an independent inspection.",
    "non_goals": "This example does not certify the implementation.",
    "decision": "Pin context to the exact ready revision.",
    "alternative": "Unpinned prose was rejected because it can drift.",
    "risk": "This generic example is incomplete until the author explains the actual change.",
    "verification_gaps": "No check result is attached. Inspect the full diff and run appropriate tests.",
    "review_question": "Which behavior and failure boundary need closer inspection?",
    "rollback": "Rollback has not been established; inspect deployment and data constraints first."
  },
  "concepts": [
    {
      "id": "reading-route",
      "title": "Start with pinned source",
      "claim": "The reference identifies source at this review revision, not proof of correct behavior.",
      "flow": [
        "Open the pinned source",
        "Trace its callers",
        "Inspect the full diff and relevant tests"
      ],
      "invariant": "Context must identify the exact reviewed revision.",
      "failure_case": "A later push can make a static brief outdated.",
      "change_guide": "Replace this example with the actual entry point and observable outcome.",
      "source_ids": [
        "entry"
      ],
      "evidence_ids": [],
      "chapter_ids": [],
      "routes": [
        "owner",
        "reviewer",
        "maintainer",
        "colleague"
      ]
    }
  ],
  "sources": [
    {
      "id": "entry",
      "path": "path/to/real-source.go",
      "side": "head",
      "line_start": 1,
      "line_end": 1
    }
  ],
  "evidence": [],
  "scenarios": [
    {
      "id": "next-push",
      "concept_id": "reading-route",
      "prompt": "What should you check after a new commit before sharing this brief?",
      "explanation": "Sync the review, inspect the new exact revision, rerun relevant checks, and author a replacement packet. Source existence alone is not a passing check.",
      "source_ids": [
        "entry"
      ]
    }
  ],
  "coverage": [],
  "changes": "Initial example; all omitted diff paths remain explicitly uncategorized."
}
```

The template command and the Python `packet` object use the full JSON authoring schema; `json.dumps` writes a ready-to-decode JSON file with actual revision and source values. IDs must be unique within their collections. Cross-references must resolve: concept `source_ids`/`evidence_ids`, scenario `concept_id`/`source_ids`, and any `chapter_ids` must identify entries in the selected review. Do not invent chapter IDs. Coverage categories are `behavior`, `tests`, `mechanical`, or `uncategorized`; paths must belong to the base-to-head diff. Omitted changed files are automatically added as uncategorized. Sources use repository-relative paths, `side` `base` or `head`, and inclusive 1-based lines. Unchanged and base-side sources are allowed. Legato reads pinned Git blobs, bounds the excerpts, and rejects unsafe paths, symlinks, and submodules; it does not fetch URLs or arbitrary host files.

All overview fields and each concept's title, claim, flow, invariant, failure case, and change guide are required. State unknowns explicitly. IDs use 1–64 letters, digits, dots, underscores, or hyphens. Check outcomes are `passed`, `failed`, or `skipped`; check timestamps must be RFC3339 and tested revisions must be full SHAs. Scenarios require a prompt, explanation, and pinned source references.

Source blobs must be UTF-8 text, at most 4 MiB. Each selected excerpt is at most 64 KiB; all excerpts together are at most 256 KiB. Up to 32 concepts, 128 sources, 128 check records, and 32 scenarios are allowed.

For real check records, add entries to `evidence` with all these fields:

```json
{
  "id": "failure-path-check",
  "description": "Failure-path test invocation",
  "source_ids": ["entry"],
  "command": "go test ./internal/example -run TestFailurePath",
  "head_sha": "REPLACE_WITH_THE_FULL_SHA_ACTUALLY_TESTED",
  "recorded_at": "2026-09-07T12:00:00Z",
  "outcome": "passed",
  "reporter": "agent",
  "limitations": "Only this test was run; this is an imported report, not independent verification."
}
```

This second snippet is a template, not a claim that this test exists or ran. Replace every value with observed facts, add the ID to the concept's `evidence_ids`, and reference actual test sources. Report failures, skipped checks, environment limits, and different tested revisions honestly. Commands are stored as text, never executed by ingestion. A digest proves content identity, not truth; source existence does not prove a passing check. Empty evidence means **No evidence attached**.

## Revisions and updates

`expected_revision` is the latest packet revision, or `0` for the first packet. A successful submit returns a new immutable packet ID and revision. Conflicting writes require a fresh read and deliberate re-authoring, not a blind retry.

Changes to HEAD, pass, or chapter metadata/membership conservatively invalidate prior context. Run `legato review sync` to observe repository changes; ordinary GET/show/export do not continuously check Git. `checked_at` means the last successful observation, not proof that your repository is still unchanged. After further work, commit, ready the current review, inspect exact pass/base/head, and submit a new packet with the latest expected revision. Use `review restart` for a replacement pass where appropriate; old snapshots remain readable even when old chapter links are unavailable. Deleting the parent review deletes its handoff history.

New packets do not inherit human notes. Comparisons describe known snapshots, changed concepts/files, and evidence to recheck; when comparison is unsafe, do a full reread. A completed review is not itself evidence that a packet is current.

## Human notes, challenge, and exports

The web Handoff view offers owner, reviewer, maintainer, and colleague reading routes over the same content. Routes do not hide risks or uncategorized diff files. Use the existing question/finding flow to challenge a claim. Include packet and concept IDs; historical chapter links may no longer be usable.

Optional scenario answers, skips, and readiness (`ready`, `need_help`, `deferred`, or empty) belong to a local-instance human note. They are not a quiz grade, verified identity, review approval, or merge gate. The authoring CLI has no human-note write command.

```bash
legato review handoff export --packet <exact-id> --format github
legato review handoff export --packet <exact-id> --format maintenance \
  --concept critical-path --concept rollback --include-sources
legato review handoff export --packet <exact-id> --format maintenance --include-notes --json
```

No concept selection means all concepts. Unlinked check records are packet-level evidence and remain in every export, including failures. Linked evidence and scenarios follow the selected concepts. Source excerpts and personal notes are excluded unless explicitly selected. Text export writes Markdown to stdout; `--json` returns Markdown, filename, and warnings. Template returns the raw editable input JSON; show returns raw view JSON; submit returns the standard CLI success envelope containing the packet. Validation/conflict errors are structured CLI usage errors; missing tours/packets are not-found errors.

Preview the brief before copying it into a GitHub PR description or context comment, or saving it as maintenance documentation. Export never posts, approves, commits, or changes GitHub. GitHub remains authoritative for discussion and verdicts. Exports are AI-authored snapshots, with exact base/head and an unverified-current-PR warning. Verify the current PR head/base yourself; do not imply that pinned blob links verify PR alignment. Static briefs do not update after a push.

**Privacy:** Never submit credentials, secrets, raw logs, unnecessary personal data, or private absolute paths. Do not select sensitive source excerpts. Preview every export, especially when including notes or sources. Automatic secret detection is not guaranteed. Local notes are private to the instance unless exported, not protected by per-person authentication; the shared API token is not a team-attestation system.

## HTTP API

Existing bearer-token authentication applies. Packet authoring is CLI-only; there is no HTTP ingestion route.

- `GET /api/review/tours/{tour_id}/handoff` returns `{tour_id, checked_at, packets}` (newest first).
- `POST /api/review/tours/{tour_id}/handoff/{packet_id}/note` replaces the human note. Example: `{"revision":0,"readiness":"need_help","body":"Check retry ordering","answers":{},"skipped":["next-push"]}`. Supply the previous note revision; success returns the incremented revision. Answers and skips cannot overlap.
- `POST /api/review/tours/{tour_id}/handoff/export` returns `{markdown, filename, warnings}` for preview/download. Example: `{"packet_id":"EXACT_PACKET_ID","format":"github","concept_ids":["reading-route"],"include_sources":false,"include_notes":false}`.

POST bodies are strict, bounded JSON objects. Errors are 400 for invalid input, 404 for missing records, 409 for stale writes, and 500 for internal failures. An instance without the optional handoff capability returns 503. Successful packet submission and note saves emit the existing `review_changed` event.
