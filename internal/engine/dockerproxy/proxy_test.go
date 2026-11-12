package dockerproxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDaemon answers the Docker API as far as the proxy asks it, knowing
// one execution's things (ex-1), another's (ex-2) and the person's own.
type fakeDaemon struct {
	mu       sync.Mutex
	got      []seen
	volumes  map[string]map[string]string
	logsNext chan struct{}
}

type seen struct {
	Method, Path string
	Query        url.Values
	Body         []byte
}

type thing struct {
	id, name string
	labels   map[string]string
}

var (
	containers = []thing{
		{"aaaa1111", "mine", map[string]string{Label: "ex-1"}},
		{"bbbb2222", "theirs", map[string]string{Label: "ex-2"}},
		{"cccc3333", "persons", nil},
	}
	networks = []thing{{"n1", "mynet", map[string]string{Label: "ex-1"}}, {"n2", "theirnet", map[string]string{Label: "ex-2"}}}
	images   = []thing{{"sha256:i1", "built:1", map[string]string{Label: "ex-1"}}, {"sha256:i2", "alpine", nil}}
	execs    = map[string]string{"exec-mine": "aaaa1111", "exec-theirs": "bbbb2222"}
)

func find(list []thing, ref string) (thing, bool) {
	for _, t := range list {
		if t.id == ref || t.name == ref {
			return t, true
		}
	}
	return thing{}, false
}

func (d *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if loc := versionPrefix.FindStringIndex(path); loc != nil {
		path = "/" + strings.TrimPrefix(path[loc[1]-1:], "/")
	}
	body, _ := io.ReadAll(r.Body)
	seg := strings.Split(strings.Trim(path, "/"), "/")
	reply := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	if r.Method == http.MethodGet {
		switch {
		case len(seg) == 3 && seg[0] == "containers" && seg[2] == "json":
			if c, ok := find(containers, seg[1]); ok {
				reply(map[string]any{"Id": c.id, "Config": map[string]any{"Labels": c.labels}})
				return
			}
			http.NotFound(w, r)
			return
		case len(seg) == 3 && seg[0] == "exec" && seg[2] == "json":
			if c, ok := execs[seg[1]]; ok {
				reply(map[string]any{"ContainerID": c})
				return
			}
			http.NotFound(w, r)
			return
		case len(seg) == 2 && seg[0] == "networks":
			if n, ok := find(networks, seg[1]); ok {
				reply(map[string]any{"Id": n.id, "Name": n.name, "Labels": n.labels})
				return
			}
			http.NotFound(w, r)
			return
		case len(seg) == 2 && seg[0] == "volumes":
			d.mu.Lock()
			labels, ok := d.volumes[seg[1]]
			d.mu.Unlock()
			if ok {
				reply(map[string]any{"Name": seg[1], "Labels": labels})
				return
			}
			http.NotFound(w, r)
			return
		case len(seg) >= 3 && seg[0] == "images" && seg[len(seg)-1] == "json":
			if i, ok := find(images, strings.Join(seg[1:len(seg)-1], "/")); ok {
				reply(map[string]any{"Id": i.id, "Config": map[string]any{"Labels": i.labels}})
				return
			}
			http.NotFound(w, r)
			return
		case len(seg) == 3 && seg[0] == "containers" && seg[2] == "logs":
			w.Write([]byte("one\n"))
			w.(http.Flusher).Flush()
			select {
			case <-d.logsNext:
			case <-time.After(5 * time.Second):
			}
			w.Write([]byte("two\n"))
			return
		}
	}
	d.mu.Lock()
	d.got = append(d.got, seen{Method: r.Method, Path: path, Query: r.URL.Query(), Body: body})
	if path == "/volumes/create" {
		var v struct {
			Name   string
			Labels map[string]string
		}
		_ = json.Unmarshal(body, &v)
		d.volumes[v.Name] = v.Labels
	}
	d.mu.Unlock()
	if r.Header.Get("Upgrade") != "" {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n"))
		io.Copy(conn, buf)
		return
	}
	reply(map[string]any{})
}

