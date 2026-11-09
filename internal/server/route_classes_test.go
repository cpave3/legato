package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every route the server serves has a class for each method it takes.
func TestEveryRouteHasAClass(t *testing.T) {
	s := New(nil, nil, nil, ":0")
	classed := map[string]bool{}
	for _, e := range routeClassTable {
		classed[e.Pattern] = true
		if e.Class != ClassRead && e.Class != ClassChange && e.Class != ClassRisky && e.Class != ClassNever {
			t.Errorf("%s %s: class %q", e.Method, e.Pattern, e.Class)
		}
		if strings.TrimSpace(e.Description) == "" {
			t.Errorf("%s %s has no description", e.Method, e.Pattern)
		}
	}
	registered := map[string]bool{}
	for _, p := range s.routes {
		pattern := p
		if method, rest, ok := strings.Cut(p, " "); ok && method == strings.ToUpper(method) {
			pattern = rest
			if !hasEntry(method, pattern) {
				t.Errorf("%s has no class: add it to routeClassTable", p)
			}
		}
		registered[pattern] = true
		if !classed[pattern] {
			t.Errorf("%s has no class: add each method it takes to routeClassTable", p)
		}
	}
	for _, e := range routeClassTable {
		if !registered[e.Pattern] {
			t.Errorf("%s %s is classed but not served", e.Method, e.Pattern)
		}
	}
}

func hasEntry(method, pattern string) bool {
	for _, e := range routeClassTable {
		if e.Method == method && e.Pattern == pattern {
			return true
		}
	}
	return false
}

func TestARequestIsClassedByItsRoute(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         RouteClass
	}{
		{http.MethodGet, "/api/agents", ClassRead},
		{http.MethodPost, "/api/tasks", ClassChange},
		{http.MethodPost, "/api/restart", ClassRisky},
		{http.MethodPost, "/api/approvals/ap-1/decision", ClassNever},
		{http.MethodPost, "/api/agents/T-1/chimera/permission", ClassNever},
		{http.MethodGet, "/api/no-such-route", ClassRisky},
		{http.MethodPatch, "/api/agents", ClassRisky},
	} {
		got := ClassifyRequest(httptest.NewRequest(tc.method, tc.path, nil))
		if got.Class != tc.want {
			t.Errorf("%s %s = %+v, want %s", tc.method, tc.path, got, tc.want)
		}
	}
}

// The settings routes take their classes from the registry.
func TestSettingsRoutesTakeTheirClassFromTheRegistry(t *testing.T) {
	if got := ClassifyRequest(httptest.NewRequest(http.MethodPut, "/api/config/settings/agents.sandbox.enabled", nil)); got.Class != ClassRisky || !strings.Contains(got.Description, "agents.sandbox.enabled") {
		t.Errorf("sandbox setting = %+v", got)
	}
	if got := ClassifyRequest(httptest.NewRequest(http.MethodPut, "/api/config/settings/theme", nil)); got.Class != ClassChange {
		t.Errorf("theme = %+v", got)
	}
	if got := ClassifyRequest(httptest.NewRequest(http.MethodPut, "/api/config/lists/credentials/gh", nil)); got.Class != ClassRisky {
		t.Errorf("credential entry = %+v", got)
	}
	if got := ClassifyRequest(httptest.NewRequest(http.MethodPut, "/api/config/lists/macros/hi", nil)); got.Class != ClassChange {
		t.Errorf("macro entry = %+v", got)
	}
}
