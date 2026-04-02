## 1. Config & LLM Client (Engine Layer)

- [ ] 1.1 Add `LLM` struct to `config/config.go` with `Endpoint`, `Model`, `APIKey` fields. Update YAML parsing to handle optional `llm` section with env var expansion on `api_key`. Add config tests.
- [ ] 1.2 Create `internal/engine/llm/client.go` — `Client` struct with `baseURL`, `apiKey`, `model`, `*http.Client`. Implement `ChatCompletion(ctx, []Message) (string, error)` hitting `POST <baseURL>/chat/completions`. Handle: auth header when key present, omit when empty, timeout via context, non-200 error wrapping. Add request/response types (`Message`, `ChatRequest`, `ChatResponse`).
- [ ] 1.3 Add `internal/engine/llm/client_test.go` — use `httptest.NewServer` to verify: successful completion, missing auth header when no key, error on non-200, context cancellation/timeout.

## 2. Tmux Input Injection (Engine Layer)

- [ ] 2.1 Add `SendKeys(session, keys string, literal bool) error` method to `Manager` in `internal/engine/tmux/tmux.go`. When `literal` is true, use `-l` flag. Return error if tmux command fails.
- [ ] 2.2 Add `SendKeys` to `TmuxManager` interface in `internal/service/agent.go`.
- [ ] 2.3 Add tests for `SendKeys` in tmux package (mock exec, verify `-l` flag logic). Update mock in service tests to implement the new interface method.

## 3. Intent Service (Service Layer)

- [ ] 3.1 Create `internal/service/intent.go` — define `LLMProvider` interface (`Complete(ctx, system, prompt string) (string, error)`), `AgentIntent` struct, and `IntentService` struct/interface. `IntentService` takes `LLMProvider`, `TmuxManager`, `*store.Store`. Implement `ParseIntent(ctx, taskID string) (*AgentIntent, error)`.
- [ ] 3.2 Implement ANSI stripping utility (regex-based, strip CSI/OSC/SGR sequences). Add to `internal/service/intent.go` or a small helper. Add unit tests for stripping.
- [ ] 3.3 Implement the LLM prompt: system prompt instructing JSON output with `action`, `description`, `risk_level`, `approve_text`, `deny_text` fields. Include the cleaned pane output as user message. Parse JSON response with fallback (try raw, try extracting from code fences, fall back to generic intent).
- [ ] 3.4 Create `internal/engine/llm/adapter.go` — adapter wrapping `Client` to implement `LLMProvider` interface.
- [ ] 3.5 Add `RespondToAgent(ctx, taskID, response string) error` to `AgentService` — look up tmux session, call `SendKeys` with literal text, then `SendKeys` with "Enter".
- [ ] 3.6 Add intent caching to `IntentService` — `map[string]*AgentIntent` keyed by taskID, cleared on state change away from `waiting`. Add `ClearIntent(taskID)` and `GetCachedIntent(taskID)` methods.
- [ ] 3.7 Add tests: mock LLM provider + mock tmux, verify parse flow, ANSI stripping, JSON fallback, caching, respond-to-agent.

## 4. Wiring (cmd/legato)

- [ ] 4.1 In `cmd/legato/main.go`: if `cfg.LLM.Endpoint` is set, create `llm.Client`, wrap in adapter, create `IntentService`. Pass `IntentService` (nil-safe) to `NewApp`.
- [ ] 4.2 Update `NewApp` signature to accept optional `IntentService`. Store on app model.

## 5. Agent Intent UI (TUI Layer)

- [ ] 5.1 Add new messages to `internal/tui/agents/messages.go`: `IntentParsingMsg{TaskID}`, `IntentParsedMsg{TaskID, Intent}`, `IntentRespondMsg{TaskID, Response}`, `IntentClearedMsg{TaskID}`.
- [ ] 5.2 Add intent state to `agents.Model`: `intentLoading map[string]bool`, `intents map[string]*AgentIntent` (from service layer type or local copy). Render intent panel in `View()` when selected agent is waiting and has an intent.
- [ ] 5.3 Implement intent panel rendering: bordered box at bottom of terminal area showing action, description, risk badge (colored), and `y`/`n` key hints. Show "Analyzing..." when loading. Show "Attach to terminal to respond" when approve/deny text is empty.
- [ ] 5.4 Handle `y`/`n` key presses in agent view `Update()` — only active when intent panel is visible with non-empty approve/deny text. Emit `IntentRespondMsg` with the appropriate text.
- [ ] 5.5 In `app.go`: on `agent_state` IPC with `waiting` activity, trigger `IntentService.ParseIntent` as a `tea.Cmd`. On `IntentParsedMsg`, update agent model. On `IntentRespondMsg`, call `AgentService.RespondToAgent`. On state change away from waiting, call `IntentService.ClearIntent` and send `IntentClearedMsg`.
- [ ] 5.6 Add agent view tests: verify intent panel renders when intent is set, `y`/`n` produce correct messages, panel hidden when no intent.

## 6. Integration & Polish

- [ ] 6.1 End-to-end manual test: configure local LLM (Ollama), spawn agent, trigger waiting state, verify intent panel appears with correct summary, approve/deny injects response.
- [ ] 6.2 Add `llm` config section to example config / setup documentation.
- [ ] 6.3 Update CLAUDE.md with new package descriptions, config fields, and architecture notes.
