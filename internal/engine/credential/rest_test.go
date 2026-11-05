package credential

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func elevenLabsLike() RestInspector {
	return RestInspector{Host: "api.elevenlabs.io", Routes: []Route{
		{Method: "POST", Path: "/v1/text-to-speech/{voice}", Operation: "tts.speak", Resource: "voice/{voice}", Meter: map[string]string{"characters": "$.text"}},
		{Method: "GET", Path: "/v1/voices", Operation: "voices.read"},
		{Method: "*", Path: "/v1/user/**", Operation: "account.manage"},
		{Method: "GET", Path: "/v1/**", Operation: "read"},
	}}
}

func classify(t *testing.T, r RestInspector, method, path, body string) (Classified, error) {
	t.Helper()
	return r.Classify(context.Background(), Call{Channel: "http-proxy", Fields: map[string]string{"method": method, "path": path}, Body: []byte(body)})
}

func TestTheFirstMatchingRouteClassifiesARequest(t *testing.T) {
	r := elevenLabsLike()
	for _, tc := range []struct{ method, path, operation, resource string }{
		{"POST", "/v1/text-to-speech/abc", "tts.speak", "api.elevenlabs.io/voice/abc"},
		{"GET", "/v1/voices", "voices.read", "api.elevenlabs.io/v1/voices"},
		{"DELETE", "/v1/user/subscription", "account.manage", "api.elevenlabs.io/v1/user/subscription"},
		{"GET", "/v1/user", "account.manage", "api.elevenlabs.io/v1/user"}, // ** matches no segment too
		{"GET", "/v1/models", "read", "api.elevenlabs.io/v1/models"},
		{"get", "/v1/voices", "voices.read", "api.elevenlabs.io/v1/voices"},
	} {
		got, err := classify(t, r, tc.method, tc.path, `{"text":"hi"}`)
		if err != nil || got.Operation != tc.operation || got.Resource != tc.resource {
			t.Errorf("%s %s = %+v, %v; want %s on %s", tc.method, tc.path, got, err, tc.operation, tc.resource)
		}
	}
}

func TestARequestThatMatchesNoRouteIsRefused(t *testing.T) {
	r := elevenLabsLike()
	for _, tc := range []struct{ method, path string }{
		{"POST", "/v1/voices"},                  // wrong method
		{"POST", "/v1/text-to-speech/abc/more"}, // {voice} is one segment
		{"GET", "/v2/voices"},
		{"POST", "/v1/text-to-speech/"},
	} {
		_, err := classify(t, r, tc.method, tc.path, `{"text":"hi"}`)
		var noRoute *NoRouteError
		if !errors.As(err, &noRoute) || noRoute.Method != strings.ToUpper(tc.method) || noRoute.Path != tc.path {
			t.Errorf("%s %s: err = %v, want a NoRouteError naming it", tc.method, tc.path, err)
		}
	}
}

func TestCharactersAreCountedInCodePoints(t *testing.T) {
	got, err := classify(t, elevenLabsLike(), "POST", "/v1/text-to-speech/abc", `{"text":"héllo","model_id":"x"}`)
	if err != nil || got.Amounts["characters"] != 5 {
		t.Fatalf("amounts = %v, %v; want 5 characters", got.Amounts, err)
	}
	got, _ = classify(t, elevenLabsLike(), "POST", "/v1/text-to-speech/abc", `{"text":"日本語 🎙"}`)
	if got.Amounts["characters"] != 5 {
		t.Fatalf("amounts = %v; want 5 characters", got.Amounts)
	}
	got, _ = classify(t, RestInspector{Host: "h", Routes: []Route{{Method: "POST", Path: "/x", Operation: "w", Meter: map[string]string{"characters": "$.input.text"}}}}, "POST", "/x", `{"input":{"text":"abc"}}`)
	if got.Amounts["characters"] != 3 {
		t.Fatalf("nested path amounts = %v; want 3", got.Amounts)
	}
}

