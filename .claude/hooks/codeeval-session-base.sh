#!/usr/bin/env bash
# Claude Code "SessionStart" hook: remember the commit a session started from,
# so the Stop hook can check everything the session changed, committed or not.
# Writes <git-dir>/codeeval-session-base (inside .git, never tracked). A
# resumed or compacted session keeps the base it started with.
set -u
input=$(cat)
repo=${CLAUDE_PROJECT_DIR:-$(pwd)}
cd "$repo" 2>/dev/null || exit 0
gitdir=$(git rev-parse --git-dir 2>/dev/null) || exit 0
file="$gitdir/codeeval-session-base"
if printf '%s' "$input" | grep -Eq '"source"[[:space:]]*:[[:space:]]*"(resume|compact)"' && [ -s "$file" ]; then
  exit 0
fi
git rev-parse HEAD > "$file" 2>/dev/null || true
rm -f "$gitdir/codeeval-hook-state" "$gitdir/codeeval-hook-state.blocks"
# A background check from an earlier session describes another change.
if [ -d "$gitdir/codeeval-broad" ]; then
  pid=$(cat "$gitdir/codeeval-broad/pid" 2>/dev/null)
  [ -n "$pid" ] && kill "$pid" 2>/dev/null
  rm -rf "$gitdir/codeeval-broad"
fi
exit 0
