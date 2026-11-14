# Running a scratch Legato from inside a sandbox

How an agent in Legato's sandbox runs a throwaway Legato (to try a change end to end) without reaching the real one that runs its session.

## Why it needs care

Inside a sandboxed session, `legato task|plan|review|swarm|learn|workflow …` is forwarded through the broker to the **real** Legato whenever `LEGATO_SANDBOXED=1` and `LEGATO_BROKER_SOCKET`/`LEGATO_BROKER_TOKEN` are set, which they always are in a sandbox. Pointing `legato` at a scratch config does not change that on its own: during LEG-150, test cards meant for a scratch board landed on the real one.

Since LEG-156, a forwardable command run in the sandbox with `LEGATO_CONFIG` set is refused (`scratch_instance_in_sandbox`) instead of forwarded, because the sandbox never inherits `LEGATO_CONFIG`: if it is set, you set it. The refusal is the guard; the steps below are how to do it right.

## Steps

1. Build to a path of your own, never `./legato` (rebuilding that restarts a Legato under hot reload):

   ```bash
   go build -o /tmp/lg/legato ./cmd/legato
   ```

2. Write a config whose database and data folder are under the scratch folder:

   ```yaml
   # /tmp/lg/config.yaml
   db:
     path: /tmp/lg/legato.db
   data_dir: /tmp/lg/data
   board:
     columns:
       - name: Backlog
       - name: Doing
       - name: Review
       - name: Done
   ```

3. Run every scratch command through one wrapper that drops the session's Legato variables and sets the scratch ones:

   ```bash
   cat >/tmp/lg/scratch <<'SH'
   #!/bin/sh
   exec env -u LEGATO_SANDBOXED -u LEGATO_BROKER_SOCKET -u LEGATO_BROKER_TOKEN \
     -u LEGATO_TASK_ID -u LEGATO_EXECUTION_ID -u LEGATO_SOCKET -u LEGATO_BIN \
     -u CLAUDE_CODE_PLUGIN_DIRS -u TMUX \
     LEGATO_CONFIG=/tmp/lg/config.yaml XDG_RUNTIME_DIR=/tmp/lg/xdg TMUX_TMPDIR=/tmp/lgt \
     "$@"
   SH
   chmod +x /tmp/lg/scratch
   mkdir -p /tmp/lg/xdg /tmp/lgt
   ```

   - Without `LEGATO_SANDBOXED` and the broker variables, `legato` opens the scratch database itself. The sandbox still hides the real data folder, so it cannot open the real one by mistake.
   - Keep `XDG_RUNTIME_DIR` and `TMUX_TMPDIR` short: they hold sockets, whose paths are limited to about 100 characters.
   - `LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID` would make the scratch instance act for your own session.

4. Use it:

   ```bash
   /tmp/lg/scratch /tmp/lg/legato columns sync
   /tmp/lg/scratch /tmp/lg/legato task create "Scratch card"
   /tmp/lg/scratch /tmp/lg/legato serve --port 3987 &   # a port of its own
   ```

5. Afterwards, stop what you started (the server, the scratch tmux server with `/tmp/lg/scratch tmux kill-server`) and remove `/tmp/lg` and `/tmp/lgt`.

## Checks

- If a scratch command answers `scratch_instance_in_sandbox`, it ran without the wrapper.
- If a card appears on the real board, stop and say so in a task note. Never clean it up through the real Legato without the operator.
- Agent tools started by the scratch instance still use your real tool state (`~/.claude`, `~/.codex`). See `docs/ai/claude-code-native.md` § Trying it without touching real state.
