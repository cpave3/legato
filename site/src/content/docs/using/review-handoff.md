---
title: Review handoff
description: An optional packet of behaviour, decisions and evidence that an agent writes for a review.
sidebar:
  order: 65
---

A review handoff is a briefing an agent writes about a change it made, to sit beside its [review tour](/legato/using/reviews/). The tour walks you through the diff. The review handoff tells you what the diff does not: how the behaviour changed and why, which decisions the agent made and what it rejected, what it checked and what it did not, where the risk is, and how to roll the change back. Every statement points at pinned lines of source, so you can check it.

Use one for a change you will have to explain to someone else, or maintain later. You read it in the web UI, challenge what you doubt, keep your own notes on it, and export it as Markdown for a pull request or for maintenance notes.

A review handoff is optional. It does not change whether the review is ready or complete, and it is not an approval on GitHub. The agent writes it; nothing in it is verified by Legato beyond the source it points at.

## Reading a review handoff

Open the tour in the web UI (`/review`) and switch from **Diff review** to **Handoff**. Use **Diff review** to inspect the change yourself, and **Handoff** for its behaviour, decisions, evidence and maintenance context.

- **Overview.** Before and after, why, what the change does not try to do, the main decision and the alternative that was rejected, the highest risk, verification gaps, a question for the reviewer, and how to roll back. A field the agent did not know says so.
- **Concepts.** Usually three to seven cards, in order of behaviour and risk. Each makes one claim and gives its flow, the invariant it keeps, how it fails, where to start when you change it, and links to source lines, checks and chapters.
- **Reading route.** **Author / owner**, **GitHub reviewer**, **Maintainer** or **Colleague** reorders the cards for that reader. Routes never hide risks, checks or the coverage of the diff.
- **Evidence.** Each check the agent reports, with its command, the commit it ran on, its outcome (`passed`, `failed` or `skipped`) and its limits. **No evidence attached** means the agent reported none. A reported check is the agent's report, not a run that Legato watched.
- **Coverage.** Every file of the diff, as `behavior`, `tests`, `mechanical` or `uncategorized`. A file the agent did not classify is `uncategorized`, never left out.
- **Revision history.** Earlier packets, with what changed between them. A packet older than the code is marked outdated.
- **Challenge this claim.** Ask about a concept, or flag a change, through the tour's usual questions and findings.
- **Optional practice.** Scenarios the agent wrote: a maintenance question with a hidden explanation. They are not graded.
- **Your note.** Record your own readiness (**Ready to maintain**, **Need help** or **Deferred**), scenario answers and notes with **Save my note**. A note is a self-assessment kept on this Legato. It does not complete or approve the review.
- **Export handoff brief.** Choose **GitHub review brief** or **Maintenance brief**, preview it, then copy or download the Markdown. Nothing is posted to GitHub.

The rest of this page is for the agent that writes the packet, and for you when you want to know what it must contain.

## Writing a review handoff

1. Finish tests and make clean, meaningful commits. Workers do not commit or submit packets; the conductor owns this step. Do not change Git history to repair review metadata.
2. Author chapters and source-linked narration, then run `legato review ready "Summary"`.
3. Inspect `legato review show --json` and `legato review handoff show --json`. Select the current ready pass and exact base/head SHAs. Do not guess a pass ID or use an abbreviated SHA.
4. Run `legato review handoff template > /tmp/handoff.json` for the complete editable schema with the current boundary. Replace all instructional text, paths, and the check timestamp before submission. The nested examples expose the full contract. Remove the example check and its concept reference when no result exists; do not invent a result. Optional scenarios can also be removed. The template works in any task repository; it does not require this documentation in the checkout. Normally use three to seven concepts, ordered by behavior and risk. Include the critical failure path, invariant, rejected alternative, and known verification gaps.
5. Submit with `legato review handoff submit --file packet.json`. For stdin, use `--file -`. Input is one strict JSON object, at most 256 KiB; unknown fields and trailing documents fail. Agent input cannot contain captured source text or human notes.
6. Inspect the stored packet, select its exact ID, and preview an export before sharing it.

All `legato review handoff` commands accept `--task <id>` and `--name <name>`, with `LEGATO_TASK_ID` and `LEGATO_REVIEW_NAME` as fallbacks. A command for a tour that does not exist fails; inspecting a review handoff never creates a tour. Show and export read persisted data without scanning Git.

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

Changes to HEAD, pass, or chapter metadata/membership conservatively invalidate prior context. Run `legato review sync` to observe repository changes; viewing, `show` and `export` do not check Git. `checked_at` means the last successful observation, not proof that your repository is still unchanged. After further work, commit, ready the current review, inspect exact pass/base/head, and submit a new packet with the latest expected revision. Use `review restart` for a replacement pass where appropriate; old snapshots remain readable even when old chapter links are unavailable. Deleting the review tour deletes its review handoff history.

New packets do not inherit human notes. Comparisons describe known snapshots, changed concepts/files, and evidence to recheck; when comparison is unsafe, do a full reread. A completed review is not itself evidence that a packet is current.

## Exports and your notes

Use the tour's question and finding flow to challenge a claim, and include the packet and concept IDs; links to chapters of an earlier pass may no longer work.

Scenario answers, skips and readiness (`ready`, `need_help`, `deferred` or empty) belong to a human note kept on your Legato. They are not a quiz grade, a verified identity, a review approval or a merge gate. The authoring CLI cannot write them.

```bash
legato review handoff export --packet <exact-id> --format github
legato review handoff export --packet <exact-id> --format maintenance \
  --concept critical-path --concept rollback --include-sources
legato review handoff export --packet <exact-id> --format maintenance --include-notes --json
```

- With no `--concept`, the export includes every concept.
- Checks linked to no concept are evidence for the whole packet, and every export includes them, failures too. Linked checks and scenarios follow the concepts you select.
- Source excerpts and personal notes are left out unless you ask for them (`--include-sources`, `--include-notes`).
- The export writes Markdown to standard output; `--json` returns the Markdown, a file name and any warnings.

Preview the brief before you copy it into a pull request description or comment, or save it as maintenance notes. Export never posts, approves, commits or changes anything on GitHub; GitHub remains the place for discussion and verdicts. An export is an AI-authored snapshot of exact base and head commits, with a warning that it is not checked against the current pull request. Check the pull request's head and base yourself: links to pinned source do not prove that they match. A brief does not update after a push.

:::caution[Privacy]
Never submit credentials, secrets, raw logs, unnecessary personal data or private absolute paths. Do not select sensitive source excerpts. Preview every export, especially one with notes or sources. Legato does not detect secrets for you. Your notes stay on your Legato unless you export them, but they are not protected per person: anyone with the web UI's token can read and change them.
:::

Internals (the HTTP API, storage and freshness) are in [docs/ai/review-handoff.md](https://github.com/cpave3/legato/blob/main/docs/ai/review-handoff.md) on GitHub.
