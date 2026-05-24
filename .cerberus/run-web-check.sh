#!/usr/bin/env bash
# Cerberus wrapper for running web/ subproject commands from repo root.
# Strips the leading "web/" from file paths so they resolve inside web/.
# Also filters out non-web/ files, since {files} may include Go files.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root/web" || exit 1

# Rebuild args: strip "web/" prefix, drop files not under web/
declare -a shifted_args
for arg in "$@"; do
  if [[ "$arg" == web/* ]]; then
    shifted_args+=("${arg#web/}")
  elif [[ "$arg" == *.go || "$arg" == internal/* || "$arg" == cmd/* || "$arg" == config/* ]]; then
    continue
  else
    shifted_args+=("$arg")
  fi
done

exec "${shifted_args[@]}"
