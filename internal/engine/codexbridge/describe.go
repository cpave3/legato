package codexbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
)

// registerDescriptionRoutes serves the read-only parts of the native protocol
// the panel asks for beside the snapshot. Codex has no counterpart for some
// of them (Chimera modes, prompt-template commands); those answer an empty
// list so the panel hides the control rather than failing.
func (b *Bridge) registerDescriptionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/instance", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		cwd := b.session.cwd
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"pid": os.Getpid(), "cwd": cwd, "version": protocolVersion, "sandboxMode": ""})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/context", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		usage := b.session.context
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"contextWindow": usage.window, "lastPromptTokens": usage.tokens})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/models", b.handleModels)
	mux.HandleFunc("GET /v1/sessions/{id}/model", b.handleModelSettings)
	mux.HandleFunc("POST /v1/sessions/{id}/model", b.handleModelChange)
	for _, list := range []string{"skills", "modes", "commands", "subagents"} {
		mux.HandleFunc("GET /v1/sessions/{id}/"+list, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, []any{})
		})
	}
	mux.HandleFunc("POST /v1/sessions/{id}/mode", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Codex sessions have no Legato-selectable modes."})
	})
	// Chimera grants sandbox access per attached path. Codex decides file
	// access by its own sandbox and approvals, so there is nothing to grant.
	mux.HandleFunc("POST /v1/sessions/{id}/paths", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

// loadModels asks the app server which models it offers. The panel's selector
// works without the list, showing the thread's own model alone.
func (b *Bridge) loadModels(ctx context.Context) {
	var listed struct {
		Data []struct {
			ID        string `json:"id"`
			IsDefault bool   `json:"isDefault"`
			Hidden    bool   `json:"hidden"`
			Efforts   []struct {
				Level string `json:"reasoningEffort"`
			} `json:"supportedReasoningEfforts"`
		} `json:"data"`
	}
	if err := b.client.call(ctx, "model/list", map[string]any{}, &listed); err != nil {
		return
	}
	models := make([]modelInfo, 0, len(listed.Data))
	for _, entry := range listed.Data {
		if entry.Hidden || entry.ID == "" {
			continue
		}
		model := modelInfo{ID: entry.ID, IsDefault: entry.IsDefault}
		for _, effort := range entry.Efforts {
			model.Efforts = append(model.Efforts, effort.Level)
		}
		models = append(models, model)
	}
	b.mu.Lock()
	b.models = models
	b.mu.Unlock()
}

type modelListEntry struct {
	Ref           string `json:"ref"`
	IsDefault     bool   `json:"isDefault"`
	ContextWindow int    `json:"contextWindow,omitempty"`
}

// handleModels lists the models the panel offers: the thread's own first,
// which is the entry that clears a choice, then the rest Codex lists.
func (b *Bridge) handleModels(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	own, inUse, window := b.session.model, b.session.effectiveModel(), b.session.context.window
	models := b.models
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
			// Only the model in use has a window the thread has measured.
			entry.ContextWindow = window
		}
		entries = append(entries, entry)
	}
	add(own, true)
	for _, model := range models {
		add(model.ID, false)
	}
	add(inUse, false)
	writeJSON(w, http.StatusOK, entries)
}

func (b *Bridge) handleModelSettings(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	state := b.session.settingsState(b.models)
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, state)
}

// handleModelChange records a model or effort chosen in Legato. Codex takes
// both with a turn, so the choice goes with the next turn Legato starts and
// holds for the thread from then on; a turn typed at the terminal before
// that still uses the thread's own.
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
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": name + " cannot be adjusted for a Codex session"})
				return
			}
			effort = ""
			if string(value) == "null" {
				continue
			}
			if err := json.Unmarshal(value, &effort); err != nil || effort == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "effort must be a string or null"})
				return
			}
			chosen := model
			if chosen == "" {
				chosen = b.session.model
			}
			if levels := b.effortsOf(chosen); len(levels) > 0 && !slices.Contains(levels, effort) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "effort must be one of " + strings.Join(levels, ", ")})
				return
			}
		}
	}

	modelChanged, effortChanged := model != b.session.modelOverride, effort != b.session.effortOverride
	b.session.modelOverride, b.session.effortOverride = model, effort
	switch {
	case modelChanged:
		b.publish(b.modelChanged())
	case effortChanged:
		b.publish([]nativeEvent{b.settingsChanged()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"to": b.session.modelRef(), "settingsState": b.session.settingsState(b.models)})
}

// effortsOf lists the effort levels a model takes, when the server said.
// The caller holds b.mu.
func (b *Bridge) effortsOf(model string) []string {
	for _, listed := range b.models {
		if listed.ID == model {
			return listed.Efforts
		}
	}
	return nil
}

// modelChanged announces the model turns now go to, with its settings: the
// panel drops the settings it held when the model under them changes. The
// caller holds b.mu.
func (b *Bridge) modelChanged() []nativeEvent {
	return []nativeEvent{{"type": "model_changed", "to": b.session.modelRef()}, b.settingsChanged()}
}

func (b *Bridge) settingsChanged() nativeEvent {
	return nativeEvent{"type": "model_settings_changed", "settingsState": b.session.settingsState(b.models)}
}

// applySettings takes the thread's settings as Codex reports them after a
// change, whoever made it. A choice Codex has taken on is no longer pending.
// The caller holds b.mu.
func (b *Bridge) applySettings(model, effort, cwd string) []nativeEvent {
	s := b.session
	before, beforeEffort := s.modelRef(), s.effectiveEffort()
	if cwd != "" {
		s.cwd = cwd
	}
	if model != "" {
		s.model = model
	}
	s.effort = effort
	if s.modelOverride == s.model {
		s.modelOverride = ""
	}
	if s.effortOverride == s.effort {
		s.effortOverride = ""
	}
	switch {
	case s.modelRef() != before:
		return b.modelChanged()
	case s.effectiveEffort() != beforeEffort:
		return []nativeEvent{b.settingsChanged()}
	}
	return nil
}
