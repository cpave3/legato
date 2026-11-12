package dockerproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Label marks every container, network, volume and image an execution
// makes, with the execution's ID as its value.
const Label = "legato.execution"

// maxBody is the largest request body the proxy reads to check it. A build
// context is streamed, not read.
const maxBody = 8 << 20

// Options is one execution's proxy.
type Options struct {
	// Execution labels what the agent makes and is the only owner it may
	// touch.
	Execution string
	// View is what the agent's sandbox shows, which is all a container may
	// mount.
	View View
	// CapAdd are the capabilities a container may add, such as SYS_PTRACE.
	CapAdd []string
	// PublishAnywhere lets published ports bind every address; otherwise
	// they bind 127.0.0.1.
	PublishAnywhere bool
	// Upstream is the daemon.
	Upstream Upstream
	// Logf logs a refusal; nothing when nil.
	Logf func(format string, args ...any)
}

// Proxy serves one execution's Docker API socket.
type Proxy struct {
	opts   Options
	rp     *httputil.ReverseProxy
	client *http.Client
}

// New makes an execution's proxy.
func New(opts Options) *Proxy {
	transport := opts.Upstream.transport()
	p := &Proxy{opts: opts, client: &http.Client{Transport: transport, Timeout: 30 * time.Second}}
	p.rp = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme = "http"
			r.Out.URL.Host = "docker"
			r.Out.Host = "docker"
		},
		Transport: transport,
		// Logs and events with follow, and pulls and builds, report as they
		// go.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusBadGateway, "legato: the Docker daemon cannot be reached: "+err.Error())
		},
	}
	return p
}

// Serve answers on ln until it is closed. Streams (attach, exec, logs and
// events that follow, BuildKit's session) have no time limit.
func (p *Proxy) Serve(ln net.Listener) error {
	server := &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	err := server.Serve(ln)
	if errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// refusal is a request the policy turns down, answered as the daemon
// answers an error so the CLI prints it.
type refusal struct {
	status int
	msg    string
}

func (r *refusal) Error() string { return r.msg }

func refuse(format string, args ...any) error {
	return &refusal{status: http.StatusForbidden, msg: fmt.Sprintf(format, args...)}
}

func notFound(kind, ref string) error {
	return &refusal{status: http.StatusNotFound, msg: fmt.Sprintf("no %s %q belongs to this agent", kind, ref)}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": msg})
}

var versionPrefix = regexp.MustCompile(`^/v[0-9]+(\.[0-9]+)*(/|$)`)

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := p.check(r)
	if err != nil {
		status, msg := http.StatusInternalServerError, err.Error()
		var ref *refusal
		if errors.As(err, &ref) {
			status = ref.status
		}
		if p.opts.Logf != nil {
			p.opts.Logf("docker: refused %s %s for %s: %s", r.Method, r.URL.Path, p.opts.Execution, msg)
		}
		writeError(w, status, "legato: "+msg)
		return
	}
	p.rp.ServeHTTP(w, r)
}

// check applies the policy to a request, rewriting it where the policy
// changes it (labels, filters, resolved paths), or refuses it.
func (p *Proxy) check(r *http.Request) error {
	if r.URL.RawPath != "" {
		return refuse("escaped characters in the path are not allowed")
	}
	path := r.URL.Path
	if loc := versionPrefix.FindStringIndex(path); loc != nil {
		path = "/" + strings.TrimPrefix(path[loc[1]-1:], "/")
	}
	seg := strings.Split(strings.Trim(path, "/"), "/")
	for _, s := range seg {
		if s == "" || s == "." || s == ".." {
			if path == "/" {
				break
			}
			return refuse("the path %s is not allowed", r.URL.Path)
		}
	}
	m := r.Method
	get := m == http.MethodGet || m == http.MethodHead
	post := m == http.MethodPost
	ctx := r.Context()
	denied := refuse("%s %s is not allowed for sandboxed agents", m, path)
	switch seg[0] {
	case "_ping", "version", "info":
		if len(seg) == 1 && get {
			return nil
		}
	case "system":
		if len(seg) == 2 && seg[1] == "df" && get {
			return nil
		}
	case "events":
		if len(seg) == 1 && get {
			return p.scopeFilters(r)
		}
	case "session", "grpc":
		// BuildKit: the client inside the sandbox serves the build's files.
		if len(seg) == 1 && post {
			return nil
		}
	case "build":
		switch {
		case len(seg) == 1 && post:
			return p.checkBuild(r)
		case len(seg) == 2 && seg[1] == "cancel" && post:
			return nil
		}
	case "containers":
		return p.checkContainers(ctx, r, seg, denied)
	case "exec":
		if len(seg) == 3 && ((get && seg[2] == "json") || (post && (seg[2] == "start" || seg[2] == "resize"))) {
			return p.ownExec(ctx, seg[1])
		}
	case "commit":
		if len(seg) == 1 && post {
			return p.checkCommit(ctx, r)
		}
	case "images":
		return p.checkImages(ctx, r, seg, denied)
	case "networks":
		return p.checkNetworks(ctx, r, seg, denied)
	case "volumes":
		return p.checkVolumes(ctx, r, seg, denied)
	case "distribution":
		if len(seg) >= 3 && seg[len(seg)-1] == "json" && get {
			return nil
		}
	}
	return denied
}

