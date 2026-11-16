package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func skillsServer(t *testing.T) (*Server, string) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dir := filepath.Join(t.TempDir(), "skills")
	srv := New(nil, nil, nil, ":0")
	srv.SetSkillService(service.NewSkillService(dir, db))
	return srv, dir
}

func dropSkill(t *testing.T, dir, name, skillMD string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
}

type skillList struct {
	Dir    string          `json:"dir"`
	Skills []service.Skill `json:"skills"`
}

func TestSkillsAreListedAndTurnedOnAndOff(t *testing.T) {
	srv, dir := skillsServer(t)
	const path = "/api/settings/skills"

	var listed skillList
	if status := serveJSON(t, srv, http.MethodGet, path, "", &listed); status != http.StatusOK || listed.Dir != dir || len(listed.Skills) != 0 {
		t.Fatalf("empty list: %d %+v", status, listed)
	}

	// A folder dropped in shows at the next ask, a broken one with its error.
	dropSkill(t, dir, "tdd", "---\nname: tdd\ndescription: Tests first.\n---\n")
	dropSkill(t, dir, "broken", "no frontmatter\n")
	if status := serveJSON(t, srv, http.MethodGet, path, "", &listed); status != http.StatusOK || len(listed.Skills) != 2 {
		t.Fatalf("list: %d %+v", status, listed)
	}
	broken, tdd := listed.Skills[0], listed.Skills[1]
	if broken.Name != "broken" || broken.Err == "" {
		t.Fatalf("broken = %+v, want its error", broken)
	}
	if tdd.Name != "tdd" || tdd.Description != "Tests first." || tdd.Path != filepath.Join(dir, "tdd") || !tdd.Enabled || tdd.Err != "" {
		t.Fatalf("tdd = %+v", tdd)
	}

	var changed service.Skill
	if status := serveJSON(t, srv, http.MethodPut, path+"/tdd", `{"enabled":false}`, &changed); status != http.StatusOK || changed.Name != "tdd" || changed.Enabled {
		t.Fatalf("turning tdd off: %d %+v", status, changed)
	}
	serveJSON(t, srv, http.MethodGet, path, "", &listed)
	if listed.Skills[1].Enabled {
		t.Fatalf("tdd still on: %+v", listed.Skills[1])
	}
}

func TestTurningASkillOnOrOffIsRefusedWhenItCannotBe(t *testing.T) {
	srv, dir := skillsServer(t)
	dropSkill(t, dir, "tdd", "---\nname: tdd\ndescription: Tests first.\n---\n")
	for _, tc := range []struct {
		name, path, body string
		want             int
	}{
		{"unknown skill", "/api/settings/skills/missing", `{"enabled":true}`, http.StatusNotFound},
		{"no state", "/api/settings/skills/tdd", `{}`, http.StatusBadRequest},
		{"not JSON", "/api/settings/skills/tdd", `on`, http.StatusBadRequest},
	} {
		var out map[string]any
		if status := serveJSON(t, srv, http.MethodPut, tc.path, tc.body, &out); status != tc.want {
			t.Errorf("%s: %d %v, want %d", tc.name, status, out, tc.want)
		}
	}

	bare := New(nil, nil, nil, ":0")
	var out map[string]any
	if status := serveJSON(t, bare, http.MethodGet, "/api/settings/skills", "", &out); status != http.StatusServiceUnavailable {
		t.Fatalf("without a skill service: %d", status)
	}
}

func TestTurningASkillOnOrOffIsAChangeTheReeveMustPropose(t *testing.T) {
	for method, want := range map[string]RouteClass{http.MethodGet: ClassRead, http.MethodPut: ClassChange} {
		req, _ := http.NewRequest(method, "/api/settings/skills/tdd", nil)
		if method == http.MethodGet {
			req, _ = http.NewRequest(method, "/api/settings/skills", nil)
		}
		if got := ClassifyRequest(req); !got.Known || got.Class != want {
			t.Errorf("%s: class %v, want %v", method, got, want)
		}
	}
}
