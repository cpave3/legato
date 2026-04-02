## Context

Legato manages AI agents in tmux sessions. When an agent enters "waiting" state (e.g. Claude Code asking for tool permission), the user must currently attach to the terminal to read the question and respond. The agent view already polls capture-pane at 200ms and displays terminal output. We want to intercept the `waiting` transition, parse the pane output with an LLM, and surface a structured intent summary with approve/deny actions — avoiding the need to enter the terminal for routine permission prompts.

This is an experimental feature. The LLM provider should be pluggable (starting with OpenAI-compatible endpoints for local LLMs), and the feature must gracefully degrade when no LLM is configured.

## Goals / Non-Goals

**Goals:**
- Parse agent pane output on `waiting` state to extract what the agent is asking permission for
- Present a structured summary (action type, description, risk level) in the agent split-view
- Allow approve/deny via keyboard shortcuts without attaching to the terminal
- Inject the user's response back into the tmux session via `send-keys`
- Use an OpenAI-compatible API client with a provider-agnostic interface for easy swapping
- Graceful degradation: no LLM configured → existing behavior, no intent panel

**Non-Goals:**
- Supporting multi-turn conversations with the agent through the TUI (MVP is single approve/deny)
- Parsing complex multi-step plans or diffs (MVP focuses on tool permission prompts)
- Streaming LLM responses to the UI (fire-and-forget, show result when ready)
- Web UI integration (future — TUI is the first consumer)
- Auto-approve policies or trust levels (future feature)
- Token optimization or prompt caching (keep it simple for MVP)

## Decisions

### 1. LLM Client: Thin OpenAI-compatible HTTP client in engine/

**Decision**: Hand-roll a minimal OpenAI chat completions client in `internal/engine/llm/` rather than pulling in a third-party library.

**Rationale**: The project follows a pattern of thin HTTP clients (see `internal/engine/jira/client.go`). The OpenAI chat completions API is a single endpoint (`POST /v1/chat/completions`) — a dependency like `go-openai` would bring far more surface area than needed. A thin client stays consistent with the codebase and keeps the dependency tree lean.

**Interface**: A `Provider` interface in the service layer allows swapping implementations later (Anthropic, Ollama native, etc.) without changing the service.

```go
// engine/llm/client.go
type Client struct { baseURL, apiKey, model string; http *http.Client }
func (c *Client) ChatCompletion(ctx context.Context, messages []Message) (string, error)

// service/intent.go
type LLMProvider interface {
    Complete(ctx context.Context, system string, prompt string) (string, error)
}
```

### 2. Intent parsing triggered by IPC `agent_state` → `waiting`

**Decision**: Trigger intent parsing when the agent state transitions to `waiting`, not on every capture-pane poll.

**Rationale**: The `waiting` state is set by Claude Code's `Stop` hook, which fires exactly when the agent finishes and is waiting for input. This is the precise moment we need to parse. Polling-based detection would be wasteful and imprecise. The IPC message already flows through the event bus — we add a listener in the service layer.

**Flow**:
1. Hook fires → `legato agent state <id> --activity waiting` → IPC broadcast
2. TUI receives IPC → publishes `EventAgentStateChanged`
3. App triggers `IntentService.ParseIntent(ctx, taskID)`
4. IntentService captures pane, calls LLM, returns `AgentIntent`
5. TUI receives intent via `IntentParsedMsg` → renders in agent panel

### 3. Pane output preprocessing

**Decision**: Capture the last N lines (configurable, default 50) of pane output, strip ANSI escape codes, and send as the user prompt to the LLM.

**Rationale**: Full scrollback is too large and mostly irrelevant. The permission prompt is always at the bottom of the pane. ANSI codes add noise. 50 lines gives enough context for the LLM to understand multi-line tool calls (e.g. file diffs) while keeping token usage reasonable.

### 4. Structured intent output via JSON mode

**Decision**: Use a system prompt that instructs the LLM to return a JSON object with `action`, `description`, `risk_level`, and `approve_text`/`deny_text` fields.

**Rationale**: Structured output makes rendering deterministic. The `approve_text` and `deny_text` fields tell us exactly what keystrokes to inject (e.g. "y\n" or "n\n"). Risk level (low/medium/high) can drive visual styling. JSON parsing is simple and reliable with local LLMs that support structured output.

```go
type AgentIntent struct {
    Action      string // e.g. "execute_command", "edit_file", "read_file"
    Description string // human-readable summary
    RiskLevel   string // "low", "medium", "high"
    ApproveText string // text to send-keys on approve (e.g. "y")
    DenyText    string // text to send-keys on deny (e.g. "n")
}
```

### 5. Response injection via tmux send-keys

**Decision**: Add a `SendKeys(session, keys string, literal bool)` method to `TmuxManager` interface and use it to inject approve/deny responses.

**Rationale**: `tmux send-keys` is the standard way to inject input. The `literal` flag (`-l`) prevents key name interpretation for text responses. This keeps the injection mechanism simple and reusable.

### 6. UI: Intent panel in agent split-view terminal area

**Decision**: When an intent is available for the selected waiting agent, render a compact intent summary panel overlaid on the terminal output area (bottom portion), with `y` to approve and `n` to deny.

**Rationale**: Keeps the UI change minimal — no new views or overlays. The terminal output is already visible; we add a context panel that appears only when relevant. `y`/`n` are intuitive and don't conflict with existing agent view keybindings.

**States:**
- No intent / LLM not configured → normal terminal view (no change)
- Intent loading → small "Analyzing..." indicator
- Intent ready → summary panel with action, description, risk badge, `y`/`n` hints
- After response → panel dismissed, terminal view resumes

### 7. Config structure

**Decision**: New `llm` section in config.yaml, optional.

```yaml
llm:
  endpoint: "http://localhost:11434/v1"  # OpenAI-compatible endpoint
  model: "qwen2.5:7b"
  api_key: "${LEGATO_LLM_API_KEY}"       # optional, env var expanded
```

**Rationale**: Follows existing config patterns (env var expansion, optional sections). Endpoint + model + optional API key covers all OpenAI-compatible providers (Ollama, LM Studio, vLLM, OpenAI itself).

## Risks / Trade-offs

- **LLM latency** → Local LLMs respond in 1-5s; show "Analyzing..." state so user can still attach manually. Add a configurable timeout (default 15s) after which intent parsing is abandoned.
- **Parsing accuracy** → Local LLMs may misparse complex prompts. The `ApproveText`/`DenyText` fields mitigate this — if the LLM gets the action summary wrong but the response text right, the approve/deny still works correctly. "Attach to terminal" remains available as escape hatch.
- **JSON output reliability** → Some local LLMs struggle with strict JSON. Use a lenient parser that extracts JSON from markdown code fences. Fall back to a generic "Agent is waiting for input" intent if parsing fails.
- **send-keys injection timing** → The agent process should be waiting for stdin when we inject. Since we only inject in response to `waiting` state (agent is blocked on input), timing should be safe. Add a small delay (100ms) before injection as a safety margin.
- **Pane output may not contain the prompt** → If the terminal has been scrolled or the prompt is above the visible area, we might miss it. `capture-pane -p -S -50` captures scrollback regardless of scroll position, mitigating this.

## Open Questions

- Should we cache intents per agent session to avoid re-parsing if the user navigates away and back? (Leaning yes — cache until next state change.)
- Should `deny` responses include a way to add a message? (Not for MVP — just reject. Future: `d` for deny-with-message overlay.)
- What's the right tail-line count for different agent types? (Start with 50, make configurable if needed.)
