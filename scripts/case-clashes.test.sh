#!/usr/bin/env bash
# Tests scripts/case-clashes.sh with lists of paths given on its input.
# Run by .github/workflows/ci.yml before the check itself.
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
failed=0

# expect <name> <exit status> <text the output must hold, or ""> <paths...>
expect() {
  local name="$1" want="$2" holds="$3"
  shift 3
  local out status
  out="$(printf '%s\n' "$@" | bash "$here/case-clashes.sh" - 2>&1)"
  status=$?
  if [ "$status" -ne "$want" ]; then
    echo "FAIL $name: exit $status, want $want"; echo "$out" | sed 's/^/    /'; failed=1; return
  fi
  if [ -n "$holds" ] && ! grep -qF -- "$holds" <<<"$out"; then
    echo "FAIL $name: output lacks \"$holds\""; echo "$out" | sed 's/^/    /'; failed=1; return
  fi
  if [ "$want" -eq 0 ] && [ -n "$out" ]; then
    echo "FAIL $name: printed on success"; echo "$out" | sed 's/^/    /'; failed=1; return
  fi
  echo "ok   $name"
}

expect "distinct paths pass" 0 "" \
  go.mod internal/a.go internal/b.go web/src/App.tsx

expect "two files that differ only in case fail" 1 "docs/README.md  docs/readme.md" \
  docs/README.md docs/readme.md

expect "two folders that differ only in case fail" 1 "Docs  docs" \
  Docs/a.md docs/b.md

# LEG-149: on macOS the import ./Approvals found approvals.ts.
expect "modules an import names alike fail" 1 "src/voice/Approvals  src/voice/approvals" \
  src/voice/Approvals.tsx src/voice/approvals.ts src/voice/approvals.test.tsx

expect "files that are not modules may share a name in another case" 0 "" \
  src/voice/Widget.tsx src/voice/widget.css src/voice/widget.test.tsx

expect "one module in two extensions passes" 0 "" \
  src/a.ts src/a.tsx

exit "$failed"
