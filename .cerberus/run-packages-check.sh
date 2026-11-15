#!/usr/bin/env bash
# Cerberus wrapper for running a command in the packages/ workspace from the
# repo root. Strips the leading "packages/" from file paths so they resolve
# inside packages/, and drops files outside it, since {files} may hold others.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root/packages" || exit 1

declare -a shifted_args
for arg in "$@"; do
  if [[ "$arg" == packages/* ]]; then
    shifted_args+=("${arg#packages/}")
  elif [[ "$arg" == */* || "$arg" == *.go ]]; then
    continue
  else
    shifted_args+=("$arg")
  fi
done

exec "${shifted_args[@]}"