func (d *fakeDaemon) requests(method, path string) []seen {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []seen
	for _, s := range d.got {
		if s.Method == method && s.Path == path {
			out = append(out, s)
		}
	}
	return out
}

type harness struct {
	s      sandboxed
	daemon *fakeDaemon
	proxy  *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{s: newSandboxed(t), daemon: &fakeDaemon{
		volumes:  map[string]map[string]string{"myvol": {Label: "ex-1"}, "theirvol": {Label: "ex-2"}},
		logsNext: make(chan struct{}),
	}}
	daemon := httptest.NewServer(h.daemon)
	t.Cleanup(daemon.Close)
	p := New(Options{Execution: "ex-1", View: h.s.view, CapAdd: []string{"SYS_PTRACE"},
		Upstream: Upstream{Network: "tcp", Address: strings.TrimPrefix(daemon.URL, "http://")}, Logf: t.Logf})
	h.proxy = httptest.NewServer(p)
	t.Cleanup(h.proxy.Close)
	return h
}

// call sends a request through the proxy: the status and the body.
func (h *harness) call(t *testing.T, method, path string, body any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			reader = strings.NewReader(b)
		default:
			data, _ := json.Marshal(b)
			reader = bytes.NewReader(data)
		}
	}
	req, _ := http.NewRequest(method, h.proxy.URL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func TestRoutesTheCLIDoesNotNeedAreRefused(t *testing.T) {
	h := newHarness(t)
	for _, route := range [][2]string{
		{"POST", "/v1.47/swarm/init"}, {"GET", "/services"}, {"POST", "/secrets/create"}, {"GET", "/configs"},
		{"GET", "/plugins"}, {"POST", "/plugins/pull"}, {"GET", "/nodes"}, {"GET", "/tasks"},
		{"POST", "/images/alpine/push"}, {"POST", "/build/prune"}, {"GET", "/containers/x/../../plugins"},
		{"PUT", "/volumes/myvol"},
	} {
		status, body := h.call(t, route[0], route[1], nil)
		if status != http.StatusForbidden || !strings.Contains(body, `"message":"legato: `) {
			t.Errorf("%s %s = %d %s, want 403 with legato's reason", route[0], route[1], status, body)
		}
	}
	if len(h.daemon.got) != 0 {
		t.Fatalf("the daemon was asked %+v", h.daemon.got)
	}
}

func TestEverydayRoutesPassThrough(t *testing.T) {
	h := newHarness(t)
	for _, route := range [][2]string{
		{"GET", "/_ping"}, {"GET", "/v1.47/version"}, {"GET", "/info"}, {"GET", "/system/df"},
		{"GET", "/images/json"}, {"POST", "/images/create?fromImage=alpine"}, {"POST", "/images/alpine/tag?repo=x"},
		{"POST", "/session"}, {"POST", "/grpc"},
	} {
		if status, body := h.call(t, route[0], route[1], nil); status != http.StatusOK {
			t.Errorf("%s %s = %d %s", route[0], route[1], status, body)
		}
	}
}

func createBody(hostConfig map[string]any) map[string]any {
	return map[string]any{"Image": "alpine", "HostConfig": hostConfig}
}

func TestEscapesFromTheSandboxAreRefused(t *testing.T) {
	h := newHarness(t)
	link := filepath.Join(h.s.work, "keys")
	if err := os.Symlink(h.s.ssh, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		body any
		want string
	}{
		{"-v ~/.ssh:/x", createBody(map[string]any{"Binds": []string{h.s.ssh + ":/x"}}), "does not exist"},
		{"-v /:/host", createBody(map[string]any{"Binds": []string{"/:/host"}}), "not in this agent's sandbox"},
		{"a link in the worktree to ~/.ssh", createBody(map[string]any{"Binds": []string{link + ":/x"}}), "not in this agent's sandbox"},
		{"the Docker socket", createBody(map[string]any{"Binds": []string{"/var/run/docker.sock:/var/run/docker.sock"}}), "not in this agent's sandbox"},
		{"the Docker socket as a mount", createBody(map[string]any{"Mounts": []any{map[string]any{"Type": "bind", "Source": "/run/docker.sock", "Target": "/s"}}}), "not in this agent's sandbox"},
		{"--privileged", createBody(map[string]any{"Privileged": true}), "privileged"},
		{"privileged spelled in lower case", `{"Image":"alpine","HostConfig":{"privileged":true}}`, "privileged"},
		{"privileged given twice", `{"Image":"alpine","HostConfig":{"Privileged":false,"privileged":true}}`, "twice"},
		{"privileged at the top level", `{"Image":"alpine","Privileged":true}`, "Privileged is not allowed"},
		{"--pid host", createBody(map[string]any{"PidMode": "host"}), "PidMode host"},
		{"--ipc host", createBody(map[string]any{"IpcMode": "host"}), "IpcMode host"},
		{"--uts host", createBody(map[string]any{"UTSMode": "host"}), "UTSMode host"},
		{"--userns host", createBody(map[string]any{"UsernsMode": "host"}), "UsernsMode host"},
		{"--cgroupns host", createBody(map[string]any{"CgroupnsMode": "host"}), "CgroupnsMode host"},
		{"--network host", createBody(map[string]any{"NetworkMode": "host"}), "host network"},
		{"--network container:theirs", createBody(map[string]any{"NetworkMode": "container:theirs"}), "no container"},
		{"--pid container:persons", createBody(map[string]any{"PidMode": "container:persons"}), "no container"},
		{"another agent's network", createBody(map[string]any{"NetworkMode": "theirnet"}), "no network"},
		{"another agent's network as an endpoint", map[string]any{"Image": "alpine", "NetworkingConfig": map[string]any{"EndpointsConfig": map[string]any{"theirnet": map[string]any{}}}}, "no network"},
		{"--cap-add SYS_ADMIN", createBody(map[string]any{"CapAdd": []string{"SYS_ADMIN"}}), "SYS_ADMIN"},
		{"--cap-add ALL", createBody(map[string]any{"CapAdd": []string{"ALL"}}), "ALL"},
		{"--device", createBody(map[string]any{"Devices": []any{map[string]any{"PathOnHost": "/dev/sda", "PathInContainer": "/dev/sda"}}}), "devices"},
		{"--device-cgroup-rule", createBody(map[string]any{"DeviceCgroupRules": []string{"b 8:* rmw"}}), "devices"},
		{"--gpus", createBody(map[string]any{"DeviceRequests": []any{map[string]any{"Count": -1}}}), "devices"},
		{"--security-opt seccomp=unconfined", createBody(map[string]any{"SecurityOpt": []string{"seccomp=unconfined"}}), "seccomp=unconfined"},
		{"--security-opt apparmor=unconfined", createBody(map[string]any{"SecurityOpt": []string{"apparmor=unconfined"}}), "apparmor"},
		{"--security-opt label=disable", createBody(map[string]any{"SecurityOpt": []string{"label=disable"}}), "label=disable"},
		{"--security-opt systempaths=unconfined", createBody(map[string]any{"MaskedPaths": []string{}}), "MaskedPaths"},
		{"--cgroup-parent", createBody(map[string]any{"CgroupParent": "/"}), "CgroupParent"},
		{"a host-wide sysctl", createBody(map[string]any{"Sysctls": map[string]string{"kernel.core_pattern": "|/x"}}), "kernel.core_pattern"},
		{"--volumes-from another agent's", createBody(map[string]any{"VolumesFrom": []string{"theirs:ro"}}), "no container"},
		{"--link to the person's", createBody(map[string]any{"Links": []string{"persons:db"}}), "no container"},
		{"another agent's volume", createBody(map[string]any{"Binds": []string{"theirvol:/data"}}), "not this agent's"},
		{"a volume binding ~/.ssh in its options", createBody(map[string]any{"Mounts": []any{map[string]any{"Type": "volume", "Source": "keys", "Target": "/k",
			"VolumeOptions": map[string]any{"DriverConfig": map[string]any{"Options": map[string]string{"type": "none", "o": "bind", "device": h.s.ssh}}}}}}), "does not exist"},
		{"a mount of a source that is missing", createBody(map[string]any{"Mounts": []any{map[string]any{"Type": "bind", "Source": filepath.Join(h.s.work, "missing"), "Target": "/w", "BindOptions": map[string]any{"CreateMountpoint": true}}}}), "does not exist"},
		{"a field Legato does not know", createBody(map[string]any{"Future": true}), "HostConfig.Future is not allowed"},
		{"another runtime", createBody(map[string]any{"Runtime": "nvidia"}), "runtime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := h.call(t, "POST", "/v1.47/containers/create", tc.body)
			if status/100 == 2 || !strings.Contains(body, "legato: ") || !strings.Contains(body, tc.want) {
				t.Fatalf("= %d %s, want a refusal saying %q", status, body, tc.want)
			}
		})
	}
	if got := h.daemon.requests("POST", "/containers/create"); len(got) != 0 {
		t.Fatalf("the daemon was asked to create %d containers", len(got))
	}
}

