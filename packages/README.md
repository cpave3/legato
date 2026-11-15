# packages

Legato's apps outside the Go binary are moving here, one folder each. They
share one pnpm workspace (`pnpm-workspace.yaml`, rooted in this folder), one
lockfile, one lint configuration (`eslint.config.js`) and one base TypeScript
configuration (`tsconfig.base.json`) that each package's `tsconfig.json`
extends.

| Package | What it is |
| --- | --- |
| [`even-hub/`](even-hub/README.md) | An Even Realities G2 glasses app, built with the Even Hub SDK, that shows the agents working now |

From this folder:

```bash
pnpm install          # every package
pnpm run check        # lint, typecheck and test them all
```

From the repository root, `task packages:check` does the same. CI runs it on
every pull request (`.github/workflows/ci.yml`, job `packages`).

The workspace stops at this folder on purpose: `web/`, `site/`,
`browser-extension/` and `desktop/` keep their own lockfiles until each moves
in, and a workspace at the repository root would take over `pnpm install` in
them.
