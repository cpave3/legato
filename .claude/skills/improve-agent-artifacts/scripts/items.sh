#!/usr/bin/env bash
# Reads what agents showed from a Legato database, read-only.
#
#   items.sh <database>                    list every item, newest last
#   items.sh <database> <item-id> <file>   write an item's latest content to a file
#
# The person's own database is ~/.local/share/legato/legato.db unless their
# config says otherwise. This script never writes to it.
set -euo pipefail
db=${1:?usage: items.sh <database> [<item-id> <file>]}
if [ $# -eq 1 ]; then
  sqlite3 -readonly "$db" "SELECT i.id, i.task_id, i.placement, r.revision, r.format, length(r.content) || ' bytes', r.title
    FROM agent_ui_items i JOIN agent_ui_item_revisions r ON r.item_id = i.id AND r.revision = i.revision
    ORDER BY r.rowid"
else
  item=${2:?item id}
  out=${3:?file to write}
  case "$item" in ui-[0-9a-f]*) ;; *) echo "items.sh: '$item' is not an item id (ui-<hex>)" >&2; exit 2 ;; esac
  sqlite3 -readonly "$db" "SELECT r.content FROM agent_ui_items i JOIN agent_ui_item_revisions r ON r.item_id = i.id AND r.revision = i.revision WHERE i.id = '$item'" > "$out"
  [ -s "$out" ] || { echo "items.sh: no item $item in $db" >&2; exit 3; }
  echo "wrote $out"
fi
