#!/usr/bin/env bash
# Fails when a migration is added whose file name sorts before the newest
# migration already released or on main. The store records each migration by
# its position in the sorted list, so one that sorts in the middle shifts the
# rest, and every database made before it refuses to open (LEG-249).
#
#   migration-order.sh REF...                  compare the working tree's
#                                              migrations with those in REFs
#   migration-order.sh --lists RELEASED CURRENT  compare two files of names
#
# Run by .github/workflows/ci.yml with the latest release tag, and on a pull
# request its base branch too; tested by cmd/legato/migration_order_test.go.
set -euo pipefail
export LC_ALL=C # the store sorts names byte by byte

dir=internal/engine/store/migrations

usage() {
  echo "usage: migration-order.sh REF... | --lists RELEASED CURRENT" >&2
  exit 2
}

[ $# -gt 0 ] || usage
if [ "$1" = --lists ]; then
  [ $# -eq 3 ] || usage
  released=$(grep . "$2" | sort -u)
  current=$(grep . "$3" | sort -u)
else
  released=$(for ref in "$@"; do
    git ls-tree --name-only "$ref" "$dir/" | sed 's|.*/||'
  done | grep '\.sql$' | sort -u)
  current=$(ls "$dir" | grep '\.sql$' | sort -u)
fi

newest=$(printf '%s\n' "$released" | tail -n 1)
added=$(comm -13 <(printf '%s\n' "$released") <(printf '%s\n' "$current"))

status=0
while IFS= read -r name; do
  [ -n "$name" ] || continue
  if [[ "$name" < "$newest" ]]; then
    echo "migration $name sorts before $newest, which is already released or on main:" >&2
    echo "  rename it to sort last (after $newest), or databases made before it will not open." >&2
    status=1
  fi
done <<<"$added"
exit $status