func (p *Proxy) checkContainers(ctx context.Context, r *http.Request, seg []string, denied error) error {
	m := r.Method
	if len(seg) == 2 {
		switch {
		case seg[1] == "json" && m == http.MethodGet:
			return p.scopeFilters(r)
		case seg[1] == "create" && m == http.MethodPost:
			return p.createContainer(ctx, r)
		case seg[1] == "prune" && m == http.MethodPost:
			return p.scopeFilters(r)
		case m == http.MethodDelete:
			return p.rewriteContainer(ctx, r, seg)
		}
		return denied
	}
	action := strings.Join(seg[2:], "/")
	allowed := map[string][]string{
		http.MethodGet:  {"json", "top", "logs", "changes", "export", "stats", "archive", "attach/ws"},
		http.MethodHead: {"archive"},
		http.MethodPut:  {"archive"},
		http.MethodPost: {"start", "stop", "restart", "kill", "pause", "unpause", "rename", "wait", "resize", "attach", "update", "exec"},
	}
	ok := false
	for _, a := range allowed[m] {
		ok = ok || a == action
	}
	if !ok {
		return denied
	}
	if err := p.rewriteContainer(ctx, r, seg); err != nil {
		return err
	}
	switch action {
	case "update":
		return p.checkUpdate(r)
	case "exec":
		return p.checkExec(r)
	}
	return nil
}

// rewriteContainer checks the container seg[1] names is this agent's and
// names it by its full ID, so a name or prefix cannot come to mean another.
func (p *Proxy) rewriteContainer(ctx context.Context, r *http.Request, seg []string) error {
	id, err := p.ownContainer(ctx, seg[1])
	if err != nil {
		return err
	}
	r.URL.Path = strings.Replace(r.URL.Path, "/containers/"+seg[1], "/containers/"+id, 1)
	return nil
}

func (p *Proxy) checkImages(ctx context.Context, r *http.Request, seg []string, denied error) error {
	m := r.Method
	get := m == http.MethodGet || m == http.MethodHead
	if len(seg) == 2 {
		switch {
		case get && (seg[1] == "json" || seg[1] == "search" || seg[1] == "get"):
			return nil
		case m == http.MethodPost && (seg[1] == "create" || seg[1] == "load"):
			return nil
		case m == http.MethodPost && seg[1] == "prune":
			return p.scopeFilters(r)
		}
	}
	if m == http.MethodDelete && len(seg) >= 2 {
		return p.ownImage(ctx, strings.Join(seg[1:], "/"))
	}
	if len(seg) >= 3 {
		switch action := seg[len(seg)-1]; {
		case get && (action == "json" || action == "history" || action == "get"):
			return nil
		case m == http.MethodPost && action == "tag":
			return nil
		}
	}
	return denied
}

func (p *Proxy) checkNetworks(ctx context.Context, r *http.Request, seg []string, denied error) error {
	m := r.Method
	switch {
	case len(seg) == 1 && m == http.MethodGet:
		return p.scopeFilters(r)
	case len(seg) == 2 && seg[1] == "create" && m == http.MethodPost:
		return p.createNetwork(r)
	case len(seg) == 2 && seg[1] == "prune" && m == http.MethodPost:
		return p.scopeFilters(r)
	case len(seg) == 2 && m == http.MethodGet:
		if predefinedNetwork(seg[1]) {
			return nil
		}
		return p.ownNetwork(ctx, seg[1])
	case len(seg) == 2 && m == http.MethodDelete:
		return p.ownNetwork(ctx, seg[1])
	case len(seg) == 3 && m == http.MethodPost && (seg[2] == "connect" || seg[2] == "disconnect"):
		if err := p.ownNetwork(ctx, seg[1]); err != nil {
			return err
		}
		body, err := readObject(r)
		if err != nil {
			return err
		}
		if err := body.normalize([]string{"Container", "EndpointConfig", "Force"}, ""); err != nil {
			return err
		}
		var container string
		if _, err := body.decode("Container", &container); err != nil {
			return err
		}
		id, err := p.ownContainer(ctx, container)
		if err != nil {
			return err
		}
		if err := body.set("Container", id); err != nil {
			return err
		}
		if seg[2] == "connect" {
			endpoint, err := body.child("EndpointConfig")
			if err != nil {
				return err
			}
			if err := p.checkEndpoint(ctx, endpoint); err != nil {
				return err
			}
			if err := body.set("EndpointConfig", endpoint); err != nil {
				return err
			}
		}
		return setBody(r, body)
	}
	return denied
}

