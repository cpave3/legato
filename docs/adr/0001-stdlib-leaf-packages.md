# 1. Standard-library leaf packages under `internal/leaf`

Status: accepted, 2026-11-27 (Cameron)

## Context

Engine packages may not import sibling engine packages
(`TestEnginePackagesDoNotImportSiblingEnginePackages`). That keeps each one
self-contained, but it left no home for a helper two engine packages need
with exactly the same meaning. CodeEval found two such copies in the
subscription-usage readers: `retryAfter` (claudeusage, opencodeusage) and
`openRegular` (claudeusage, codexusage), byte for byte the same.

## Decision

A small helper with one meaning that knows nothing of Legato may live in
its own package under `internal/leaf/`:

- A leaf package imports **only the standard library**: no third-party
  module and no Legato package, another leaf included.
  `TestLeafPackagesImportOnlyTheStandardLibrary` (in
  `internal/engine/store/architecture_test.go`) enforces it.
- Any package may import a leaf package. Of Legato's own packages, an
  engine package may import **only** leaf packages; the sibling-engine
  rule is unchanged, and the same test file enforces both.
- One package per helper, named for what it does (`retryafter`,
  `regularfile`), never `util`, `common` or `helpers`.
- Code belongs in a leaf only when its callers need the **same**
  semantics. Look-alikes with different behaviour stay apart: Slack's,
  Discord's and Jira's Retry-After readers (whole seconds only, floats, a
  zero allowed) are not `retryafter.Parse`, and the named-account reader's
  `O_NOFOLLOW` open is not `regularfile.Open`.

`config/` stays standalone. This decision does not let `config` import a
leaf package; that would be a separate decision.

## Consequences

- The first two leaves are `internal/leaf/retryafter` and
  `internal/leaf/regularfile`.
- A helper that needs anything beyond the standard library, or that would
  carry provider-specific policy, does not qualify; it stays where it is,
  duplicated if need be.
