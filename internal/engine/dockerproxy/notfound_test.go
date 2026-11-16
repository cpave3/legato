package dockerproxy

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Something the agent cannot see is answered as the Docker daemon answers
// something missing, word for word, so tools that probe for a container
// behave as they do outside the sandbox, and a missing one cannot be told
// from another agent's.
func TestWhatTheAgentCannotSeeIsNotFoundInDockersWords(t *testing.T) {
	h := newHarness(t)
	if status, body := h.call(t, "DELETE", "/containers/mine", nil); status != http.StatusOK {
		t.Fatalf("removing its own container = %d %s", status, body)
	}
	for _, tc := range []struct {
		kind, method, path, want string
	}{
		{"container", "GET", "/v1.47/containers/%s/json", `No such container: %s`},
		{"container", "POST", "/containers/%s/start", `No such container: %s`},
		{"container", "DELETE", "/containers/%s", `No such container: %s`},
		{"network", "GET", "/networks/%s", `network %s not found`},
		{"volume", "GET", "/volumes/%s", `get %s: no such volume`},
		{"exec", "GET", "/exec/%s/json", `No such exec instance: %s`},
	} {
		refs := map[string][]string{
			// removed (its own, now gone), never made, another agent's, the person's
			"container": {"mine", "nothing-here", "theirs", "persons"},
			"network":   {"nothing-here", "theirnet"},
			"volume":    {"nothing-here", "theirvol"},
			"exec":      {"nothing-here", "exec-theirs"},
		}[tc.kind]
		for _, ref := range refs {
			status, body := h.call(t, tc.method, strings.Replace(tc.path, "%s", ref, 1), nil)
			want := `{"message":"` + strings.Replace(tc.want, "%s", ref, 1) + `"}` + "\n"
			if status != http.StatusNotFound || body != want {
				t.Errorf("%s %s with %s = %d %q, want 404 %q", tc.method, tc.path, ref, status, body, want)
			}
		}
	}
}

// docker inspect without a type asks for a plugin last; there are none to
// see, and none may be changed.
func TestPluginsAreNoneToSee(t *testing.T) {
	h := newHarness(t)
	if status, body := h.call(t, "GET", "/v1.47/plugins", nil); status != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Fatalf("GET /plugins = %d %s, want an empty list", status, body)
	}
	if status, body := h.call(t, "GET", "/plugins/dev-cosmos-1/json", nil); status != http.StatusNotFound || body != `{"message":"plugin \"dev-cosmos-1\" not found"}`+"\n" {
		t.Fatalf("GET /plugins/dev-cosmos-1/json = %d %s, want Docker's 404", status, body)
	}
	for _, route := range [][2]string{
		{"POST", "/plugins/pull?remote=x"}, {"GET", "/plugins/privileges?remote=x"}, {"POST", "/plugins/x/enable"},
		{"POST", "/plugins/x/disable"}, {"POST", "/plugins/x/push"}, {"POST", "/plugins/x/set"},
		{"POST", "/plugins/x/upgrade"}, {"DELETE", "/plugins/x"}, {"POST", "/plugins/create?name=x"},
	} {
		if status, body := h.call(t, route[0], route[1], nil); status != http.StatusForbidden || !strings.Contains(body, `"message":"legato: `) {
			t.Errorf("%s %s = %d %s, want 403 with legato's reason", route[0], route[1], status, body)
		}
	}
	if len(h.daemon.got) != 0 {
		t.Fatalf("the daemon was asked %+v", h.daemon.got)
	}
}

// The docker CLI's own inspect, without a type, tries every kind of object
// and ends in "No such object" when none has the name.
func TestTheDockerCLIFindsNoSuchObject(t *testing.T) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker is not installed")
	}
	h := newHarness(t)
	cmd := exec.Command(docker, "inspect", "dev-cosmos-1")
	cmd.Env = append(os.Environ(), "DOCKER_HOST=tcp://"+strings.TrimPrefix(h.proxy.URL, "http://"), "DOCKER_CONFIG="+t.TempDir(), "DOCKER_CONTEXT=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil || !strings.Contains(strings.ToLower(stderr.String()), "no such object: dev-cosmos-1") {
		t.Fatalf("docker inspect = %v, stderr %q, want No such object", err, stderr.String())
	}
}
