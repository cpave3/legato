package claudebridge

import (
	"net/http"
	"strings"
	"testing"
)

type modelEntry struct {
	Ref       string `json:"ref"`
	IsDefault bool   `json:"isDefault"`
}

type settingsState struct {
	Model      string         `json:"model"`
	Overrides  map[string]any `json:"overrides"`
	Defaults   map[string]any `json:"defaults"`
	Effective  map[string]any `json:"effective"`
	Capability struct {
		SchemaVersion int    `json:"schemaVersion"`
		ModelID       string `json:"modelId"`
		Settings      map[string]struct {
			Status  string   `json:"status"`
			Type    string   `json:"type"`
			Values  []string `json:"values"`
			Default any      `json:"default"`
		} `json:"settings"`
	} `json:"capabilities"`
}

func newModelHarness(t *testing.T, models ...string) *harness {
	t.Helper()
	h := newHarness(t)
	h.bridge.models = models
	return h
}

func (h *harness) models() []modelEntry {
	h.t.Helper()
	var models []modelEntry
	if code := h.getJSON("/v1/sessions/task-1/models", &models); code != http.StatusOK {
		h.t.Fatalf("GET models = %d", code)
	}
	return models
}

func (h *harness) settings() settingsState {
	h.t.Helper()
	var state settingsState
	if code := h.getJSON("/v1/sessions/task-1/model", &state); code != http.StatusOK {
		h.t.Fatalf("GET model = %d", code)
	}
	return state
}

func TestModelListOffersTheSessionsModelAndTheConfiguredOnes(t *testing.T) {
	h := newModelHarness(t, "claude-sonnet-5-5", "claude-haiku-5-5", "claude-opus-5-5")
	h.mod(hello(nil)) // the session runs claude-opus-5-5

	models := h.models()

	// The session's own model leads and is the default; a configured model
	// that is the same one is not listed twice.
	want := []modelEntry{
		{Ref: "anthropic/claude-opus-5-5", IsDefault: true},
		{Ref: "anthropic/claude-sonnet-5-5"},
		{Ref: "anthropic/claude-haiku-5-5"},
	}
	if len(models) != len(want) {
		t.Fatalf("models = %+v", models)
	}
	for index, entry := range want {
		if models[index] != entry {
			t.Fatalf("models[%d] = %+v, want %+v", index, models[index], entry)
		}
	}
}

func TestChoosingAModelOverridesItForThisSessionOnly(t *testing.T) {
	h := newModelHarness(t, "claude-sonnet-5-5")
	h.mod(hello(nil))
	events := h.subscribe("")
	h.nextCommand() // viewers

	code, body := h.post("/v1/sessions/task-1/model", map[string]any{"model": "anthropic/claude-sonnet-5-5", "expectedModel": "anthropic/claude-opus-5-5"})

	if code != http.StatusOK || !strings.Contains(body, `"to":"anthropic/claude-sonnet-5-5"`) {
		t.Fatalf("response = %d %s", code, body)
	}
	// The mod applies it to each request it sees; nothing is saved to the
	// person's Claude Code settings.
	if command := h.nextCommand(); command["type"] != "model" || command["model"] != "claude-sonnet-5-5" {
		t.Fatalf("command = %v", command)
	}
	if event := nextEvent(t, events); event["type"] != "model_changed" || event["to"] != "anthropic/claude-sonnet-5-5" {
		t.Fatalf("event = %v", event)
	}
	if model := h.snapshot().Model.ModelID; model != "claude-sonnet-5-5" {
		t.Fatalf("snapshot model = %q", model)
	}
}

func TestChoosingTheSessionsOwnModelRemovesTheOverride(t *testing.T) {
	h := newModelHarness(t, "claude-sonnet-5-5")
	h.mod(hello(nil))

	// Picking the default entry and an explicit reset both clear it.
	for _, reset := range []any{"anthropic/claude-opus-5-5", nil} {
		h.post("/v1/sessions/task-1/model", map[string]any{"model": "anthropic/claude-sonnet-5-5"})
		h.nextCommand()

		code, _ := h.post("/v1/sessions/task-1/model", map[string]any{"model": reset})

		if code != http.StatusOK {
			t.Fatalf("reset to %v = %d", reset, code)
		}
		if command := h.nextCommand(); command["type"] != "model" || command["model"] != nil {
			t.Fatalf("reset to %v: command = %v, want the override cleared", reset, command)
		}
		if model := h.snapshot().Model.ModelID; model != "claude-opus-5-5" {
			t.Fatalf("reset to %v: snapshot model = %q", reset, model)
		}
	}
}

func TestAModelTypedByIdIsOfferedOnceItIsInUse(t *testing.T) {
	h := newModelHarness(t)
	h.mod(hello(nil))

	// `/model <id>` in the composer names a model the list did not offer.
	h.post("/v1/sessions/task-1/model", map[string]any{"model": "claude-fable-5-1"})

	models := h.models()
	if len(models) != 2 || models[1].Ref != "anthropic/claude-fable-5-1" {
		t.Fatalf("models = %+v, want the model in use listed so the panel can mark it", models)
	}
}

