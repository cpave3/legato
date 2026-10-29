package credential

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

// KeyPlace is where an HTTP API takes its key: a header, written with
// Format ("Bearer {secret}"; "{secret}" when empty), or a query parameter.
type KeyPlace struct {
	Header string `yaml:"header" json:"header,omitempty"`
	Format string `yaml:"format" json:"format,omitempty"`
	Query  string `yaml:"query" json:"query,omitempty"`
}

// Validate checks that a key place names one header or one query parameter.
func (k KeyPlace) Validate() error {
	switch {
	case k.Header != "" && k.Query != "":
		return errors.New("the key goes in a header or a query parameter, not both")
	case k.Header == "" && k.Query == "":
		return errors.New("the key needs a place: a header or a query parameter")
	case k.Format != "" && !strings.Contains(k.Format, "{secret}"):
		return errors.New(`the key's format must contain {secret}`)
	case k.Format != "" && k.Query != "":
		return errors.New("a query parameter takes the key as it is, without a format")
	}
	return nil
}

func (k KeyPlace) format() string {
	if k.Format == "" {
		return "{secret}"
	}
	return k.Format
}

// ProxyCredential is one credential the proxy serves under /c/<Name>/.
type ProxyCredential struct {
	Name      string
	Upstream  *url.URL
	Key       KeyPlace
	Inspector RestInspector
}

// ProxyUse is one proxied request, classified.
type ProxyUse struct {
	Execution
	Credential string
	Method     string
	Path       string
	Classified
}

// Decision says whether a use a channel saw may go ahead.
type Decision struct {
	Allowed bool
	// Pending: the person was asked and has not answered yet.
	Pending    bool
	ApprovalID string
	Reason     string
}

// ProxyBackend is what the proxy asks of Legato: whose a placeholder is,
// whether a use is allowed, the key, and what a use measured after it ended.
type ProxyBackend interface {
	ResolvePlaceholder(ctx context.Context, placeholder string) (credential string, execution Execution, ok bool, err error)
	Authorize(ctx context.Context, use ProxyUse) (Decision, error)
	Key(ctx context.Context, credential string) ([]byte, error)
	Measured(ctx context.Context, use ProxyUse, amounts map[string]int64)
}

// HTTPProxy serves the http-proxy channel. An agent sends plain HTTP to
// loopback with a placeholder in the key's place; the proxy checks the use,
// puts the real key in, and forwards the request to the credential's fixed
// upstream over TLS. It never follows a redirect.
type HTTPProxy struct {
	Credentials map[string]ProxyCredential
	Backend     ProxyBackend
	// Transport reaches the upstreams. Nil: http.DefaultTransport, which
	// checks certificates against the system's roots.
	Transport http.RoundTripper
}