func TestAContainerIsMadeWithinTheSandbox(t *testing.T) {
	h := newHarness(t)
	status, body := h.call(t, "POST", "/v1.47/containers/create?name=web", map[string]any{
		"Image": "alpine", "Labels": map[string]string{"com.docker.compose.project": "app", Label: "ex-2"},
		"HostConfig": map[string]any{
			"Binds": []string{h.s.work + ":/src:rw,z", h.s.feed + ":/feed:rw", "fresh:/data"},
			"Mounts": []any{map[string]any{"Type": "bind", "Source": h.s.feed, "Target": "/feed2"},
				map[string]any{"Type": "volume", "Source": "myvol", "Target": "/v"}},
			"CapAdd":       []string{"SYS_PTRACE"},
			"NetworkMode":  "mynet",
			"PortBindings": map[string]any{"80/tcp": []any{map[string]string{"HostIp": "", "HostPort": "8080"}, map[string]string{"HostIp": "0.0.0.0", "HostPort": "8081"}}},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("create = %d %s", status, body)
	}
	got := h.daemon.requests("POST", "/containers/create")
	if len(got) != 1 {
		t.Fatalf("creates = %+v", got)
	}
	var sent struct {
		Labels     map[string]string
		HostConfig struct {
			Binds  []string
			Mounts []struct {
				Source   string
				ReadOnly bool
			}
			PortBindings map[string][]struct{ HostIp, HostPort string }
		}
	}
	if err := json.Unmarshal(got[0].Body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Labels[Label] != "ex-1" || sent.Labels["com.docker.compose.project"] != "app" {
		t.Errorf("labels = %v, want this execution's and compose's own", sent.Labels)
	}
	wantBinds := []string{h.s.work + ":/src:z", h.s.feed + ":/feed:ro", "fresh:/data"}
	if strings.Join(sent.HostConfig.Binds, " ") != strings.Join(wantBinds, " ") {
		t.Errorf("binds = %v, want %v (a read-only resource stays read-only)", sent.HostConfig.Binds, wantBinds)
	}
	if m := sent.HostConfig.Mounts; len(m) != 2 || m[0].Source != h.s.feed || !m[0].ReadOnly {
		t.Errorf("mounts = %+v, want the resource read-only", m)
	}
	for _, b := range sent.HostConfig.PortBindings["80/tcp"] {
		if b.HostIp != "127.0.0.1" {
			t.Errorf("port binding %+v, want it on 127.0.0.1", b)
		}
	}
	// A named volume the daemon would make is made first, labelled.
	if vols := h.daemon.requests("POST", "/volumes/create"); len(vols) != 1 || !strings.Contains(string(vols[0].Body), `"legato.execution":"ex-1"`) {
		t.Errorf("volume creates = %+v", vols)
	}
}

func TestVolumesBindOnlyWhatTheSandboxShows(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name string
		opts map[string]string
		want string
	}{
		{"~/.ssh by its host path", map[string]string{"type": "none", "o": "bind", "device": filepath.Join(h.s.root, "home/me/.ssh")}, "does not exist"},
		{"the root", map[string]string{"type": "none", "o": "bind", "device": "/"}, "not in this agent's sandbox"},
		{"a block device", map[string]string{"type": "ext4", "device": "/dev/sda1"}, "not allowed"},
	} {
		status, body := h.call(t, "POST", "/volumes/create", map[string]any{"Name": "v", "DriverOpts": tc.opts})
		if status != http.StatusForbidden || !strings.Contains(body, tc.want) {
			t.Errorf("%s: = %d %s, want a refusal saying %q", tc.name, status, body, tc.want)
		}
	}
	if status, body := h.call(t, "POST", "/volumes/create", map[string]any{"Name": "feed", "DriverOpts": map[string]string{"type": "none", "o": "bind", "device": h.s.feed}}); status != http.StatusOK {
		t.Fatalf("a volume of a read-only resource = %d %s", status, body)
	}
	got := h.daemon.requests("POST", "/volumes/create")
	var sent struct {
		DriverOpts map[string]string
		Labels     map[string]string
	}
	if len(got) != 1 || json.Unmarshal(got[0].Body, &sent) != nil {
		t.Fatalf("volume creates = %+v", got)
	}
	if sent.DriverOpts["o"] != "bind,ro" || sent.DriverOpts["device"] != h.s.feed || sent.Labels[Label] != "ex-1" {
		t.Fatalf("sent %+v, want a read-only bind, labelled", sent)
	}
}

func TestAnAgentTouchesOnlyItsOwnThings(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		method, path string
		body         any
		status       int
	}{
		{"POST", "/containers/theirs/exec", map[string]any{"Cmd": []string{"sh"}}, http.StatusNotFound},
		{"POST", "/containers/persons/exec", map[string]any{"Cmd": []string{"sh"}}, http.StatusNotFound},
		{"POST", "/exec/exec-theirs/start", map[string]any{}, http.StatusNotFound},
		{"GET", "/containers/bbbb2222/json", nil, http.StatusNotFound},
		{"GET", "/containers/theirs/logs", nil, http.StatusNotFound},
		{"DELETE", "/containers/theirs", nil, http.StatusNotFound},
		{"GET", "/containers/theirs/archive?path=/", nil, http.StatusNotFound},
		{"POST", "/commit?container=theirs", nil, http.StatusNotFound},
		{"POST", "/networks/theirnet/connect", map[string]any{"Container": "mine"}, http.StatusNotFound},
		{"POST", "/networks/mynet/connect", map[string]any{"Container": "theirs"}, http.StatusNotFound},
		{"DELETE", "/volumes/theirvol", nil, http.StatusNotFound},
		{"DELETE", "/images/alpine", nil, http.StatusForbidden},
		{"POST", "/containers/mine/exec", map[string]any{"Cmd": []string{"sh"}, "Privileged": true}, http.StatusForbidden},
		{"POST", "/containers/mine/update", map[string]any{"Devices": []any{}}, http.StatusForbidden},
	} {
		status, body := h.call(t, tc.method, tc.path, tc.body)
		if status != tc.status || !strings.Contains(body, "legato: ") {
			t.Errorf("%s %s = %d %s, want %d", tc.method, tc.path, status, body, tc.status)
		}
	}
	if len(h.daemon.got) != 0 {
		t.Fatalf("the daemon was asked %+v", h.daemon.got)
	}
	// Its own, by name, reach the daemon by full ID.
	for _, tc := range [][2]string{{"POST", "/v1.47/containers/mine/start"}, {"POST", "/containers/mine/exec"}, {"DELETE", "/images/built:1"}, {"DELETE", "/volumes/myvol"}, {"POST", "/exec/exec-mine/start"}} {
		if status, body := h.call(t, tc[0], tc[1], map[string]any{}); status != http.StatusOK {
			t.Errorf("%s %s = %d %s", tc[0], tc[1], status, body)
		}
	}
	if got := h.daemon.requests("POST", "/containers/aaaa1111/start"); len(got) != 1 {
		t.Fatalf("starting its own container by name did not reach the daemon by ID: %+v", h.daemon.got)
	}
}

func TestListsAndEventsShowOnlyTheAgentsOwn(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/containers/json", "/networks", "/volumes", "/events", "/containers/prune", "/volumes/prune", "/networks/prune", "/images/prune"} {
		method := "GET"
		if strings.HasSuffix(path, "prune") {
			method = "POST"
		}
		filters := url.QueryEscape(`{"status":["running"]}`)
		if status, body := h.call(t, method, path+"?all=1&filters="+filters, nil); status != http.StatusOK {
			t.Fatalf("%s = %d %s", path, status, body)
		}
		got := h.daemon.requests(method, path)
		if len(got) != 1 {
			t.Fatalf("%s reached the daemon %d times", path, len(got))
		}
		var f map[string][]string
		if err := json.Unmarshal([]byte(got[0].Query.Get("filters")), &f); err != nil {
			t.Fatal(err)
		}
		if strings.Join(f["label"], ",") != Label+"=ex-1" || strings.Join(f["status"], ",") != "running" {
			t.Errorf("%s filters = %v, want the agent's label and its own filter", path, f)
		}
	}
}

