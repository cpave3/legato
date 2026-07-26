#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
run_dir="$repo_dir/.legato/run"
pid_file="$run_dir/server.pid"
log_file="$run_dir/server.log"
port="${LEGATO_SERVER_PORT:-3080}"
shutdown_timeout="${LEGATO_SHUTDOWN_TIMEOUT:-10}"

mkdir -p "$run_dir"

command_for_pid() {
  local pid="$1"
  ps -p "$pid" -o command= 2>/dev/null || true
}

listener_pids() {
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true
    return
  fi

  if command -v fuser >/dev/null 2>&1; then
    fuser "$port/tcp" 2>/dev/null | tr ' ' '\n' | sed '/^$/d'
    return
  fi

  echo "error: lsof or fuser is required to identify the process listening on port $port" >&2
  exit 1
}

stop_pid() {
  local pid="$1"
  local command
  command="$(command_for_pid "$pid")"

  if [[ "$command" != *legato* ]]; then
    echo "error: refusing to stop PID $pid on port $port because it is not Legato: $command" >&2
    exit 1
  fi

  echo "Stopping Legato PID $pid..."
  kill -TERM "$pid"

  local waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if (( waited >= shutdown_timeout )); then
      echo "error: Legato PID $pid did not stop within ${shutdown_timeout}s" >&2
      echo "       stop it manually or set LEGATO_SHUTDOWN_TIMEOUT to a larger value" >&2
      exit 1
    fi
    sleep 1
    waited=$((waited + 1))
  done
}

mapfile_compat() {
  while IFS= read -r line; do
    [[ -n "$line" ]] && listener_list+=("$line")
  done
}

listener_list=()
mapfile_compat < <(listener_pids)

if (( ${#listener_list[@]} > 0 )); then
  for pid in "${listener_list[@]}"; do
    stop_pid "$pid"
  done
fi

if [[ -f "$pid_file" ]]; then
  previous_pid="$(tr -cd '0-9' < "$pid_file")"
  if [[ -n "$previous_pid" ]] && kill -0 "$previous_pid" 2>/dev/null; then
    stop_pid "$previous_pid"
  fi
fi

echo "Building Legato and the embedded web client..."
(
  cd "$repo_dir"
  task build:full
)

echo "Starting Legato server on port $port..."
(
  cd "$repo_dir"
  nohup "$repo_dir/legato" serve --port "$port" >>"$log_file" 2>&1 &
  echo "$!" >"$pid_file"
)

new_pid="$(cat "$pid_file")"
for _ in 1 2 3 4 5; do
  if ! kill -0 "$new_pid" 2>/dev/null; then
    echo "error: Legato exited during startup; recent log output:" >&2
    tail -n 40 "$log_file" >&2 || true
    exit 1
  fi
  if command -v curl >/dev/null 2>&1 && curl --silent --fail --max-time 1 "http://127.0.0.1:$port/health" >/dev/null; then
    echo "Legato is healthy (PID $new_pid)."
    echo "Log: $log_file"
    exit 0
  fi
  sleep 1
done

echo "Legato is running (PID $new_pid), but its health endpoint was not ready after 5s."
echo "Log: $log_file"
