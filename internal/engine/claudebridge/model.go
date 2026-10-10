package claudebridge

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

// effortLevels are the efforts a Claude Code request can ask for, lowest first.
var effortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// effortSetting is the name the native panel's model settings use for effort.
const effortSetting = "reasoningEffort"

// modelSettingsState is the model settings contract the native panel reads
// and writes: what is chosen, what the session would use on its own, the
// result of the two, and which settings can be adjusted at all.
type modelSettingsState struct {
	Model        string            `json:"model"`
	Overrides    map[string]any    `json:"overrides"`
	Defaults     map[string]any    `json:"defaults"`
	Effective    map[string]any    `json:"effective"`
	Capabilities modelCapabilities `json:"capabilities"`
}

type modelCapabilities struct {
	SchemaVersion int                          `json:"schemaVersion"`
	ModelID       string                       `json:"modelId"`
	Profile       string                       `json:"profile"`
	Source        string                       `json:"source"`
	Fingerprint   string                       `json:"fingerprint"`
	Settings      map[string]settingCapability `json:"settings"`
}

type settingCapability struct {
	Status  string   `json:"status"`
	Type    string   `json:"type"`
	Values  []string `json:"values,omitempty"`
	Default any      `json:"default,omitempty"`
}

// settingsState describes the one setting a Claude Code session exposes:
// effort. The session's own effort is known once it has made a request.
func (s *session) settingsState() modelSettingsState {
	state := modelSettingsState{
		Model:     s.modelRef(),
		Overrides: map[string]any{},
		Defaults:  map[string]any{},
		Effective: map[string]any{},
		Capabilities: modelCapabilities{
			SchemaVersion: 1,
			ModelID:       s.effectiveModel(),
			Profile:       "claude-code",
			Source:        protocolVersion,
			Fingerprint:   s.effectiveModel(),
			Settings: map[string]settingCapability{
				effortSetting: {Status: "supported", Type: "enum", Values: effortLevels},
			},
		},
	}
	if s.effort != "" {
		state.Defaults[effortSetting] = s.effort
		capability := state.Capabilities.Settings[effortSetting]
		capability.Default = s.effort
		state.Capabilities.Settings[effortSetting] = capability
	}
	if s.effortOverride != "" {
		state.Overrides[effortSetting] = s.effortOverride
	}
	if effort := s.effectiveEffort(); effort != "" {
		state.Effective[effortSetting] = effort
	}
	return state
}

type modelListEntry struct {
	Ref           string `json:"ref"`
	IsDefault     bool   `json:"isDefault"`
	ContextWindow int    `json:"contextWindow,omitempty"`
}

// handleModels lists the models the panel offers: the session's own first,
// then the configured ones, then one in use that neither names.
func (b *Bridge) handleModels(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	own, inUse, window := b.session.model, b.session.effectiveModel(), b.session.context.Window
	b.mu.Unlock()

	entries := []modelListEntry{}
	listed := map[string]bool{}
	add := func(model string, isDefault bool) {
		if model == "" || listed[model] {
			return
		}
		listed[model] = true
		entry := modelListEntry{Ref: modelProvider + "/" + model, IsDefault: isDefault}
		if model == inUse {
			// Only the model in use has a window the session has measured.
			entry.ContextWindow = window
		}
		entries = append(entries, entry)
	}
	add(own, true)
	for _, model := range b.models {
		add(model, false)
	}
	add(inUse, false)
	writeJSON(w, http.StatusOK, entries)
}

func (b *Bridge) handleModelSettings(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	state := b.session.settingsState()
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, state)
}

// handleModelChange applies a model or effort chosen in Legato. Neither is
// written to Claude Code's settings: the mod rewrites each request the session
// makes, so the choice holds for this session and ends with it.
func (b *Bridge) handleModelChange(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid model settings JSON"})
		return
	}
	var expected string
	_ = json.Unmarshal(body["expectedModel"], &expected)
	var reset bool
	_ = json.Unmarshal(body["resetSettings"], &reset)

	b.mu.Lock()
	defer b.mu.Unlock()
	if expected != "" && expected != b.session.modelRef() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "The model changed. Reopen its settings."})
		return
	}

	model, effort := b.session.modelOverride, b.session.effortOverride
	if raw, chosen := body["model"]; chosen {
		var ref string
		if string(raw) != "null" {
			if err := json.Unmarshal(raw, &ref); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "model must be a string or null"})
				return
			}
		}
		model = strings.TrimPrefix(strings.TrimSpace(ref), modelProvider+"/")
		if model == b.session.model {
			model = ""
		}
	}
	if reset {
		effort = ""
	}
	if raw, present := body["settings"]; present {
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(raw, &settings); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "settings must be an object"})
			return
		}
		for name, value := range settings {
			if name != effortSetting {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": name + " cannot be adjusted for a Claude Code session"})
				return
			}
			effort = ""
			if string(value) != "null" {
				if err := json.Unmarshal(value, &effort); err != nil || !slices.Contains(effortLevels, effort) {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "effort must be one of " + strings.Join(effortLevels, ", ")})
					return
				}
			}
		}
	}

	modelChanged, effortChanged := model != b.session.modelOverride, effort != b.session.effortOverride
	if modelChanged {
		if err := b.send(map[string]any{"type": "model", "model": nullable(model)}); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		b.session.modelOverride = model
	}
	if effortChanged {
		if err := b.send(map[string]any{"type": "effort", "effort": nullable(effort)}); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		b.session.effortOverride = effort
	}
	switch {
	case modelChanged:
		b.publish(b.session.modelChanged())
	case effortChanged:
		b.publish([]nativeEvent{b.session.settingsChanged()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"to": b.session.modelRef(), "settingsState": b.session.settingsState()})
}

// nullable is the JSON form of a value that may be cleared: null when empty.
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
