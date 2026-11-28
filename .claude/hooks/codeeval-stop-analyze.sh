#!/usr/bin/env bash
# Claude Code "Stop" hook: before Claude finishes, check everything changed
# since the session base, in two tiers so the agent never waits long:
#
#   quick (every stop, in the foreground, about 1-2 s): v2 debt, and the
#     single-file defect rules on the changed files only
#     (`codeeval analyze --checks quick`);
#   broad (in the background): cross-file defect rules (unused exports,
#     dependency and import-policy rules ...) and near-duplication
#     (`--checks broad`, about 20-30 s on a large repository). It starts at
#     each stop and its result is reported at the next one.
#
# When a check fails it exits 2 with the report head on stderr, which Claude
# Code feeds back to Claude. A background result describes the working tree
# as it was at the previous stop, and says so.
#
# Base, first that exists: $CODEEVAL_BASE; the session's checked-base (HEAD
# at its last stop that did not block, so a later turn is not blocked on
# committed work an earlier check already covered); the session's
# session-base (written by codeeval-session-base.sh); merge-base with
# origin/main; HEAD.
#
# Debt may rise by up to CODEEVAL_DEBT_BUDGET points (default 10) before the
# hook blocks: scoring/v2 is uncalibrated, and a safety check always adds
# branches. Once the branch's pull request is approved (gh pr view), a
# failure is reported to the user and does not block, so a late finding does
# not send approved work back for review (CODEEVAL_HOOK_WHEN_APPROVED=block
# keeps blocking). SessionStart prints the same quick check for the agent to
# run before it pushes.
#
# Escape hatches, so the hook never traps an agent in a fixing loop or forces
# it to "fix" a false positive:
#   - it blocks at most once per turn (stop_hook_active);
#   - it never blocks twice on the same failure: analyze prints a failure
#     fingerprint (the failing items' identities), and fingerprints already
#     shown this session pass;
#   - at most CODEEVAL_HOOK_MAX_BLOCKS blocks per session (default 3);
#   - it does not block once the pull request is approved (see above);
#   - CODEEVAL_HOOK=off disables it;
#   - a false positive can be suppressed in code with a reasoned comment
#     `codeeval-ignore <rule-id>: <reason>` (listed in reports, not hidden).
# State is per Claude session, in <git-dir>/codeeval/<session_id>/:
# session-base, checked-base, broad-base, hook-state{,.blocks} and broad/ (reset by
# codeeval-session-base.sh when that session starts).
#
# Environment (optional): CODEEVAL_BIN, CODEEVAL_BASE, CODEEVAL_CONFIG,
# CODEEVAL_DEBT_FAIL_ON (new-debt|net-debt|gates|none, default net-debt),
# CODEEVAL_DEBT_BUDGET (default 10), CODEEVAL_DEFECTS_FAIL_ON (gate|any|none,
# default gate), CODEEVAL_PATHS, CODEEVAL_HOOK, CODEEVAL_HOOK_MAX_BLOCKS,
# CODEEVAL_HOOK_WHEN_APPROVED (advisory|block, default advisory), CODEEVAL_GH,
# CODEEVAL_STOP_CHECKS (tiered: quick + background broad, the default;
# quick: no background check; all: everything in the foreground, slow on
# large repositories).
set -u
input=$(cat)
[ "${CODEEVAL_HOOK:-on}" = "off" ] && exit 0
if printf '%s' "$input" | grep -q '"stop_hook_active"[[:space:]]*:[[:space:]]*true'; then exit 0; fi
bin=${CODEEVAL_BIN:-codeeval}
command -v "$bin" >/dev/null 2>&1 || [ -x "$bin" ] || exit 0
repo=${CLAUDE_PROJECT_DIR:-$(pwd)}
cd "$repo" 2>/dev/null || exit 0
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || exit 0
gitdir=$(git rev-parse --absolute-git-dir)
sid=$(printf '%s' "$input" | sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_-]*\)".*/\1/p' | head -1)
dir="$gitdir/codeeval/${sid:-default}"
mkdir -p "$dir" || exit 0
valid() { [ -n "$1" ] && git rev-parse -q --verify "$1^{commit}" >/dev/null 2>&1; }
# Each tier keeps its own checkpoint (checked-base, broad-base) on top of the
# session's base, so a broad run still in flight never loses the commits the
# quick tier has already moved past.
since() {
  local b=${CODEEVAL_BASE:-} f
  for f in "$@" "$dir/session-base"; do
    if ! valid "$b" && [ -s "$f" ]; then b=$(cat "$f"); fi
  done
  valid "$b" || b=$(git merge-base HEAD origin/main 2>/dev/null || git rev-parse HEAD)
  echo "$b"
}
base=$(since "$dir/checked-base")
bbase=$(since "$dir/broad-base")
head=$(git rev-parse -q --verify HEAD 2>/dev/null)
mode=${CODEEVAL_STOP_CHECKS:-tiered}
args=(analyze --repo "$repo" --working-tree --debt-fail-on "${CODEEVAL_DEBT_FAIL_ON:-net-debt}"
  --debt-budget "${CODEEVAL_DEBT_BUDGET:-10}" --defects-fail-on "${CODEEVAL_DEFECTS_FAIL_ON:-gate}" --files 15 --top 10 --format md)
