package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type compactAgentFake struct {
	service.AgentService
	compacted    string
	instructions string
	err          error
}

func (f *compactAgentFake) CompactAgent(_ context.Context, taskID, instructions string) error {
	if f.err != nil {
		return f.err
	}
	f.compacted, f.instructions = taskID, instructions
	return nil
}

func TestCompactingAnAgentPassesItsFocusOn(t *testing.T) {
	agents := &compactAgentFake{}

	status := serveJSON(t, New(nil, agents, nil, ":0"), http.MethodPost, "/api/agents/T-1/compact", `{"instructions":"keep the plan"}`, nil)

	if status != http.StatusAccepted || agents.compacted != "T-1" || agents.instructions != "keep the plan" {
		t.Fatalf("status = %d, compacted %q with %q", status, agents.compacted, agents.instructions)
	}
}

func TestCompactingWithNoBodyHasNoFocus(t *testing.T) {
	agents := &compactAgentFake{}

	status := serveJSON(t, New(nil, agents, nil, ":0"), http.MethodPost, "/api/agents/T-1/compact", "", nil)

	if status != http.StatusAccepted || agents.compacted != "T-1" || agents.instructions != "" {
		t.Fatalf("status = %d, compacted %q with %q", status, agents.compacted, agents.instructions)
	}
}

// A refusal says why, in words the panel and the Reeve show as they are.
func TestARefusedCompactionSaysWhy(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"working", fmt.Errorf("%w: busy", service.ErrAgentWorking), http.StatusConflict},
		{"unsupported", fmt.Errorf("%w: terminal only", service.ErrCompactUnsupported), http.StatusUnprocessableEntity},
		{"focus", fmt.Errorf("%w: Codex compacts with its own prompt", service.ErrCompactFocusUnsupported), http.StatusUnprocessableEntity},
	} {
		t.Run(c.name, func(t *testing.T) {
			var failure struct{ Error string }

			status := serveJSON(t, New(nil, &compactAgentFake{err: c.err}, nil, ":0"), http.MethodPost, "/api/agents/T-1/compact", "", &failure)

			if status != c.want || failure.Error != c.err.Error() {
				t.Fatalf("status = %d, body %+v; want %d with %q", status, failure, c.want, c.err.Error())
			}
		})
	}
}

// Codex and Chimera list no compact of their own; Legato offers it for
// every agent it can compact, once.
func TestTheSkillListOffersCompactOnce(t *testing.T) {
	for _, c := range []struct {
		kind, listed string
		want         []string
	}{
		{"codex", `[{"name":"review"}]`, []string{"review", "compact"}},
		{"chimera", `[]`, []string{"compact"}},
		{"claude-code", `[{"name":"compact","source":"builtin"}]`, []string{"compact"}},
		{"aider", `[]`, []string{}},
	} {
		t.Run(c.kind, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(c.listed))
			}))
			defer endpoint.Close()
			agents := &chimeraModelsAgentFake{endpoint: true, session: &store.AgentSession{
				AgentKind: c.kind, ChimeraEndpoint: strPtr(endpoint.URL), ChimeraSessionID: strPtr("s1"),
			}}
			var skills []struct{ Name string }

			status := serveJSON(t, New(nil, agents, nil, ":0"), http.MethodGet, "/api/agents/T-1/chimera/skills", "", &skills)

			names := []string{}
			for _, skill := range skills {
				names = append(names, skill.Name)
			}
			if status != http.StatusOK || strings.Join(names, ",") != strings.Join(c.want, ",") {
				t.Fatalf("status = %d, skills = %v, want %v", status, names, c.want)
			}
		})
	}
}
