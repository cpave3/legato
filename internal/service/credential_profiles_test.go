package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/credential"
)

func shipped(t *testing.T) map[string]CredentialProfile {
	t.Helper()
	profiles, err := LoadCredentialProfiles()
	if err != nil {
		t.Fatal(err)
	}
	return profiles
}

func TestLegatoShipsTheFourProfiles(t *testing.T) {
	profiles := shipped(t)
	for _, name := range []string{"secret", "elevenlabs", "api-key", "ssh"} {
		if _, ok := profiles[name]; !ok {
			t.Errorf("no shipped profile %s", name)
		}
	}
	eleven := profiles["elevenlabs"]
	if eleven.Upstream != "https://api.elevenlabs.io" || eleven.Key.Header != "xi-api-key" || !listHas(eleven.Channels, ChannelHTTPProxy) || eleven.Inspector != "rest" {
		t.Fatalf("elevenlabs = %+v", eleven)
	}
	if eleven.Env["ELEVENLABS_API_KEY"] != "{placeholder}" || eleven.Env["ELEVENLABS_BASE_URL"] != "{base_url}" {
		t.Fatalf("elevenlabs env = %v", eleven.Env)
	}
	ssh := profiles["ssh"]
	if !listHas(ssh.Channels, ChannelSSHAgent) || ssh.Inspector != "ssh" || strings.Join(ssh.Accepts, ",") != "keypair" {
		t.Fatalf("ssh = %+v", ssh)
	}
}

func TestEveryElevenLabsRouteIsClassified(t *testing.T) {
	eleven := shipped(t)["elevenlabs"]
	inspector := credential.RestInspector{Host: "api.elevenlabs.io", Routes: eleven.Routes}
	for _, tc := range []struct{ method, path, operation, resource string }{
		{"POST", "/v1/text-to-speech/JBFq", "tts.speak", "api.elevenlabs.io/voice/JBFq"},
		{"POST", "/v1/text-to-speech/JBFq/stream", "tts.speak", "api.elevenlabs.io/voice/JBFq"},
		{"POST", "/v1/text-to-speech/JBFq/with-timestamps", "tts.speak", "api.elevenlabs.io/voice/JBFq"},
		{"POST", "/v1/text-to-speech/JBFq/stream/with-timestamps", "tts.speak", "api.elevenlabs.io/voice/JBFq"},
		{"GET", "/v1/voices", "voices.read", "api.elevenlabs.io/v1/voices"},
		{"GET", "/v1/voices/JBFq", "voices.read", "api.elevenlabs.io/v1/voices/JBFq"},
		{"GET", "/v2/voices", "voices.read", "api.elevenlabs.io/v2/voices"},
		{"GET", "/v1/models", "models.read", "api.elevenlabs.io/v1/models"},
		{"GET", "/v1/user", "account.manage", "api.elevenlabs.io/v1/user"},
		{"GET", "/v1/user/subscription", "account.manage", "api.elevenlabs.io/v1/user/subscription"},
		{"POST", "/v1/workspace/members", "account.manage", "api.elevenlabs.io/v1/workspace/members"},
		{"GET", "/v1/service-accounts/abc/api-keys", "keys.manage", "api.elevenlabs.io/v1/service-accounts/abc/api-keys"},
		{"DELETE", "/v1/api-keys/abc", "keys.manage", "api.elevenlabs.io/v1/api-keys/abc"},
	} {
		route, _, ok := inspector.Match(tc.method, tc.path)
		if !ok || route.Operation != tc.operation {
			t.Errorf("%s %s: route %+v, %v; want %s", tc.method, tc.path, route, ok, tc.operation)
			continue
		}
		body := []byte(`{"text":"hello"}`)
		got, err := inspector.Classify(nil, credential.Call{Fields: map[string]string{"method": tc.method, "path": tc.path}, Body: body})
		if err != nil || got.Resource != tc.resource {
			t.Errorf("%s %s: resource %q, %v; want %q", tc.method, tc.path, got.Resource, err, tc.resource)
		}
		if tc.operation == "tts.speak" && got.Amounts["characters"] != 5 {
			t.Errorf("%s %s: characters %d", tc.method, tc.path, got.Amounts["characters"])
		}
	}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/v1/voices/add"},
		{"DELETE", "/v1/voices/JBFq"},
		{"POST", "/v1/sound-generation"},
		{"GET", "/v1/history"},
	} {
		if _, _, ok := inspector.Match(tc.method, tc.path); ok {
			t.Errorf("%s %s matched a route; want it refused", tc.method, tc.path)
		}
	}
}

func TestTheAPIKeyProfileSplitsReadsFromWrites(t *testing.T) {
	p := shipped(t)["api-key"]
	inspector := credential.RestInspector{Host: "api.example.com", Routes: p.Routes}
	for method, operation := range map[string]string{"GET": "http.read", "HEAD": "http.read", "POST": "http.write", "DELETE": "http.write", "PATCH": "http.write"} {
		route, _, ok := inspector.Match(method, "/v1/things/1")
		if !ok || route.Operation != operation {
			t.Errorf("%s: %+v, want %s", method, route, operation)
		}
	}
}

func TestAProfileWithAnUnknownChannelOrBadRoutesIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"unknown channel":     "name: x\nchannels: [carrier-pigeon]\n",
		"proxy, no inspector": "name: x\nchannels: [http-proxy]\nupstream: https://a.example\nkey: {header: X}\nroutes: [{method: GET, path: /x, operation: r}]\n",
		"proxy, no routes":    "name: x\nchannels: [http-proxy]\ninspector: rest\nupstream: https://a.example\nkey: {header: X}\n",
		"bad route":           "name: x\nchannels: [http-proxy]\ninspector: rest\nupstream: https://a.example\nkey: {header: X}\nroutes: [{method: GET, path: x, operation: r}]\n",
		"http upstream":       "name: x\nchannels: [http-proxy]\ninspector: rest\nupstream: http://a.example\nkey: {header: X}\nroutes: [{method: GET, path: /x, operation: r}]\n",
		"ssh, not keypair":    "name: x\nchannels: [ssh-agent]\ninspector: ssh\naccepts: [value]\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCredentialProfiles(dir); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}
