# 2. `config` owns the macro model

Status: accepted, 2026-11-27 (Cameron)

## Context

CLAUDE.md says `config/` stands alone and imports no Legato package. It
imported `internal/engine/macros`, the only exception, for the `Macro`
type the `macros:` list decodes into. A CodeEval audit (task i95qqzwb)
found that no engine package imports `engine/macros`. Its importers were
`config`, `internal/server`, `internal/service` and `internal/tui`, all of
which already import `config`. Two of the TUI's engine imports were
also this package.

## Decision

The macro model moves into `config` (`config/macros.go`): `Macro`, its
`keys`/`message` decoding for YAML and JSON, `Validate`,
`MacroAppliesToTask`/`MacroAppliesToPR`, `AnyFavoriteMacro` and
`SplitFavoriteMacros`. `GET /api/macros`'s envelope becomes the server's
own `macroListResult`. `internal/engine/macros` is deleted, with no alias
or compatibility package left behind.

The YAML and JSON field names, validation and its errors, defaults and
the order favourites are listed in stay as they were.

`config/architecture_test.go` checks that `config` imports no Legato
package. `internal/engine/store/architecture_test.go` already keeps
engine packages from importing `config`, so no engine → config arrow can
appear.

## Consequences

- `config` is standalone again, as CLAUDE.md says.
- The TUI's imports of `engine/macros` (in `app.go` and
  `overlay/macro_picker.go`) become `config` imports. A layer may import
  `config`.
- Rejected: an exception that lets `config` import `engine/macros` would
  have kept a hole in the rule. Making `macros` a leaf would not fit ADR 1:
  the macro model is Legato's own domain type, and leaves are helpers that
  know nothing of Legato.
