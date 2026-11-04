#!/usr/bin/env bash
# Prints true when this release should build the desktop app, else false.
# Used by .github/workflows/release.yml with the workflow's desktop input:
#   always, never, or auto: build when desktop/ changed since the newest
#   stable release that carries the AppImage (or when none does yet).
# Needs gh (with GH_TOKEN) and the repository's tags.
set -euo pipefail

mode="${1:-auto}"
case "$mode" in
  always) echo true; exit 0 ;;
  never) echo false; exit 0 ;;
  auto) ;;
  *)
    echo "usage: desktop-changed.sh auto|always|never" >&2
    exit 2
    ;;
esac

# A failed listing stops here (set -e), rather than reading as "none".
tags="$(gh release list --limit 50 --exclude-drafts --exclude-pre-releases --json tagName --jq '.[].tagName')"
last=""
for tag in $tags; do
  if gh release view "$tag" --json assets --jq '.assets[].name' | grep -qx 'legato-desktop-x86_64.AppImage'; then
    last="$tag"
    break
  fi
done

if [ -z "$last" ]; then
  echo true
elif git diff --quiet "$last" HEAD -- desktop/; then
  echo false
else
  echo true
fi
