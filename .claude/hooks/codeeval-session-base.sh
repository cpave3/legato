#!/usr/bin/env bash
# Claude Code "SessionStart" hook: remember the commit a session started from,
# so the Stop hook can check what the session changed, committed or not, and
# tell the agent how to run the same check itself before it pushes.
#
# State is kept per Claude session in <git-dir>/codeeval/<session_id>/ (inside
# .git, never tracked), so another session starting in the same checkout
# leaves this one's base, block budget and background check alone. A resumed
# or compacted session keeps the base it started with. Session folders are
# never pruned: a folder's mtime is not its last use, and each holds a few
# small files.
#
# Environment (optional, as for the Stop hook): CODEEVAL_BIN, CODEEVAL_CONFIG,
# CODEEVAL_DEBT_FAIL_ON, CODEEVAL_DEBT_BUDGET.
set -u
input=$(cat)
repo=${CLAUDE_PROJECT_DIR:-$(pwd)}
cd "$repo" 2>/dev/null || exit 0
gitdir=$(git rev-parse --absolute-git-dir 2>/dev/null) || exit 0
sid=$(printf '%s' "$input" | sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_-]*\)".*/\1/p' | head -1)
dir="$gitdir/codeeval/${sid:-default}"
mkdir -p "$dir" || exit 0
file="$dir/session-base"

# The pre-push check, printed to the agent's context: the quick tier of the
# Stop hook's check against the branch's merge base, with the same settings.
q() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }
upstream=$(git symbolic-ref -q --short refs/remotes/origin/HEAD 2>/dev/null || echo origin/main)
cmd="$(q "${CODEEVAL_BIN:-codeeval}") analyze --base \"\$(git merge-base HEAD $upstream)\" --working-tree --checks quick --debt-fail-on ${CODEEVAL_DEBT_FAIL_ON:-net-debt} --debt-budget ${CODEEVAL_DEBT_BUDGET:-10}"
[ -n "${CODEEVAL_CONFIG:-}" ] && cmd="$cmd --config $(q "$CODEEVAL_CONFIG")"
echo "CodeEval checks this session's changes when you stop (experimental scoring). Run the same quick check yourself before you push or open a pull request, so a finding never arrives after review (exit 3 = findings):"
echo "  $cmd"

if printf '%s' "$input" | grep -Eq '"source"[[:space:]]*:[[:space:]]*"(resume|compact)"' && [ -s "$file" ]; then
  exit 0
fi
git rev-parse HEAD > "$file" 2>/dev/null || true
rm -f "$dir/checked-base" "$dir/broad-base" "$dir/hook-state" "$dir/hook-state.blocks"
# A background check from an earlier start of this session describes another change.
if [ -d "$dir/broad" ]; then
  pid=$(cat "$dir/broad/pid" 2>/dev/null)
  [ -n "$pid" ] && kill "$pid" 2>/dev/null
  rm -rf "$dir/broad"
fi
exit 0