func TestBuildsStayOffTheHostNetworkAndAreLabelled(t *testing.T) {
	h := newHarness(t)
	if status, body := h.call(t, "POST", "/build?networkmode=host", nil); status != http.StatusForbidden || !strings.Contains(body, "network host") {
		t.Fatalf("a build on the host network = %d %s", status, body)
	}
	if status, body := h.call(t, "POST", "/build?t=app:dev&labels="+url.QueryEscape(`{"a":"b"}`), "context"); status != http.StatusOK {
		t.Fatalf("build = %d %s", status, body)
	}
	got := h.daemon.requests("POST", "/build")
	var labels map[string]string
	if len(got) != 1 || json.Unmarshal([]byte(got[0].Query.Get("labels")), &labels) != nil || labels[Label] != "ex-1" || labels["a"] != "b" {
		t.Fatalf("build = %+v", got)
	}
	if string(got[0].Body) != "context" {
		t.Fatalf("the build context reached the daemon as %q", got[0].Body)
	}
}

// docker attach, exec -it and BuildKit's session upgrade the connection;
// logs -f streams. Both reach the client as they happen.
func TestStreamsPassThrough(t *testing.T) {
	h := newHarness(t)
	conn, err := net.Dial("tcp", strings.TrimPrefix(h.proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	conn.Write([]byte("POST /v1.47/exec/exec-mine/start HTTP/1.1\r\nHost: docker\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}"))
	reader := bufio.NewReader(conn)
	status, _ := reader.ReadString('\n')
	if !strings.Contains(status, "101") {
		t.Fatalf("status line %q, want 101", status)
	}
	for line, _ := reader.ReadString('\n'); line != "\r\n" && line != ""; line, _ = reader.ReadString('\n') {
	}
	conn.Write([]byte("ping"))
	echo := make([]byte, 4)
	if _, err := io.ReadFull(reader, echo); err != nil || string(echo) != "ping" {
		t.Fatalf("echo %q, %v", echo, err)
	}

	resp, err := http.Get(h.proxy.URL + "/containers/mine/logs?follow=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := bufio.NewReader(resp.Body)
	first, err := lines.ReadString('\n')
	if err != nil || first != "one\n" {
		t.Fatalf("first line %q, %v: the stream did not arrive as it was written", first, err)
	}
	close(h.daemon.logsNext)
	if second, _ := lines.ReadString('\n'); second != "two\n" {
		t.Fatalf("second line %q", second)
	}
}
