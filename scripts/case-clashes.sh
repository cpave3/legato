#!/usr/bin/env bash
# Fails when two paths in the repository differ only in case. On a file
# system that ignores case (macOS's, Windows') they are one path, so a build
# there breaks where Linux's passes (LEG-149). It compares:
#   - every tracked file and every folder above one;
#   - every JavaScript or TypeScript module without its extension, and every
#     folder, as an import names them: tsc resolves ./Approvals by trying
#     Approvals.ts first, which macOS finds as approvals.ts beside
#     Approvals.tsx, and ./Foo, meant for Foo/index.ts, as foo.ts.
# Reads `git ls-files`, or the paths on its input when given `-`.
# Run by .github/workflows/ci.yml; tested by cmd/legato/case_clashes_test.go.
set -euo pipefail

case "${1:-}" in
  "") paths() { git ls-files; } ;;
  -) paths() { cat; } ;;
  *)
    echo "usage: case-clashes.sh [-]" >&2
    exit 2
    ;;
esac

paths | awk '
# A module group holds folders too, but only one with a module in it is a
# clash of its own: one of folders alone is already a path clash.
function note(kind, name, module,    key) {
  key = kind " " tolower(name)
  if (module) modules[key] = 1
  if ((key SUBSEP name) in seen) return
  seen[key SUBSEP name] = 1
  if (!(key in names)) { order[++groups] = key; kinds[key] = kind }
  names[key] = names[key] "  " name
  count[key]++
}
{
  path = $0
  note("path", path, 0)
  dir = path
  while (sub(/\/[^\/]*$/, "", dir)) { note("path", dir, 0); note("module", dir, 0) }
  if (match(path, /\.(ts|tsx|mts|cts|js|jsx|mjs|cjs)$/)) note("module", substr(path, 1, RSTART - 1), 1)
}
END {
  clashes = 0
  for (i = 1; i <= groups; i++) {
    key = order[i]
    if (count[key] < 2 || (kinds[key] == "module" && !(key in modules))) continue
    if (!clashes) print "These paths differ only in case, so macOS and Windows see one path where Linux sees two. Rename one:"
    clashes++
    if (kinds[key] == "module") print "  module (as an import names it):" names[key]
    else print "  path:" names[key]
  }
  exit clashes > 0
}'