func TestAMeteredBodyOverOneMebibyteIsRefused(t *testing.T) {
	body := `{"text":"` + strings.Repeat("a", MaxMeteredBody) + `"}`
	_, err := classify(t, elevenLabsLike(), "POST", "/v1/text-to-speech/abc", body)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("err = %v, want ErrBodyTooLarge", err)
	}
	// A route with no meter does not read the body at all.
	if _, err := classify(t, elevenLabsLike(), "GET", "/v1/voices", body); err != nil {
		t.Fatalf("unmetered route err = %v", err)
	}
}

func TestAMeteredRouteNeedsJSONItCanRead(t *testing.T) {
	if _, err := classify(t, elevenLabsLike(), "POST", "/v1/text-to-speech/abc", `not json`); err == nil {
		t.Fatal("want an error for a body that is not JSON")
	}
}

func TestParseRoutesChecksEachRoute(t *testing.T) {
	for _, bad := range []Route{
		{Method: "GET", Path: "v1/voices", Operation: "x"},                      // no leading slash
		{Method: "GET", Path: "/v1/**/voices", Operation: "x"},                  // ** only last
		{Method: "GET", Path: "/v1/voices"},                                     // no operation
		{Method: "GET", Path: "/v1/{v}", Operation: "x", Resource: "voice/{w}"}, // unknown name
		{Method: "GET", Path: "/x", Operation: "x", Meter: map[string]string{"bytes": "$.a"}},
		{Method: "GET", Path: "/x", Operation: "x", Meter: map[string]string{"characters": "text"}},
	} {
		if err := ValidateRoutes([]Route{bad}); err == nil {
			t.Errorf("route %+v: want an error", bad)
		}
	}
	if err := ValidateRoutes(elevenLabsLike().Routes); err != nil {
		t.Fatal(err)
	}
}

// A route with when takes a request only when its JSON body has those
// values: a draft pull request is classified apart from any other.
func TestARouteWithWhenTakesOnlyRequestsWhoseBodyMatches(t *testing.T) {
	r := RestInspector{Host: "api.github.com", Routes: []Route{
		{Method: "POST", Path: "/repos/{owner}/{repo}/pulls", When: map[string]any{"$.draft": true}, Operation: "pr.create-draft", Resource: "github:{owner}/{repo}@refs/heads/{$.head}"},
		{Method: "POST", Path: "/repos/{owner}/{repo}/pulls", Operation: "pr.create", Resource: "github:{owner}/{repo}@refs/heads/{$.head}"},
	}}
	for _, tc := range []struct{ body, operation string }{
		{`{"head":"legato/x","draft":true}`, "pr.create-draft"},
		{`{"head":"legato/x","draft":false}`, "pr.create"},
		{`{"head":"legato/x","draft":"true"}`, "pr.create"},
		{`{"head":"legato/x"}`, "pr.create"},
	} {
		got, err := classify(t, r, "POST", "/repos/cpave3/legato/pulls", tc.body)
		if err != nil || got.Operation != tc.operation || got.Resource != "github:cpave3/legato@refs/heads/legato/x" {
			t.Errorf("%s = %+v, %v; want %s", tc.body, got, err, tc.operation)
		}
	}
	if route, _, ok := r.Match("POST", "/repos/cpave3/legato/pulls"); !ok || !route.needsBody() {
		t.Fatal("a route with when must have its body read")
	}
}

func TestWhenNamesJSONFieldsAndPlainValues(t *testing.T) {
	for _, when := range []map[string]any{
		{"draft": true},
		{"$.draft": []any{true}},
		{"$.draft": 1.5},
	} {
		if err := ValidateRoutes([]Route{{Method: "POST", Path: "/x", Operation: "x", When: when}}); err == nil {
			t.Errorf("when %v: want an error", when)
		}
	}
	if err := ValidateRoutes([]Route{{Method: "POST", Path: "/x", Operation: "x", When: map[string]any{"$.draft": true, "$.state": "open"}}}); err != nil {
		t.Fatal(err)
	}
}
