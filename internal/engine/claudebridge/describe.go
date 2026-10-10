package claudebridge

import (
	"net/http"
	"os"
)

// protocolVersion names the bridge to Legato in GET /v1/instance.
const protocolVersion = "legato-claude-bridge/1"

// registerDescriptionRoutes serves the read-only parts of the native protocol
// the panel asks for beside the snapshot. Claude Code has no counterpart for
// some of them (Chimera modes, prompt-template commands); those answer an
// empty list so the panel hides the control rather than failing.
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
		writeJSON(w, http.StatusOK, map[string]any{"contextWindow": usage.Window, "lastPromptTokens": usage.Tokens})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/models", b.handleModels)
	mux.HandleFunc("GET /v1/sessions/{id}/model", b.handleModelSettings)
	mux.HandleFunc("POST /v1/sessions/{id}/model", b.handleModelChange)
	// The panel offers these as slash suggestions that fill the composer, so
	// the person completes the arguments before the command is sent.
	mux.HandleFunc("GET /v1/sessions/{id}/skills", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		commands := b.session.commands
		b.mu.Unlock()
		skills := make([]map[string]any, 0, len(commands))
		for _, command := range commands {
			skills = append(skills, map[string]any{
				"name": command.Name, "description": command.Description, "path": "", "source": command.Source,
			})
		}
		writeJSON(w, http.StatusOK, skills)
	})
	for _, list := range []string{"modes", "commands", "subagents"} {
		mux.HandleFunc("GET /v1/sessions/{id}/"+list, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, []any{})
		})
	}
	mux.HandleFunc("POST /v1/sessions/{id}/mode", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Claude Code sessions have no Legato-selectable modes."})
	})
	// Chimera grants sandbox access per attached path. Claude Code decides
	// file access by its own permission rules, so there is nothing to grant.
	mux.HandleFunc("POST /v1/sessions/{id}/paths", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}
