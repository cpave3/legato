#!/usr/bin/env bash
# PATCHED from CodeEval 0.8.5: per-session state, see CE-3; remove once
# upstream ships it. State lives in <git-dir>/codeeval/<session_id>/, so a
# session starting in the same checkout never moves another's base, budget
# or background check. Re-running `codeeval hook install` overwrites this.
# Claude Code "SessionStart" hook: remember the commit a session started from,
# so the Stop hook can check everything the session changed, committed or not.
# Writes <git-dir>/codeeval/<session_id>/session-base (inside .git, never tracked). A
# resumed or compacted session keeps the base it started with.
set -u
input=$(cat)
repo=${CLAUDE_PROJECT_DIR:-$(pwd)}
cd "$repo" 2>/dev/null || exit 0
gitdir=$(git rev-parse --git-dir 2>/dev/null) || exit 0
sid=$(printf '%s' "$input" | sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_-]*\)".*/\1/p' | head -1)
dir="$gitdir/codeeval/${sid:-default}"
mkdir -p "$dir" || exit 0
file="$dir/session-base"
if printf '%s' "$input" | grep -Eq '"source"[[:space:]]*:[[:space:]]*"(resume|compact)"' && [ -s "$file" ]; then
  exit 0
fi
git rev-parse HEAD > "$file" 2>/dev/null || true
rm -f "$dir/hook-state" "$dir/hook-state.blocks"
# A background check from an earlier start of this session describes another change.
if [ -d "$dir/broad" ]; then
  pid=$(cat "$dir/broad/pid" 2>/dev/null)
  [ -n "$pid" ] && kill "$pid" 2>/dev/null
  rm -rf "$dir/broad"
fi
exit 0