func (p *Proxy) checkVolumes(ctx context.Context, r *http.Request, seg []string, denied error) error {
	m := r.Method
	switch {
	case len(seg) == 1 && m == http.MethodGet:
		return p.scopeFilters(r)
	case len(seg) == 2 && seg[1] == "create" && m == http.MethodPost:
		return p.createVolume(r)
	case len(seg) == 2 && seg[1] == "prune" && m == http.MethodPost:
		return p.scopeFilters(r)
	case len(seg) == 2 && (m == http.MethodGet || m == http.MethodDelete):
		return p.ownVolume(ctx, seg[1])
	}
	return denied
}

// scopeFilters narrows a list, an event stream or a prune to what carries
// this execution's label.
func (p *Proxy) scopeFilters(r *http.Request) error {
	q := r.URL.Query()
	filters := map[string][]string{}
	for _, raw := range q["filters"] {
		var lists map[string][]string
		if err := json.Unmarshal([]byte(raw), &lists); err == nil {
			for k, v := range lists {
				filters[k] = append(filters[k], v...)
			}
			continue
		}
		var sets map[string]map[string]bool
		if err := json.Unmarshal([]byte(raw), &sets); err != nil {
			return refuse("filters: %v", err)
		}
		for k, v := range sets {
			for value, on := range v {
				if on {
					filters[k] = append(filters[k], value)
				}
			}
		}
	}
	filters["label"] = append(filters["label"], Label+"="+p.opts.Execution)
	data, _ := json.Marshal(filters)
	q.Del("filters")
	q.Set("filters", string(data))
	r.URL.RawQuery = q.Encode()
	return nil
}

// readObject reads a request's JSON body as an object; an empty one for no
// body.
func readObject(r *http.Request) (*object, error) {
	if r.Body == nil {
		return &object{}, nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	r.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(data) > maxBody {
		return nil, refuse("the request body is too large to check")
	}
	o, err := parseObject(data)
	if err != nil {
		return nil, refuse("the request body: %v", err)
	}
	if o == nil {
		o = &object{}
	}
	return o, nil
}

func setBody(r *http.Request, body *object) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Del("Transfer-Encoding")
	r.TransferEncoding = nil
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	return nil
}

// labels adds this execution's label to the labels in field of o.
func (p *Proxy) labels(o *object, field string) error {
	labels := map[string]string{}
	if _, err := o.decode(field, &labels); err != nil {
		return err
	}
	if labels == nil {
		labels = map[string]string{}
	}
	labels[Label] = p.opts.Execution
	return o.set(field, labels)
}

// upstreamJSON reads a GET from the daemon; the status, and v filled on 200.
func (p *Proxy) upstreamJSON(ctx context.Context, method, path string, body any, v any) (int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("the Docker daemon cannot be reached: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 && v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (p *Proxy) mine(labels map[string]string) bool {
	return labels[Label] == p.opts.Execution
}

// ownContainer is the full ID of the container ref names, when it is this
// agent's.
func (p *Proxy) ownContainer(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", notFound("container", ref)
	}
	var c struct {
		ID     string `json:"Id"`
		Config struct {
			Labels map[string]string
		}
	}
	status, err := p.upstreamJSON(ctx, http.MethodGet, "/containers/"+url.PathEscape(ref)+"/json", nil, &c)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || !p.mine(c.Config.Labels) || c.ID == "" {
		return "", notFound("container", ref)
	}
	return c.ID, nil
}

func (p *Proxy) ownExec(ctx context.Context, id string) error {
	var e struct{ ContainerID string }
	status, err := p.upstreamJSON(ctx, http.MethodGet, "/exec/"+url.PathEscape(id)+"/json", nil, &e)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return notFound("exec instance", id)
	}
	if _, err := p.ownContainer(ctx, e.ContainerID); err != nil {
		return notFound("exec instance", id)
	}
	return nil
}

func (p *Proxy) ownNetwork(ctx context.Context, ref string) error {
	var n struct{ Labels map[string]string }
	status, err := p.upstreamJSON(ctx, http.MethodGet, "/networks/"+url.PathEscape(ref), nil, &n)
	if err != nil {
		return err
	}
	if status != http.StatusOK || !p.mine(n.Labels) {
		return notFound("network", ref)
	}
	return nil
}

func (p *Proxy) ownVolume(ctx context.Context, name string) error {
	var v struct{ Labels map[string]string }
	status, err := p.upstreamJSON(ctx, http.MethodGet, "/volumes/"+url.PathEscape(name), nil, &v)
	if err != nil {
		return err
	}
	if status != http.StatusOK || !p.mine(v.Labels) {
		return notFound("volume", name)
	}
	return nil
}

// ownImage allows removing only an image this execution built (or
// committed), which carries its label; images are otherwise shared.
func (p *Proxy) ownImage(ctx context.Context, ref string) error {
	var img struct {
		Config struct{ Labels map[string]string }
	}
	status, err := p.upstreamJSON(ctx, http.MethodGet, "/images/"+ref+"/json", nil, &img)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return notFound("image", ref)
	}
	if !p.mine(img.Config.Labels) {
		return refuse("image %s is shared: only images this agent built can be removed", ref)
	}
	return nil
}

func predefinedNetwork(name string) bool {
	return name == "bridge" || name == "none" || name == "default"
}
