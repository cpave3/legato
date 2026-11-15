# Chimera recovery excerpt fixture

`fixture.json` is **real Compactor output for synthetic conversation input**, not
an operator transcript or a live provider run. `generate.mjs` invokes Chimera's
built `Compactor.compact` with a deliberately unavailable summary model and an
isolated temporary home; it deletes that home afterwards. It does not launch an
agent or use a Legato database, live model, credentials or session state.

Generated from the clean Chimera checkout at
`99d44f9d3c4b75ab14132534fc0414c84402e5c5`. The built JS sourcemap's
`compactor.ts` content matched the checked-out source byte-for-byte (SHA-256
`7db3b471accfb996920916877ae9dbab9e7035d1346caa1fbbc588fe75836760`).

- `full`: unavailable summary model, complete serialized message records plus
  authoritative files/tasks blocks.
- `truncated`: the same path with a long synthetic diagnostic message;
  Chimera's actual `boundText` cuts through a JSON string and inserts
  `[Older context truncated for recovery]`, retaining complete head/tail records.

Regenerate with an already-built checkout of that Chimera revision:

```sh
node contracts/chimera-recovery/generate.mjs /path/to/chimera
```

The fixture retains complete result metadata (summary, counts, strategy and
warnings). The rendering reads the supplied summary without altering it.
