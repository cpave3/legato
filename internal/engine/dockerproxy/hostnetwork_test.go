package dockerproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func hostNetworkHarness(t *testing.T, capAdd ...string) (*harness, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var logged []string
	h := newHarnessWith(t, func(o *Options) {
		o.HostNetwork = true
		o.CapAdd = append(o.CapAdd, capAdd...)
		o.Logf = func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			logged = append(logged, fmt.Sprintf(format, args...))
		}
	})
	return h, &logged
}

// What the daemon was sent to make the last container.
func lastCreate(t *testing.T, h *harness) map[string]any {
	t.Helper()
	got := h.daemon.requests("POST", "/containers/create")
	if len(got) == 0 {
		t.Fatal("no container was made")
	}
	var body map[string]any
	if err := json.Unmarshal(got[len(got)-1].Body, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestTheHostNetworkIsRefusedUnlessTheAgentMayUseIt(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"--network host", "POST", "/containers/create", createBody(map[string]any{"NetworkMode": "host"})},
		{"the host network as an endpoint", "POST", "/containers/create", map[string]any{"Image": "alpine", "NetworkingConfig": map[string]any{"EndpointsConfig": map[string]any{"host": map[string]any{}}}}},
		{"a build on the host network", "POST", "/build?networkmode=host", nil},
	} {
		if status, body := h.call(t, tc.method, tc.path, tc.body); status != http.StatusForbidden || !strings.Contains(body, "host") {
			t.Errorf("%s = %d %s, want refused", tc.name, status, body)
		}
	}
}

func TestAnAgentAllowedTheHostNetworkMayUseIt(t *testing.T) {
	h, logged := hostNetworkHarness(t)
	body := createBody(map[string]any{"NetworkMode": "host"})
	body["NetworkingConfig"] = map[string]any{"EndpointsConfig": map[string]any{"host": map[string]any{}}}
	if status, out := h.call(t, "POST", "/v1.47/containers/create?name=proposal-print", body); status != http.StatusOK {
		t.Fatalf("--network host = %d %s", status, out)
	}
	sent := lastCreate(t, h)
	hc := sent["HostConfig"].(map[string]any)
	if hc["NetworkMode"] != "host" {
		t.Fatalf("NetworkMode sent = %v", hc["NetworkMode"])
	}
	if !strings.Contains(fmt.Sprint(hc["CapDrop"]), "NET_RAW") || !strings.Contains(fmt.Sprint(hc["CapDrop"]), "NET_BIND_SERVICE") {
		t.Errorf("CapDrop = %v: a container on the host network must neither sniff it nor take its low ports", hc["CapDrop"])
	}
	if sent["Labels"].(map[string]any)[Label] != "ex-1" {
		t.Errorf("labels = %v", sent["Labels"])
	}
	if !strings.Contains(strings.Join(*logged, "\n"), "host network") || !strings.Contains(strings.Join(*logged, "\n"), "proposal-print") {
		t.Errorf("the log = %q, want a line naming the host-network container", *logged)
	}
	if status, out := h.call(t, "POST", "/build?networkmode=host", "context"); status != http.StatusOK {
		t.Fatalf("a build on the host network = %d %s", status, out)
	}
	if status, out := h.call(t, "GET", "/networks/host", nil); status != http.StatusOK {
		t.Fatalf("inspecting the host network = %d %s", status, out)
	}
	// A container on a network of its own is unchanged.
	if status, out := h.call(t, "POST", "/containers/create", createBody(map[string]any{"NetworkMode": "mynet"})); status != http.StatusOK {
		t.Fatalf("its own network = %d %s", status, out)
	}
	if hc := lastCreate(t, h)["HostConfig"].(map[string]any); hc["CapDrop"] != nil {
		t.Errorf("CapDrop on its own network = %v, want none added", hc["CapDrop"])
	}
}

// The host network is all the option gives: every other way to the host
// stays shut, and the network's own settings stay the host's.
func TestTheHostNetworkGivesNothingElse(t *testing.T) {
	h, _ := hostNetworkHarness(t, "NET_ADMIN", "NET_RAW", "NET_BIND_SERVICE")
	host := func(more map[string]any) map[string]any {
		hc := map[string]any{"NetworkMode": "host"}
		for k, v := range more {
			hc[k] = v
		}
		return createBody(hc)
	}
	for _, tc := range []struct {
		name string
		body any
		want string
	}{
		{"--pid host", host(map[string]any{"PidMode": "host"}), "PidMode host"},
		{"--ipc host", host(map[string]any{"IpcMode": "host"}), "IpcMode host"},
		{"--uts host", host(map[string]any{"UTSMode": "host"}), "UTSMode host"},
		{"--userns host", host(map[string]any{"UsernsMode": "host"}), "UsernsMode host"},
		{"--cgroupns host", host(map[string]any{"CgroupnsMode": "host"}), "CgroupnsMode host"},
		{"--privileged", host(map[string]any{"Privileged": true}), "privileged"},
		{"--device", host(map[string]any{"Devices": []any{map[string]any{"PathOnHost": "/dev/sda"}}}), "devices"},
		{"--security-opt seccomp=unconfined", host(map[string]any{"SecurityOpt": []string{"seccomp=unconfined"}}), "seccomp"},
		{"--cap-add SYS_ADMIN", host(map[string]any{"CapAdd": []string{"SYS_ADMIN"}}), "SYS_ADMIN"},
		{"--cap-add NET_ADMIN, though cap_add lists it", host(map[string]any{"CapAdd": []string{"NET_ADMIN"}}), "NET_ADMIN"},
		{"--cap-add NET_RAW, though cap_add lists it", host(map[string]any{"CapAdd": []string{"CAP_NET_RAW"}}), "NET_RAW"},
		{"--cap-add NET_BIND_SERVICE, though cap_add lists it", host(map[string]any{"CapAdd": []string{"net_bind_service"}}), "NET_BIND_SERVICE"},
		{"--cap-add ALL", host(map[string]any{"CapAdd": []string{"ALL"}}), "ALL"},
		{"a net.* sysctl, which would be the host's", host(map[string]any{"Sysctls": map[string]string{"net.ipv4.ip_forward": "1"}}), "net.ipv4.ip_forward"},
		{"a bind of ~/.ssh", host(map[string]any{"Binds": []string{h.s.ssh + ":/k"}}), "ssh"},
	} {
		status, body := h.call(t, "POST", "/containers/create", tc.body)
		if status != http.StatusForbidden || !strings.Contains(body, "legato: ") || !strings.Contains(body, tc.want) {
			t.Errorf("%s = %d %s, want refused saying %q", tc.name, status, body, tc.want)
		}
	}
	// cap_add's capabilities still work on a network of its own.
	if status, body := h.call(t, "POST", "/containers/create", createBody(map[string]any{"CapAdd": []string{"NET_ADMIN"}})); status != http.StatusOK {
		t.Fatalf("NET_ADMIN on the bridge = %d %s", status, body)
	}
}