func TestAStaleModelChoiceIsRefused(t *testing.T) {
	h := newModelHarness(t, "claude-sonnet-5-5")
	h.mod(hello(nil))

	code, body := h.post("/v1/sessions/task-1/model", map[string]any{"model": "anthropic/claude-sonnet-5-5", "expectedModel": "anthropic/claude-haiku-5-5"})

	if code != http.StatusConflict || !strings.Contains(body, "error") {
		t.Fatalf("response = %d %s", code, body)
	}
}

func TestEffortIsASettingOfTheSessionsModel(t *testing.T) {
	h := newModelHarness(t)
	h.mod(hello(nil), map[string]any{"type": "settings", "effort": "medium"})

	state := h.settings()

	if state.Model != "anthropic/claude-opus-5-5" || state.Capability.SchemaVersion != 1 || state.Capability.ModelID != "claude-opus-5-5" {
		t.Fatalf("state = %+v", state)
	}
	effort := state.Capability.Settings["reasoningEffort"]
	if effort.Status != "supported" || effort.Type != "enum" || strings.Join(effort.Values, ",") != "low,medium,high,xhigh,max" || effort.Default != "medium" {
		t.Fatalf("effort capability = %+v", effort)
	}
	if state.Defaults["reasoningEffort"] != "medium" || state.Effective["reasoningEffort"] != "medium" || len(state.Overrides) != 0 {
		t.Fatalf("defaults %v effective %v overrides %v", state.Defaults, state.Effective, state.Overrides)
	}
}

func TestChoosingAnEffortOverridesItForThisSessionOnly(t *testing.T) {
	h := newModelHarness(t)
	h.mod(hello(nil), map[string]any{"type": "settings", "effort": "medium"})
	events := h.subscribe("")
	h.nextCommand() // viewers

	code, body := h.post("/v1/sessions/task-1/model", map[string]any{
		"expectedModel": "anthropic/claude-opus-5-5", "settings": map[string]any{"reasoningEffort": "xhigh"},
	})

	if code != http.StatusOK || !strings.Contains(body, `"settingsState"`) {
		t.Fatalf("response = %d %s", code, body)
	}
	if command := h.nextCommand(); command["type"] != "effort" || command["effort"] != "xhigh" {
		t.Fatalf("command = %v", command)
	}
	changed := nextEvent(t, events)
	if changed["type"] != "model_settings_changed" {
		t.Fatalf("event = %v", changed)
	}
	state := h.settings()
	if state.Overrides["reasoningEffort"] != "xhigh" || state.Effective["reasoningEffort"] != "xhigh" || state.Defaults["reasoningEffort"] != "medium" {
		t.Fatalf("overrides %v effective %v defaults %v", state.Overrides, state.Effective, state.Defaults)
	}

	// "Reset to defaults" and an explicit null both clear it.
	for _, clear := range []map[string]any{
		{"resetSettings": true, "settings": map[string]any{}},
		{"settings": map[string]any{"reasoningEffort": nil}},
	} {
		h.post("/v1/sessions/task-1/model", map[string]any{"settings": map[string]any{"reasoningEffort": "low"}})
		h.nextCommand()
		h.post("/v1/sessions/task-1/model", clear)
		if command := h.nextCommand(); command["type"] != "effort" || command["effort"] != nil {
			t.Fatalf("clear with %v: command = %v", clear, command)
		}
		if state := h.settings(); len(state.Overrides) != 0 || state.Effective["reasoningEffort"] != "medium" {
			t.Fatalf("clear with %v: state = %+v", clear, state)
		}
	}
}

func TestAnUnknownEffortLevelIsRefused(t *testing.T) {
	h := newModelHarness(t)
	h.mod(hello(nil))

	code, body := h.post("/v1/sessions/task-1/model", map[string]any{"settings": map[string]any{"reasoningEffort": "ludicrous"}})

	if code != http.StatusBadRequest || !strings.Contains(body, "error") {
		t.Fatalf("response = %d %s", code, body)
	}
}

func TestTheSessionsOwnModelChangeKeepsAnOverrideInForce(t *testing.T) {
	h := newModelHarness(t)
	h.mod(hello(nil))
	h.post("/v1/sessions/task-1/model", map[string]any{"model": "claude-sonnet-5-5"})

	// The person ran /model in the terminal: the session's own model moves,
	// but requests still go to the model chosen in Legato.
	h.mod(map[string]any{"type": "model", "model": "claude-haiku-5-5"})

	if model := h.snapshot().Model.ModelID; model != "claude-sonnet-5-5" {
		t.Fatalf("snapshot model = %q", model)
	}
	models := h.models()
	if models[0].Ref != "anthropic/claude-haiku-5-5" || !models[0].IsDefault {
		t.Fatalf("models = %+v, want the session's new model as the default", models)
	}
}
