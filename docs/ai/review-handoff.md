## Review handoff: internals

The user guide, with the author workflow, the complete runnable packet example, the schema and its limits, is [Review handoff](../../site/src/content/docs/using/review-handoff.md) (on the site at `/legato/using/review-handoff/`). This file keeps the HTTP API and the server behaviour. The CLI verbs are summarised in [CLI](cli.md) § Review handoff; the tour model in [Reviews](reviews.md).

"Review handoff" is the packet; a bare "handoff" is a task sent to a worker instance (see `GLOSSARY.md`).

### Storage and freshness

- A submitted packet is immutable: each submit returns a new packet ID and revision. `expected_revision` is checked against the latest revision (`0` for the first), and a stale one is a conflict.
- Packets belong to their review tour; deleting the tour deletes its packet history.
- `ReviewService.Sync` observes repository changes for a tour with packets even after it is `reviewed` (`observeHandoff`), and sets `checked_at`. Changes to `HEAD`, the pass, or chapter metadata or membership mark earlier packets outdated. Ordinary `GET`, `show` and `export` read persisted data and do not scan Git.
- Human notes (readiness, scenario answers and skips) are stored per packet on this instance, with their own revision. The authoring CLI has no write command for them; only the web API writes them. They are not protected by per-person authentication: the shared API token is not a team-attestation system.

### HTTP API

Existing bearer-token authentication applies. Packet authoring is CLI-only; there is no HTTP ingestion route.

- `GET /api/review/tours/{tour_id}/handoff` returns `{tour_id, checked_at, packets}`, newest first.
- `POST /api/review/tours/{tour_id}/handoff/{packet_id}/note` replaces the human note. Example: `{"revision":0,"readiness":"need_help","body":"Check retry ordering","answers":{},"skipped":["next-push"]}`. Supply the previous note revision; success returns the incremented revision. Answers and skips cannot overlap. Readiness is `ready`, `need_help`, `deferred` or empty.
- `POST /api/review/tours/{tour_id}/handoff/export` returns `{markdown, filename, warnings}` for preview or download. Example: `{"packet_id":"EXACT_PACKET_ID","format":"github","concept_ids":["reading-route"],"include_sources":false,"include_notes":false}`.

POST bodies are strict, bounded JSON objects. Errors are `400` for invalid input, `404` for missing records, `409` for stale writes, and `500` for internal failures. An instance without the optional handoff capability returns `503`. A successful packet submission and a note save emit the existing `review_changed` event.

### CLI output shapes

`legato review handoff template` returns the raw editable input JSON; `show` returns the raw view JSON; `submit` returns the standard CLI success envelope (`{ok, command, data}`) with the packet in `data`. Validation and conflict errors are structured CLI usage errors; missing tours and packets are not-found errors. `export` writes Markdown to stdout; with `--json` it returns the Markdown, the filename and the warnings.
