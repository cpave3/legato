# Releases and updates

A Legato built from a GitHub release knows its version. A Legato built from source says it is `dev`: that is how a developer's checkout and a worker that runs releases tell themselves apart.

## The version

`internal/engine/version` holds the version a release build stamps in (`-X github.com/cpave3/legato/internal/engine/version.stamped=vX.Y.Z`); without it the version is `dev`. `version.IsRelease` is true for a stable `vX.Y.Z` only, `version.Newer` compares two. The version is in `legato version` (`--json`: `{version, release}`; it reads no configuration), `GET /health`, `GET /api/meta`, the MCP server's handshake and the 1Password integration's name.

## Making a release

`.github/workflows/release.yml`, run by hand on `main`:

```bash
gh workflow run release -f bump=patch    # or minor, major
```

1. `scripts/next-version.sh <bump>`: the newest stable tag (`vX.Y.Z`, compared as numbers; prereleases and other tags left out), bumped. The first release is `v0.0.1` (patch) or `v0.1.0` (minor).
2. `go vet ./...` and `go test -short ./...`.
3. `task release:dist VERSION=vX.Y.Z`: the web UI once, then `task build:release` for `linux/amd64`, `linux/arm64` and `darwin/arm64` into `dist/legato-<os>-<arch>` (`CGO_ENABLED=0`, `-trimpath -s -w`), and `dist/SHA256SUMS`. No Mac is needed: Legato builds without C, and Go's linker signs a `darwin/arm64` binary ad hoc, which Apple Silicon requires. The binaries are not notarized.
4. The Linux amd64 binary must say it is the version it was built as.
5. Build provenance (`actions/attest-build-provenance`) for each binary, then `gh release create vX.Y.Z` with the binaries and `SHA256SUMS`, its notes generated from the commits. The tag is the version: nothing is committed back to `main`.

The asset names have no version in them, so `https://github.com/cpave3/legato/releases/latest/download/legato-linux-amd64` is always the newest.