var hopHeaders = []string{"Connection", "Keep-Alive", "Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func writeJSON(w http.ResponseWriter, status int, body map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Only origin-form request lines: a proxy request names another host.
	if r.Method == http.MethodConnect || !strings.HasPrefix(r.RequestURI, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "this is not a forward proxy; send requests to /c/<credential>/ on this address"})
		return
	}
	name, rest, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/c/"), "/")
	c, known := p.Credentials[name]
	if !strings.HasPrefix(r.URL.Path, "/c/") || !ok || !known {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no credential is served at " + r.URL.Path})
		return
	}
	path := "/" + rest

	placeholder := c.placeholderIn(r)
	owner, execution, found, err := p.Backend.ResolvePlaceholder(ctx, placeholder)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Legato could not check the key: " + err.Error()})
		return
	}
	if placeholder == "" || !found || owner != name {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "this request carries no key Legato gave this agent for " + name})
		return
	}

	route, _, matched := c.Inspector.Match(r.Method, path)
	if !matched {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "no route of " + name + " allows this request", "method": strings.ToUpper(r.Method), "path": path})
		return
	}
	var body []byte
	if _, metered := route.Meter["characters"]; metered {
		body, err = io.ReadAll(io.LimitReader(r.Body, MaxMeteredBody+1))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reading the request body: " + err.Error()})
			return
		}
	}
	classified, err := c.Inspector.Classify(ctx, Call{Channel: "http-proxy", Fields: map[string]string{"method": r.Method, "path": path}, Body: body})
	if errors.Is(err, ErrBodyTooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	requestBytes := r.ContentLength
	if body != nil {
		requestBytes = int64(len(body))
	}
	if requestBytes < 0 {
		requestBytes = 0
	}
	classified.Amounts["requests"] = 1
	classified.Amounts["bytes"] = requestBytes
	use := ProxyUse{Execution: execution, Credential: name, Method: strings.ToUpper(r.Method), Path: path, Classified: classified}

	decision, err := p.Backend.Authorize(ctx, use)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Legato could not check this use: " + err.Error()})
		return
	}
	if !decision.Allowed {
		if decision.Pending {
			writeJSON(w, http.StatusForbidden, map[string]string{"approval": decision.ApprovalID, "status": "pending",
				"error": "the person has been asked to allow " + use.Operation + " on " + use.Resource + "; Legato will message you when they decide"})
			return
		}
		out := map[string]string{"error": firstOr(decision.Reason, "not allowed"), "operation": use.Operation, "resource": use.Resource, "status": "denied"}
		if decision.ApprovalID != "" {
			out["approval"] = decision.ApprovalID
		}
		writeJSON(w, http.StatusForbidden, out)
		return
	}

	key, err := p.Backend.Key(ctx, name)
	if err != nil {
		log.Printf("credentials: proxy %s: reading the key: %v", name, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Legato could not read the key of " + name})
		return
	}
	out, err := c.outgoing(ctx, r, path, body, placeholder, key)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	transport := p.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	counter := &countingReader{r: out.Body}
	if out.Body != nil {
		out.Body = io.NopCloser(counter)
	}
	resp, err := transport.RoundTrip(out)
	if err != nil {
		log.Printf("credentials: proxy %s: %v", name, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the upstream of " + name + " could not be reached"})
		return
	}
	defer resp.Body.Close()

	keyText := string(key)
	for header, values := range resp.Header {
		if isHop(header) {
			continue
		}
		for _, v := range values {
			if keyText != "" && strings.Contains(v, keyText) {
				continue
			}
			w.Header().Add(header, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	written, _ := copyFlushing(w, resp.Body)
	// The request's bytes were part of the use; a streamed body's length
	// was not known then.
	measured := written
	if body == nil && requestBytes == 0 {
		measured += counter.n
	}
	p.Backend.Measured(context.WithoutCancel(ctx), use, map[string]int64{"bytes": measured})
}

func firstOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func isHop(header string) bool {
	for _, h := range hopHeaders {
		if strings.EqualFold(h, header) {
			return true
		}
	}
	return strings.HasPrefix(strings.ToLower(header), "proxy-")
}

// placeholderIn reads the key's place of a request, without the format.
func (c ProxyCredential) placeholderIn(r *http.Request) string {
	if c.Key.Query != "" {
		return r.URL.Query().Get(c.Key.Query)
	}
	value := r.Header.Get(c.Key.Header)
	before, after, _ := strings.Cut(c.Key.format(), "{secret}")
	if !strings.HasPrefix(value, before) || !strings.HasSuffix(value, after) || len(value) < len(before)+len(after) {
		return ""
	}
	return value[len(before) : len(value)-len(after)]
}

// outgoing is the request to the upstream: the agent's method, path, query,
// headers and body, without hop-by-hop headers, Proxy-* headers or the
// placeholder, and with the real key.
func (c ProxyCredential) outgoing(ctx context.Context, r *http.Request, path string, body []byte, placeholder string, key []byte) (*http.Request, error) {
	target := *c.Upstream
	target.Path = strings.TrimSuffix(c.Upstream.Path, "/") + path
	target.RawPath = ""
	query := r.URL.Query()
	for name, values := range query {
		kept := values[:0]
		for _, v := range values {
			if !strings.Contains(v, placeholder) {
				kept = append(kept, v)
			}
		}
		if len(kept) == 0 {
			query.Del(name)
		} else {
			query[name] = kept
		}
	}
	if c.Key.Query != "" {
		query.Set(c.Key.Query, string(key))
	}
	target.RawQuery = query.Encode()

	var reader io.Reader = r.Body
	if body != nil {
		reader = bytes.NewReader(body)
	}
	out, err := http.NewRequestWithContext(ctx, r.Method, target.String(), reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		out.ContentLength = int64(len(body))
	} else {
		out.ContentLength = r.ContentLength
		if r.ContentLength == 0 {
			out.Body = nil
		}
	}
	connection := r.Header.Values("Connection")
	for header, values := range r.Header {
		if isHop(header) || listedIn(connection, header) || (c.Key.Header != "" && strings.EqualFold(header, c.Key.Header)) {
			continue
		}
		for _, v := range values {
			if !strings.Contains(v, placeholder) {
				out.Header.Add(header, v)
			}
		}
	}
	if c.Key.Header != "" {
		out.Header.Set(c.Key.Header, strings.ReplaceAll(c.Key.format(), "{secret}", string(key)))
	}
	return out, nil
}

// listedIn reports whether a Connection header names a header.
func listedIn(connection []string, header string) bool {
	for _, line := range connection {
		for _, name := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(name), header) {
				return true
			}
		}
	}
	return false
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// copyFlushing streams a response, flushing each piece so audio arrives as
// the upstream makes it.
func copyFlushing(w http.ResponseWriter, r io.Reader) (int64, error) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			written, werr := w.Write(buf[:n])
			total += int64(written)
			if werr != nil {
				return total, werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}
