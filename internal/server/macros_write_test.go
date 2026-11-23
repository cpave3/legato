package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// macroServer serves the macro routes from a real MacroService over SQLite.
func macroServer(t *testing.T, file string) *Server {
	t.Helper()
	srv := settingsServer(t, file)
	srv.SetMacroService(service.NewMacroService(srv.settingsRegistry.(*service.SettingsService)))
	return srv
}

type macroListBody struct {
	Macros []service.MacroEntry            `json:"macros"`
	Tiers  map[string][]service.AgentTier `json:"tiers"`
}

func listMacros(t *testing.T, srv *Server) macroListBody {
	t.Helper()
	rec := callUpdates(srv, http.MethodGet, "/api/macros", "")
	var body macroListBody
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("GET /api/macros: %d %s", rec.Code, rec.Body)
	}
	return body
}

func macroNamed(body macroListBody, name string) *service.MacroEntry {
	for i := range body.Macros {
		if body.Macros[i].Name == name {
			return &body.Macros[i]
		}
	}
	return nil
}

func TestAMacroIsSavedRenamedAndDeletedThroughTheAPI(t *testing.T) {
	srv := macroServer(t, "")
	if rec := callUpdates(srv, http.MethodPut, "/api/macros/Deep%20review", `{"message":"review deeply","applies_to":"pr","agent":"codex"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	saved := macroNamed(listMacros(t, srv), "Deep review")
	if saved == nil || saved.Keys != "review deeply" || saved.AppliesTo != "pr" || saved.Agent != "codex" || saved.Source != service.MacroFromDatabase {
		t.Fatalf("saved = %+v", saved)
	}
	if rec := callUpdates(srv, http.MethodPost, "/api/macros/Deep%20review/rename", `{"name":"Thorough review"}`); rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	body := listMacros(t, srv)
	if macroNamed(body, "Deep review") != nil || macroNamed(body, "Thorough review") == nil {
		t.Fatalf("after rename: %+v", body.Macros)
	}
	if rec := callUpdates(srv, http.MethodDelete, "/api/macros/Thorough%20review", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body)
	}
	if macroNamed(listMacros(t, srv), "Thorough review") != nil {
		t.Fatal("deleted macro still listed")
	}
}

func TestAnEditedBuiltInIsResetThroughTheAPI(t *testing.T) {
	srv := macroServer(t, "")
	if rec := callUpdates(srv, http.MethodPut, "/api/macros/Plan", `{"keys":"my plan","applies_to":"task"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	if plan := macroNamed(listMacros(t, srv), "Plan"); plan == nil || !plan.Overridden || plan.Keys != "my plan" {
		t.Fatalf("edited Plan = %+v", plan)
	}
	if rec := callUpdates(srv, http.MethodPost, "/api/macros/Plan/reset", ""); rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	if plan := macroNamed(listMacros(t, srv), "Plan"); plan == nil || plan.Overridden || plan.Keys == "my plan" {
		t.Fatalf("reset Plan = %+v", plan)
	}
}

func TestMacroWritesTheAPIRefuses(t *testing.T) {
	srv := macroServer(t, "macros:\n  - name: fromfile\n    keys: hi\n")
	callUpdates(srv, http.MethodPut, "/api/macros/Mine", `{"message":"text"}`)
	for _, tc := range []struct {
		name, method, path, body string
		want                     int
		says                     string
	}{
		{"no message", http.MethodPut, "/api/macros/Empty", `{"applies_to":"task"}`, http.StatusBadRequest, "needs text"},
		{"bad applies_to", http.MethodPut, "/api/macros/Odd", `{"message":"x","applies_to":"board"}`, http.StatusBadRequest, "applies_to"},
		{"a config.yaml macro", http.MethodPut, "/api/macros/fromfile", `{"message":"x"}`, http.StatusConflict, "config.yaml"},
		{"deleting a built-in", http.MethodDelete, "/api/macros/Review", "", http.StatusConflict, "reset it instead"},
		{"resetting yours", http.MethodPost, "/api/macros/Mine/reset", "", http.StatusConflict, "only a built-in"},
		{"renaming a built-in", http.MethodPost, "/api/macros/Plan/rename", `{"name":"Mine 2"}`, http.StatusConflict, "cannot change"},
		{"renaming to a name in use", http.MethodPost, "/api/macros/Mine/rename", `{"name":"review"}`, http.StatusConflict, "another macro"},
		{"renaming a missing macro", http.MethodPost, "/api/macros/Nope/rename", `{"name":"Other"}`, http.StatusNotFound, "no macro"},
		{"deleting a missing macro", http.MethodDelete, "/api/macros/Nope", "", http.StatusNotFound, "no macro"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := callUpdates(srv, tc.method, tc.path, tc.body)
			if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.says) {
				t.Fatalf("%s %s: %d %s, want %d saying %q", tc.method, tc.path, rec.Code, rec.Body, tc.want, tc.says)
			}
		})
	}
}

