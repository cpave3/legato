#!/usr/bin/env bash
# A scratch Legato, built from the working tree, for trying guidance with a
# real agent. It has its own config, database, sockets and tmux server, and
# its own binary: it never touches ./legato or the person's database.
#
#   scratch.sh up                         build the web app and the binary, start the server
#   scratch.sh task "<title>"             create a task; prints its ID
#   scratch.sh agent <task-id> <kind> [<working dir>]
#                                         start an agent (chimera, claude-code, codex)
#   scratch.sh ask <task-id> "<text>"     type a message into the agent's pane, as the person would
#   scratch.sh pane <task-id>             show the end of the agent's pane
#   scratch.sh key <task-id> <key>...     press keys in the pane (Enter, Escape, Down), to answer a prompt
#   scratch.sh wait [<seconds>]           wait until the agents have shown one more item
#   scratch.sh items                      list what the agents showed
#   scratch.sh dump <item-id> <file>      write an item's content to a file
#   scratch.sh url [<task-id> [<item-id>]]  print the web address and the token to paste
#   scratch.sh down                       stop the agents, the tmux server and the server
#
# LEGATO_SCRATCH (default /tmp/legato-scratch-<uid>) holds everything.
# LEGATO_SCRATCH_PORT (default 3987) is the web port.
#
# The agents are real: they use the person's own agent tool configuration and
# model credits, and they run in the working directory you give them.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../../../.." && pwd)
scratch=${LEGATO_SCRATCH:-/tmp/legato-scratch-$(id -u)}
port=${LEGATO_SCRATCH_PORT:-3987}
bin=$scratch/bin/legato
base=http://127.0.0.1:$port

# Started from inside a Legato session, a scratch Legato would inherit that
# session's identity and load its mods a second time.
unset TMUX CLAUDE_CODE_PLUGIN_DIRS LEGATO_TASK_ID LEGATO_EXECUTION_ID LEGATO_SOCKET LEGATO_BIN \
  LEGATO_TASK_EPHEMERAL LEGATO_MANTICORE_URL LEGATO_MANTICORE_API_KEY
# Socket paths have a length limit (about 100 characters): keep these short.
runtime=${XDG_RUNTIME_DIR:-/tmp}/lgs-$(id -u)
export LEGATO_CONFIG=$scratch/config.yaml XDG_RUNTIME_DIR=$runtime/rt TMUX_TMPDIR=$runtime/tm

api() { curl -sS -H "Authorization: Bearer $(cat "$scratch/data/auth-token")" -H "Content-Type: application/json" "$@"; }
legato() { (cd "$scratch" && "$bin" "$@"); }
count_items() { sqlite3 -readonly "$scratch/legato.db" "SELECT COUNT(*) FROM agent_ui_item_revisions" 2>/dev/null || echo 0; }

# Only this scratch server, found by its executable. A pattern match on
# command lines (pgrep -f, pkill -f) also matches the shell that runs it.
stop_server() {
  local pid exe
  for pid in $(pgrep -x legato || true); do
    exe=$(readlink "/proc/$pid/exe" 2>/dev/null || true)
    case "$exe" in "$bin" | "$bin (deleted)") kill "$pid" 2>/dev/null || true ;; esac
  done
}

command=${1:-}
[ $# -gt 0 ] && shift
case "$command" in
  up)
    mkdir -p "$scratch/bin" "$XDG_RUNTIME_DIR" "$TMUX_TMPDIR"
    [ -f "$LEGATO_CONFIG" ] || cat > "$LEGATO_CONFIG" <<EOF
db:
  path: $scratch/legato.db
data_dir: $scratch/data
board:
  columns:
    - name: Backlog
    - name: Doing
    - name: Done
EOF
    stop_server
    # The web app is embedded in the binary, so a change under web/ needs both.
    (cd "$root/web" && pnpm build > "$scratch/web-build.log" 2>&1) || { echo "web build failed: $scratch/web-build.log" >&2; exit 1; }
    (cd "$root" && go build -o "$bin" ./cmd/legato)
    legato columns sync > /dev/null
    (cd "$scratch" && nohup "$bin" serve --port "$port" > "$scratch/serve.log" 2>&1 &)
    for _ in $(seq 1 40); do
      [ -f "$scratch/data/auth-token" ] && [ "$(api -o /dev/null -w '%{http_code}' "$base/api/settings" 2>/dev/null)" = 200 ] && { echo "up: $base"; exit 0; }
      sleep 0.25
    done
    echo "the server did not come up: $scratch/serve.log" >&2
    exit 1
    ;;
  task)
    legato task create "${1:?title}" --json | python3 -c 'import json, sys; print(json.load(sys.stdin)["data"]["id"])'
    ;;
  agent)
    task=${1:?task id}
    kind=${2:?agent kind: chimera, claude-code or codex}
    dir=${3:-$root}
    api -X POST -d "{\"agent_kind\":\"$kind\",\"working_dir\":\"$dir\"}" "$base/api/tasks/$task/agent"
    echo
    ;;
  ask)
    task=${1:?task id}
    text=${2:?text}
    tmux send-keys -t "legato-$task" -l "$text"
    sleep 0.5
    tmux send-keys -t "legato-$task" Enter
    ;;
  pane)
    tmux capture-pane -p -t "legato-${1:?task id}" | grep -v '^[[:space:]]*$' | tail -n "${2:-20}"
    ;;
  key)
    task=${1:?task id}
    shift
    tmux send-keys -t "legato-$task" "${@:?key name, for example Enter}"
    ;;
  wait)
    limit=${1:-300}
    before=$(count_items)
    for _ in $(seq 1 $((limit / 5))); do
      [ "$(count_items)" -gt "$before" ] && { echo "shown"; exit 0; }
      sleep 5
    done
    echo "nothing new was shown in ${limit}s" >&2
    exit 1
    ;;
  items)
    "$here/items.sh" "$scratch/legato.db"
    ;;
  dump)
    "$here/items.sh" "$scratch/legato.db" "${1:?item id}" "${2:?file}"
    ;;
  url)
    if [ $# -ge 2 ]; then echo "$base/agents?task=$1&artifact=$2"; elif [ $# -eq 1 ]; then echo "$base/agents?task=$1"; else echo "$base/agents"; fi
    echo "token: $(cat "$scratch/data/auth-token")"
    ;;
  down)
    if [ -f "$scratch/data/auth-token" ]; then
      for task in $(api "$base/api/agents" 2>/dev/null | python3 -c 'import json, sys; [print(a["task_id"]) for a in json.load(sys.stdin)]' 2>/dev/null || true); do
        api -X POST -d "{\"task_id\":\"$task\"}" "$base/api/agents/kill" > /dev/null 2>&1 || true
      done
    fi
    tmux kill-server 2>/dev/null || true
    stop_server
    echo "down"
    ;;
  *)
    sed -n '2,21p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
