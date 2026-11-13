# Recorded Claude Code hook sequences

Each `.jsonl` file is one session's hook payloads in the order Claude Code
2.1.322 delivered them, one payload per line, recorded on 2026-11-12 for
LEG-144 (see `docs/ai/agent-states.md`). Paths and session IDs are replaced;
everything else is as Claude Code sent it.

- `background-bash`, `several-jobs-and-subagent`, `monitor`, `cron`, `api-error`
  and `clear` were captured from `claude -p` and an interactive session with
  a hook that appended stdin to a file.
- `permission-allowed`, `permission-denied`, `question`, `interrupt` and
  `compact` were captured from an interactive session in tmux and are
  written out from that log with long fields shortened.

A sequence that ends early (`permission-denied`, `interrupt`) ends there
because Claude Code fired no further hook.
