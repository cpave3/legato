package server

import (
	"net/http"
	"strings"
	"sync"

	"github.com/cpave3/legato/config"
)

// RouteClass is what a call to a route needs when an agent makes it (the
// Reeve): read is free, change needs an approval, risky can need a signed
// one, never is refused whatever the approval. See docs/ai/reeve.md.
type RouteClass string

const (
	ClassRead   RouteClass = "read"
	ClassChange RouteClass = "change"
	ClassRisky  RouteClass = "risky"
	ClassNever  RouteClass = "never"
)

// routeClassEntry classes one method of one route, with a plain description
// for approval cards.
type routeClassEntry struct {
	Method      string
	Pattern     string
	Class       RouteClass
	Description string
}

// RouteInfo is the class of a request and what it does.
type RouteInfo struct {
	Method      string     `json:"method"`
	Pattern     string     `json:"pattern"`
	Class       RouteClass `json:"class"`
	Description string     `json:"description"`
	// Known is false for a route the table does not class: it counts as
	// risky.
	Known bool `json:"known"`
}

var (
	classMuxOnce sync.Once
	classMux     *http.ServeMux
	classByKey   map[string]routeClassEntry
)

// buildClassMux registers every entry as "METHOD pattern" on a ServeMux of
// its own, so a request is matched to its entry the way the server's mux
// matches it to its handler.
func buildClassMux() {
	classMux = http.NewServeMux()
	classByKey = map[string]routeClassEntry{}
	noop := func(http.ResponseWriter, *http.Request) {}
	for _, e := range routeClassTable {
		key := e.Method + " " + e.Pattern
		if _, dup := classByKey[key]; dup {
			continue
		}
		classByKey[key] = e
		classMux.HandleFunc(key, noop)
	}
}

// ClassifyRequest is the class of a request: its route's entry in the
// table, the settings registry's class for a setting or a list, and risky
// for a route the table does not class.
func ClassifyRequest(r *http.Request) RouteInfo {
	classMuxOnce.Do(buildClassMux)
	_, pattern := classMux.Handler(r)
	e, ok := classByKey[pattern]
	// The web UI's catch-all serves pages, never an API call.
	if ok && e.Pattern == "/" && strings.HasPrefix(r.URL.Path, "/api/") {
		ok = false
	}
	// The API's catch-all answers 404: a GET there reads nothing, unless
	// the path is a route served for other methods, whose handler would
	// decide what a GET does.
	if ok && e.Pattern == notServedPattern {
		ok = false
	}
	if !ok || pattern == "" {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && !servedForAnotherMethod(r) {
			return RouteInfo{Method: r.Method, Pattern: r.URL.Path, Class: ClassRead, Description: "Read a route Legato does not serve (it answers 404)", Known: false}
		}
		return RouteInfo{Method: r.Method, Pattern: r.URL.Path, Class: ClassRisky, Description: "A call Legato has no class for", Known: false}
	}
	info := RouteInfo{Method: e.Method, Pattern: e.Pattern, Class: e.Class, Description: e.Description, Known: true}
	if e.Method != http.MethodGet {
		switch e.Pattern {
		case "/api/config/settings/{key}":
			if s, found := config.LookupSetting(r.PathValue("key")); found {
				info.Class = RouteClass(s.Class)
				info.Description = settingVerb(r.Method) + " the setting " + s.Key + " (" + s.Title + ")"
			} else if v := pathTail(r.URL.Path, "/api/config/settings/"); v != "" {
				if s, found := config.LookupSetting(v); found {
					info.Class = RouteClass(s.Class)
					info.Description = settingVerb(r.Method) + " the setting " + s.Key + " (" + s.Title + ")"
				}
			}
		case "/api/config/lists/{list}/{name}":
			parts := strings.SplitN(pathTail(r.URL.Path, "/api/config/lists/"), "/", 2)
			if l, found := config.LookupEntryList(parts[0]); found {
				info.Class = RouteClass(l.Class)
				info.Description = settingVerb(r.Method) + " an entry of " + l.Title
			}
		}
	}
	return info
}

// notServedPattern is the API's catch-all, which answers 404 for a route
// Legato does not serve (notServedHandler).
const notServedPattern = "/api/"

// servedForAnotherMethod reports whether the table classes r's path for a
// method other than GET.
func servedForAnotherMethod(r *http.Request) bool {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		probe := r.Clone(r.Context())
		probe.Method = method
		if _, pattern := classMux.Handler(probe); pattern != "" {
			return true
		}
	}
	return false
}

func settingVerb(method string) string {
	if method == http.MethodDelete {
		return "Reset"
	}
	return "Change"
}

func pathTail(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	return strings.TrimPrefix(path, prefix)
}

// RouteClasses lists the table, for the Reeve's view of the API.
func RouteClasses() []RouteInfo {
	out := make([]RouteInfo, 0, len(routeClassTable))
	for _, e := range routeClassTable {
		out = append(out, RouteInfo{Method: e.Method, Pattern: e.Pattern, Class: e.Class, Description: e.Description, Known: true})
	}
	return out
}