func TestTheMacroListGivesEachAgentsTiers(t *testing.T) {
	srv := macroServer(t, "adapters:\n  claude-code:\n    tiers:\n      large: {description: Opus, launch_args: [--model, opus]}\n")
	body := listMacros(t, srv)
	if got := body.Tiers["claude-code"]; len(got) != 1 || got[0].Name != "large" || got[0].Description != "Opus" {
		t.Fatalf("tiers = %+v", body.Tiers)
	}
}

func TestAMacroIsStarredThroughTheAPI(t *testing.T) {
	srv := macroServer(t, "macros:\n  - name: fromfile\n    keys: hello\n")
	shipped := macroNamed(listMacros(t, srv), "Plan").Keys
	if rec := callUpdates(srv, http.MethodPut, "/api/macros/Plan", `{"favorite":true}`); rec.Code != http.StatusOK {
		t.Fatalf("star Plan: %d %s", rec.Code, rec.Body)
	}
	if plan := macroNamed(listMacros(t, srv), "Plan"); !plan.Favorite || plan.Keys != shipped || plan.Overridden {
		t.Fatalf("starred Plan = %+v, want the shipped text, starred, not edited", plan)
	}
	if rec := callUpdates(srv, http.MethodPut, "/api/macros/Plan", `{"favorite":false}`); rec.Code != http.StatusOK {
		t.Fatalf("unstar Plan: %d %s", rec.Code, rec.Body)
	}
	if plan := macroNamed(listMacros(t, srv), "Plan"); plan.Favorite {
		t.Fatalf("unstarred Plan = %+v", plan)
	}

	if rec := callUpdates(srv, http.MethodPut, "/api/macros/Ship", `{"message":"ship it","favorite":true}`); rec.Code != http.StatusOK {
		t.Fatalf("new starred macro: %d %s", rec.Code, rec.Body)
	}
	if ship := macroNamed(listMacros(t, srv), "Ship"); ship == nil || !ship.Favorite || ship.Keys != "ship it" {
		t.Fatalf("Ship = %+v, want it saved and starred", ship)
	}
	if rec := callUpdates(srv, http.MethodPut, "/api/macros/Ship", `{"message":"ship it now"}`); rec.Code != http.StatusOK {
		t.Fatalf("edit Ship: %d %s", rec.Code, rec.Body)
	}
	if ship := macroNamed(listMacros(t, srv), "Ship"); !ship.Favorite || ship.Keys != "ship it now" {
		t.Fatalf("edited Ship = %+v, want the new text, still starred", ship)
	}

	if rec := callUpdates(srv, http.MethodPut, "/api/macros/fromfile", `{"favorite":true}`); rec.Code != http.StatusConflict {
		t.Fatalf("star a config.yaml macro: %d %s, want 409", rec.Code, rec.Body)
	}
	if rec := callUpdates(srv, http.MethodPut, "/api/macros/nothing", `{"favorite":true}`); rec.Code != http.StatusNotFound {
		t.Fatalf("star a macro that does not exist: %d %s, want 404", rec.Code, rec.Body)
	}
}
