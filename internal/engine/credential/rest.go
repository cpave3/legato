package credential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxMeteredBody is the largest request body the rest inspector reads to
// meter it. A larger one is refused.
const MaxMeteredBody = 1 << 20

// ErrBodyTooLarge is a metered request body over MaxMeteredBody.
var ErrBodyTooLarge = errors.New("the request body is too large to meter (over 1 MiB)")

// Route classifies requests by method and path. Path segments are literal,
// {name} for one segment, or a last ** for the rest. Resource is a template
// of the names; without one the resource is the request path.
type Route struct {
	Method    string            `yaml:"method" json:"method"`
	Path      string            `yaml:"path" json:"path"`
	Operation string            `yaml:"operation" json:"operation"`
	Resource  string            `yaml:"resource" json:"resource,omitempty"`
	Meter     map[string]string `yaml:"meter" json:"meter,omitempty"`
}

// NoRouteError is a request no route matches.
type NoRouteError struct{ Method, Path string }

func (e *NoRouteError) Error() string {
	return fmt.Sprintf("no route of this credential allows %s %s", e.Method, e.Path)
}

// RestInspector classifies HTTP requests with a route table. Host is the
// upstream's host, the start of every resource.
type RestInspector struct {
	Host   string
	Routes []Route
}

// ValidateRoutes checks a route table as a profile gives it.
func ValidateRoutes(routes []Route) error {
	for i, r := range routes {
		if r.Operation == "" {
			return fmt.Errorf("routes[%d]: an operation is needed", i)
		}
		if !strings.HasPrefix(r.Path, "/") {
			return fmt.Errorf("routes[%d]: path %q must start with /", i, r.Path)
		}
		names := map[string]bool{}
		segments := strings.Split(strings.TrimPrefix(r.Path, "/"), "/")
		for j, seg := range segments {
			if seg == "**" && j != len(segments)-1 {
				return fmt.Errorf("routes[%d]: ** may only end a path", i)
			}
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				names[seg[1:len(seg)-1]] = true
			}
		}
		for _, name := range templateNames(r.Resource) {
			if !names[name] {
				return fmt.Errorf("routes[%d]: resource names {%s}, which the path does not", i, name)
			}
		}
		for meter, path := range r.Meter {
			if meter != "characters" {
				return fmt.Errorf("routes[%d]: a route meters only characters (requests and bytes are always counted)", i)
			}
			if !strings.HasPrefix(path, "$.") {
				return fmt.Errorf("routes[%d]: meter path %q must start with $.", i, path)
			}
		}
	}
	return nil
}

func templateNames(template string) []string {
	var names []string
	for {
		start := strings.Index(template, "{")
		if start < 0 {
			return names
		}
		end := strings.Index(template[start:], "}")
		if end < 0 {
			return names
		}
		names = append(names, template[start+1:start+end])
		template = template[start+end+1:]
	}
}

// Match returns the first route for a request and the names it read.
func (r RestInspector) Match(method, path string) (*Route, map[string]string, bool) {
	method = strings.ToUpper(method)
	for i := range r.Routes {
		route := &r.Routes[i]
		if route.Method != "*" && !strings.EqualFold(route.Method, method) {
			continue
		}
		if names, ok := matchPath(route.Path, path); ok {
			return route, names, true
		}
	}
	return nil, nil, false
}

func matchPath(pattern, path string) (map[string]string, bool) {
	ps := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	names := map[string]string{}
	for i, p := range ps {
		if p == "**" {
			return names, true
		}
		if i >= len(segs) {
			return nil, false
		}
		seg := segs[i]
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			if seg == "" {
				return nil, false
			}
			names[p[1:len(p)-1]] = seg
			continue
		}
		if p != seg {
			return nil, false
		}
	}
	return names, len(ps) == len(segs)
}

// Classify reads Fields["method"] and Fields["path"], and the body when the
// route meters characters.
func (r RestInspector) Classify(_ context.Context, call Call) (Classified, error) {
	method, path := strings.ToUpper(call.Fields["method"]), call.Fields["path"]
	route, names, ok := r.Match(method, path)
	if !ok {
		return Classified{}, &NoRouteError{Method: method, Path: path}
	}
	resource := r.Host + path
	if route.Resource != "" {
		resource = route.Resource
		for name, value := range names {
			resource = strings.ReplaceAll(resource, "{"+name+"}", value)
		}
		resource = r.Host + "/" + strings.TrimPrefix(resource, "/")
	}
	out := Classified{Operation: route.Operation, Resource: resource, Amounts: map[string]int64{}}
	if field, ok := route.Meter["characters"]; ok {
		if len(call.Body) > MaxMeteredBody {
			return Classified{}, ErrBodyTooLarge
		}
		var body any
		if err := json.Unmarshal(call.Body, &body); err != nil {
			return Classified{}, fmt.Errorf("the request body is not JSON this route can meter: %w", err)
		}
		if text, ok := jsonAt(body, field).(string); ok {
			out.Amounts["characters"] = int64(utf8.RuneCountInString(text))
		} else {
			out.Amounts["characters"] = 0
		}
	}
	return out, nil
}

// jsonAt follows a $.a.b path through decoded JSON objects.
func jsonAt(value any, path string) any {
	for _, key := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}
