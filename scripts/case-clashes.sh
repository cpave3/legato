#!/usr/bin/env bash
# Fails when two paths in the repository differ only in case. On a file
# system that ignores case (macOS's, Windows') they are one path, so a build
# there breaks where Linux's passes (LEG-149). It compares:
#   - every tracked file and every folder above one;
#   - every JavaScript or TypeScript module without its extension, as an
#     import names it: tsc resolves ./Approvals by trying Approvals.ts first,
#     which macOS finds as approvals.ts beside Approvals.tsx.
# Reads `git ls-files`, or the paths on its input when given `-`.
# Run by .github/workflows/ci.yml; tested by case-clashes.test.sh.
set -euo pipefail

paths() {
  if [ "${1:-}" = "-" ]; then cat; else git ls-files; fi
}

paths "${1:-}" | awk '
function note(kind, name,    key) {
  key = kind " " tolower(name)
  if ((key SUBSEP name) in seen) return
  seen[key SUBSEP name] = 1
  if (!(key in names)) { order[++groups] = key; kinds[key] = kind }
  names[key] = names[key] "  " name
  count[key]++
}
{
  path = $0
  note("path", path)
  dir = path
  while (sub(/\/[^\/]*$/, "", dir)) note("path", dir)
  if (match(path, /\.(ts|tsx|mts|cts|js|jsx|mjs|cjs)$/)) note("module", substr(path, 1, RSTART - 1))
}
END {
  clashes = 0
  for (i = 1; i <= groups; i++) {
    key = order[i]
    if (count[key] < 2) continue
    if (!clashes) print "These paths differ only in case, so macOS and Windows see one path where Linux sees two. Rename one:"
    clashes++
    if (kinds[key] == "module") print "  module (as an import names it):" names[key]
    else print "  path:" names[key]
  }
  exit clashes > 0
}'
