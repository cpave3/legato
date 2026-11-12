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
		{http.MethodGet, "/api/no-such-route", ClassRead},
		{http.MethodPost, "/api/no-such-route", ClassRisky},
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

// A GET on a route Legato does not serve reads nothing: it answers 404, so
// the Reeve learns that at once instead of asking the operator to approve
// it. These are routes the Reeve guessed in a session.
func TestAGETOnARouteLegatoDoesNotServeIsARead(t *testing.T) {
	for _, path := range []string{"/api/handoffs", "/api/columns", "/api/reeve/change-sets/cs-1"} {
		got := ClassifyRequest(httptest.NewRequest(http.MethodGet, path, nil))
		if got.Class != ClassRead || got.Known {
			t.Errorf("GET %s = %+v, want an unknown read", path, got)
		}
	}
}

// A GET on a path Legato serves for other methods only is not a read: its
// handler, not the table, would decide what a GET does there.
func TestAGETOnARouteServedForOtherMethodsIsNotARead(t *testing.T) {
	for _, path := range []string{"/api/agents/T-1/chimera/send", "/api/agents/kill", "/api/restart"} {
		if got := ClassifyRequest(httptest.NewRequest(http.MethodGet, path, nil)); got.Class == ClassRead {
			t.Errorf("GET %s = %+v, want it refused or approved", path, got)
		}
	}
}

// The GETs the Reeve made in a session all read.
func TestTheReevesEverydayReadsAreReads(t *testing.T) {
	for _, path := range []string{"/api/tasks/T-1", "/api/agents", "/api/board", "/api/reeve/sessions/T-1/change-sets", "/api/approvals/ap-1", "/api/credentials"} {
		if got := ClassifyRequest(httptest.NewRequest(http.MethodGet, path, nil)); got.Class != ClassRead || !got.Known {
			t.Errorf("GET %s = %+v, want a known read", path, got)
		}
	}
}

// getNotRead lists every GET route that is not a read, on purpose, and why.
// A GET classed anything else by mistake fails the test below.
var getNotRead = map[string]string{
	"/api/pairing":                   "shows the pairing phrase, which hands out a token",
	"/api/agents/{task_id}/terminal": "a socket that types into an agent's terminal",
	"/ws":                            "the live connection that can type into agents",
	"/api/reeve/voice/session":       "the person's own spoken conversation",
	"/oauth/callback":                "finishes connecting an integration",
}

func TestEveryGETIsAReadUnlessListedOnPurpose(t *testing.T) {
	for _, e := range routeClassTable {
		if e.Method != http.MethodGet {
			continue
		}
		_, listed := getNotRead[e.Pattern]
		if e.Class != ClassRead && !listed {
			t.Errorf("GET %s is %s: a GET reads, unless it is listed in getNotRead with why", e.Pattern, e.Class)
		}
		if e.Class == ClassRead && listed {
			t.Errorf("GET %s reads but is listed in getNotRead", e.Pattern)
		}
	}
}

// Every route the server serves answers a GET with a class from the table,
// or is never a read: a served route the table has no GET for must not
// pass as one Legato does not serve.
func TestEveryServedRouteHasAClassForGET(t *testing.T) {
	s := New(nil, nil, nil, ":0")
	for _, p := range s.routes {
		method, pattern, ok := strings.Cut(p, " ")
		if !ok || method != strings.ToUpper(method) {
			method, pattern = "", p
		}
		if method != "" && method != http.MethodGet {
			continue
		}
		path := samplePath(pattern)
		got := ClassifyRequest(httptest.NewRequest(http.MethodGet, path, nil))
		if !got.Known && got.Class == ClassRead && pattern != "/api/" {
			t.Errorf("GET %s (%s) passes as a route Legato does not serve: class its GET in routeClassTable", path, p)
		}
	}
}

// samplePath is a path a pattern matches: each {name} becomes x.
func samplePath(pattern string) string {
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") {
			parts[i] = "x"
		}
	}
	return strings.Join(parts, "/")
}
