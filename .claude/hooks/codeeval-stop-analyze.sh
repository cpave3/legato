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
# Base, first that exists: $CODEEVAL_BASE, <git-dir>/codeeval-session-base
# (written by codeeval-session-base.sh), merge-base with origin/main, HEAD.
#
# Escape hatches, so the hook never traps an agent in a fixing loop or forces
# it to "fix" a false positive:
#   - it blocks at most once per turn (stop_hook_active);
#   - it never blocks twice on the same failure: analyze prints a failure
#     fingerprint (the failing items' identities), and fingerprints already
#     shown this session pass;
#   - at most CODEEVAL_HOOK_MAX_BLOCKS blocks per session (default 3);
#   - CODEEVAL_HOOK=off disables it;
#   - a false positive can be suppressed in code with a reasoned comment
#     `codeeval-ignore <rule-id>: <reason>` (listed in reports, not hidden).
# Session state lives in <git-dir>/codeeval-hook-state{,.blocks} and
# <git-dir>/codeeval-broad/ (reset by codeeval-session-base.sh).
#
# Environment (optional): CODEEVAL_BIN, CODEEVAL_BASE, CODEEVAL_CONFIG,
# CODEEVAL_DEBT_FAIL_ON (new-debt|net-debt|gates|none, default net-debt),
# CODEEVAL_DEFECTS_FAIL_ON (gate|any|none, default gate), CODEEVAL_PATHS,
# CODEEVAL_HOOK, CODEEVAL_HOOK_MAX_BLOCKS,
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
base=${CODEEVAL_BASE:-}
if [ -z "$base" ] && [ -s "$gitdir/codeeval-session-base" ]; then base=$(cat "$gitdir/codeeval-session-base"); fi
git rev-parse -q --verify "${base:-x}^{commit}" >/dev/null 2>&1 || base=$(git merge-base HEAD origin/main 2>/dev/null || git rev-parse HEAD)
mode=${CODEEVAL_STOP_CHECKS:-tiered}
args=(analyze --repo "$repo" --base "$base" --working-tree --debt-fail-on "${CODEEVAL_DEBT_FAIL_ON:-net-debt}"
  --defects-fail-on "${CODEEVAL_DEFECTS_FAIL_ON:-gate}" --files 15 --top 10 --format md)
[ -n "${CODEEVAL_CONFIG:-}" ] && args+=(--config "$CODEEVAL_CONFIG")
[ -n "${CODEEVAL_PATHS:-}" ] && args+=(--path "$CODEEVAL_PATHS")
state="$gitdir/codeeval-hook-state"
broad="$gitdir/codeeval-broad"

fingerprint() { printf '%s\n' "$1" | sed -n 's/^Failure fingerprint: `\([0-9a-f]*\)`.*/\1/p' | head -1; }
shown() { [ -n "$1" ] && [ -f "$state" ] && grep -qx "$1" "$state"; }

# 1. The foreground check.
if [ "$mode" = "all" ]; then
  out=$("$bin" "${args[@]}" 2>/dev/null); code=$?
else
  out=$("$bin" "${args[@]}" --checks quick 2>/dev/null); code=$?
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
  rm -f "$broad/exit" "$broad/out.md"
  if [ "$bcode" = "3" ]; then
    fp=$(fingerprint "$bout")
    if ! shown "$fp"; then
      fps+=("${fp:-broad-unknown}")
      [ -n "$msg" ] && msg="$msg"$'\n\n'
      msg="${msg}Background check (cross-file rules and near-duplication) of the working tree as of ${bwhen:-the previous stop}; if you have changed these files since, re-check with: codeeval analyze --base ${base:0:12} --working-tree --checks broad"$'\n\n'"$bout"
    fi
  fi
fi

# 3. Start the next background check, unless one is still running.
if [ "$mode" = "tiered" ]; then
  mkdir -p "$broad"
  pid=$(cat "$broad/pid" 2>/dev/null)
  if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
    date '+%H:%M:%S' > "$broad/started"
    # The pid file holds codeeval's own pid, so a new session can stop it.
    (
      "$bin" "${args[@]}" --checks broad > "$broad/out.tmp" 2>/dev/null &
      echo $! > "$broad/pid"
      wait $!
      c=$?
      [ -f "$broad/pid" ] || exit 0   # stopped by a new session
      mv "$broad/out.tmp" "$broad/out.md" && echo "$c" > "$broad/exit.tmp" && mv "$broad/exit.tmp" "$broad/exit"
      rm -f "$broad/pid"
    ) </dev/null >/dev/null 2>&1 &
  fi
fi

[ -z "$msg" ] && exit 0   # pass, already shown, or a tool error: never block on our own failure
blocks=$( [ -f "$state.blocks" ] && wc -l < "$state.blocks" || echo 0 )
[ "$blocks" -ge "${CODEEVAL_HOOK_MAX_BLOCKS:-3}" ] && exit 0            # session budget spent
printf '%s\n' "${fps[@]}" >> "$state"
echo block >> "$state.blocks"
{
  echo "codeeval analyze (base ${base:0:12} → working tree) failed: fix what this change introduced, or explain in your reply why it is justified."
  echo "This is reported once: the same findings will not block again. For a false positive, add \`codeeval-ignore <rule-id>: <reason>\` on or above the line rather than reshaping correct code."
  echo
  printf '%s\n' "$msg" | head -c 9000
} >&2
exit 2
