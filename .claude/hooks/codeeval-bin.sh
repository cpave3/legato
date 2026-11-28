#!/bin/sh
# Prints the CodeEval binary the CodeEval hooks run, or nothing when there is
# none; it always exits 0. Legato's own, not CodeEval's: `codeeval hook
# install` pins the installing machine's path in .claude/settings.json, so
# the committed commands ask this script instead (LEG-707).
#
# First that exists: $CODEEVAL_BIN, the shared install
# $HOME/.local/share/codeeval/current/codeeval (mounted read-only into every
# Legato sandbox), then codeeval on PATH. With nothing printed, the commands
# in .claude/settings.json run neither hook, so nothing is printed (LEG-713).
if [ -n "${CODEEVAL_BIN:-}" ]; then
  printf '%s\n' "$CODEEVAL_BIN"
elif [ -x "${HOME:-}/.local/share/codeeval/current/codeeval" ]; then
  printf '%s\n' "$HOME/.local/share/codeeval/current/codeeval"
else
  command -v codeeval 2>/dev/null
fi
exit 0