[ -n "${CODEEVAL_CONFIG:-}" ] && args+=(--config "$CODEEVAL_CONFIG")
[ -n "${CODEEVAL_PATHS:-}" ] && args+=(--path "$CODEEVAL_PATHS")
state="$dir/hook-state"
broad="$dir/broad"

fingerprint() { printf '%s\n' "$1" | sed -n 's/^Failure fingerprint: `\([0-9a-f]*\)`.*/\1/p' | head -1; }
shown() { [ -n "$1" ] && [ -f "$state" ] && grep -qx "$1" "$state"; }
# checked: the foreground check covered everything up to HEAD without
# blocking, so the next stop starts from here.
checked() { [ -n "$head" ] && { [ "$code" -eq 0 ] || [ "$code" -eq 3 ]; } && echo "$head" > "$dir/checked-base"; }
approved() {
  local gh=${CODEEVAL_GH:-gh} t=() d
  [ "${CODEEVAL_HOOK_WHEN_APPROVED:-advisory}" = "advisory" ] && command -v "$gh" >/dev/null 2>&1 || return 1
  command -v timeout >/dev/null 2>&1 && t=(timeout 10)
  d=$(${t[@]+"${t[@]}"} "$gh" pr view --json reviewDecision -q .reviewDecision 2>/dev/null) && [ "$d" = "APPROVED" ]
}

# 1. The foreground check.
if [ "$mode" = "all" ]; then
  out=$("$bin" "${args[@]}" --base "$base" 2>/dev/null); code=$?
else
  out=$("$bin" "${args[@]}" --base "$base" --checks quick 2>/dev/null); code=$?
fi
msg="" fps=()
if [ "$code" -eq 3 ]; then
  fp=$(fingerprint "$out")
  if ! shown "$fp"; then
    fps+=("${fp:-quick-unknown}")
    msg="$out"
  fi
fi

# 2. The previous stop's background check, if it has finished.
if [ "$mode" = "tiered" ] && [ -f "$broad/exit" ]; then
  bcode=$(cat "$broad/exit") bout=$(cat "$broad/out.md" 2>/dev/null) bwhen=$(cat "$broad/started" 2>/dev/null)
  bhead=$(cat "$broad/head" 2>/dev/null) bfrom=$bbase
  rm -f "$broad/exit" "$broad/out.md"
  fp=$(fingerprint "$bout")
  if [ "$bcode" = "3" ] && ! shown "$fp"; then
    fps+=("${fp:-broad-unknown}")
    [ -n "$msg" ] && msg="$msg"$'\n\n'
    msg="${msg}Background check (cross-file rules and near-duplication) of the working tree as of ${bwhen:-the previous stop}; if you have changed these files since, re-check with: codeeval analyze --base ${bfrom:0:12} --working-tree --checks broad"$'\n\n'"$bout"
  elif { [ "$bcode" = "0" ] || [ "$bcode" = "3" ]; } && valid "$bhead"; then
    # That run covered everything up to the HEAD it started at.
    echo "$bhead" > "$dir/broad-base"
    bbase=$bhead
  fi
fi

# 3. Start the next background check, unless one is still running.
if [ "$mode" = "tiered" ]; then
  mkdir -p "$broad"
  pid=$(cat "$broad/pid" 2>/dev/null)
  if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
    date '+%H:%M:%S' > "$broad/started"
    printf '%s\n' "$head" > "$broad/head"
    # The pid file holds codeeval's own pid, so a new session can stop it.
    (
      "$bin" "${args[@]}" --base "$bbase" --checks broad > "$broad/out.tmp" 2>/dev/null &
      echo $! > "$broad/pid"
      wait $!
      c=$?
      [ -f "$broad/pid" ] || exit 0   # stopped by a new session
      mv "$broad/out.tmp" "$broad/out.md" && echo "$c" > "$broad/exit.tmp" && mv "$broad/exit.tmp" "$broad/exit"
      rm -f "$broad/pid"
    ) </dev/null >/dev/null 2>&1 &
  fi
fi

[ -z "$msg" ] && { checked; exit 0; }   # pass, already shown, or a tool error: never block on our own failure
blocks=$( [ -f "$state.blocks" ] && wc -l < "$state.blocks" || echo 0 )
[ "$blocks" -ge "${CODEEVAL_HOOK_MAX_BLOCKS:-3}" ] && { checked; exit 0; }   # session budget spent
printf '%s\n' "${fps[@]}" >> "$state"
if approved; then
  checked
  printf '{"systemMessage": "CodeEval found new findings since %s, but this branch'"'"'s pull request is already approved, so the Stop hook did not block. To see them: codeeval analyze --base %s --working-tree"}\n' "${base:0:12}" "${base:0:12}"
  exit 0
fi
echo block >> "$state.blocks"
{
  echo "codeeval analyze (base ${base:0:12} → working tree) failed: fix what this change introduced, or explain in your reply why it is justified."
  echo "This is reported once: the same findings will not block again. For a false positive, add \`codeeval-ignore <rule-id>: <reason>\` on or above the line rather than reshaping correct code."
  echo
  printf '%s\n' "$msg" | head -c 9000
} >&2
exit 2
