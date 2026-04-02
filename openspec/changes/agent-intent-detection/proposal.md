## Why

When AI agents (e.g. Claude Code) running in legato-managed tmux sessions enter a "waiting" state, they're typically asking the user for permission to perform an action (run a command, edit a file, etc.). Currently the user must attach to the terminal session, read the context, and type a response. This friction scales poorly — especially with multiple agents — and blocks the path to a web UI where terminal attach isn't available. By parsing the agent's pane output with an LLM and surfacing structured intent summaries with approve/deny actions directly in the TUI, users can manage agent permissions without leaving the board.

## What Changes

- New `internal/engine/llm/` package providing an OpenAI-compatible chat completion client, abstractable behind a provider interface for easy swapping (local LLM, OpenAI, Anthropic, etc.)
- New `internal/service/intent/` service that captures tmux pane output on `waiting` state transitions, sends it to the LLM for intent extraction, and produces structured `AgentIntent` objects
- Updated agent split-view (`internal/tui/agents/`) to display parsed intent summaries with approve/deny actions when hovering over a waiting agent
- `send-keys` injection in `internal/engine/tmux/` to deliver user responses back to the agent session
- New config section for LLM provider settings (endpoint, model, API key via env var)

## Capabilities

### New Capabilities
- `llm-client`: OpenAI-compatible LLM client with provider abstraction for chat completions
- `agent-intent-parsing`: Service that extracts structured intents from captured pane output using the LLM client
- `agent-intent-ui`: TUI overlay/panel in agent split-view showing parsed intents with approve/deny quick actions
- `tmux-input-injection`: Send-keys support in tmux manager for injecting user responses into agent sessions

### Modified Capabilities
- `agent-split-view`: Add intent summary panel and approve/deny keybindings when viewing a waiting agent

## Impact

- **New dependency**: Go LLM client library (e.g. `github.com/sashabaranov/go-openai` or similar OpenAI-compatible library) — or hand-rolled thin client following the existing Jira HTTP client pattern
- **Config**: New `llm` section in `config.yaml` (endpoint, model, api_key env var)
- **Engine layer**: New `llm/` package, extended `tmux/` with `SendKeys`
- **Service layer**: New `intent/` service consuming `llm/` and `tmux/` engine packages
- **TUI layer**: Modified agent view to render intents and handle approve/deny input
- **Experimental**: Feature is opt-in, gracefully degrades when LLM is not configured (no intent parsing, existing behavior preserved)
