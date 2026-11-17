// Package mcpupstream is a bounded, validating client of one MCP server, over
// Streamable HTTP or a transport the caller gives it (a local server's pipes),
// on the official Go SDK. It lists the
// server's tools, keeping only those whose names and JSON Schema 2020-12
// schemas it can check (no remote references) and reporting the rest, and
// calls them with arguments and structured results checked against those
// schemas, within size, time and concurrency bounds. It offers the server no
// roots, sampling or elicitation, follows no URL the server returns, and
// never sends a call again when it cannot know whether the server ran it.
// It knows nothing of Legato: the caller gives it an HTTP client (with its
// egress policy) and a token source.
package mcpupstream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	// ErrInvalidArguments is a call whose arguments the tool's schema refuses.
	ErrInvalidArguments = errors.New("mcpupstream: the arguments do not match the tool's input schema")
	// ErrInvalidResult is a structured result the tool's output schema refuses.
	ErrInvalidResult = errors.New("mcpupstream: the server's result does not match the tool's output schema")
	// ErrTooLarge is a request or result past the client's bounds.
	ErrTooLarge = errors.New("mcpupstream: too large")
	// ErrSessionEnded is a call whose session the server ended before
	// answering: the call may or may not have run, so it is not sent again.
	ErrSessionEnded = errors.New("mcpupstream: the server ended the session; the call may not have run, try again")
	// ErrUnauthorized is a server that refused a fresh token too.
	ErrUnauthorized = errors.New("mcpupstream: the server refused Legato's authorization")
)

// TokenSource gives the bearer token to send; force asks for a new one after
// the server refused the one in hand. nil means an anonymous server.
type TokenSource interface {
	Token(ctx context.Context, force bool) (string, error)
}

// Limits bound what the client accepts and sends.
type Limits struct {
	MaxTools          int
	MaxSchemaBytes    int
	MaxDescription    int
	MaxArgumentBytes  int
	MaxResultBytes    int
	CallTimeout       time.Duration
	MaxConcurrentCall int
}

// DefaultLimits are used for any zero field.
var DefaultLimits = Limits{MaxTools: 256, MaxSchemaBytes: 64 << 10, MaxDescription: 2048, MaxArgumentBytes: 256 << 10, MaxResultBytes: 1 << 20, CallTimeout: 60 * time.Second, MaxConcurrentCall: 4}

// Options configure a client.
type Options struct {
	Endpoint string
	HTTP     *http.Client
	Tokens   TokenSource
	// Connect, when set, gives the transport of each new session in place
	// of Streamable HTTP to Endpoint (a local server's pipes); HTTP and
	// Tokens are not used.
	Connect func(ctx context.Context) (mcp.Transport, error)
	// AcceptDraft07 takes tools whose schemas declare JSON Schema draft-07
	// (as servers built on the TypeScript SDK do), validated as draft-07;
	// otherwise only 2020-12 is taken. Remote references are refused either way.
	AcceptDraft07 bool
	Limits        Limits
	// OnToolsChanged is told when the server says its tools changed.
	OnToolsChanged func()
}

// Tool is one tool the client can call. Description and annotations are
// the server's words: untrusted text, never permission.
type Tool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	ReadOnlyHint bool            `json:"-"`
	// Hash changes when any of the tool's definition does.
	Hash   string `json:"-"`
	input  *jsonschema.Resolved
	output *jsonschema.Resolved
}

// Rejected is a tool the client will not offer, and why.
type Rejected struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Catalog is what listing found.
type Catalog struct {
	Tools     []Tool
	Rejected  []Rejected
	Truncated bool
}

// Find is the tool of that name, or nil.
func (c *Catalog) Find(name string) *Tool {
	for i := range c.Tools {
		if c.Tools[i].Name == name {
			return &c.Tools[i]
		}
	}
	return nil
}

// Result is a tool's answer, as the server sent it (bounded and checked).
type Result struct {
	IsError bool
	Raw     json.RawMessage
}

// Client is a client of one MCP server.
type Client struct {
	opts    Options
	limits  Limits
	calls   chan struct{}
	mu      sync.Mutex
	session *mcp.ClientSession
	refused atomic.Bool
}

// New makes a client; it connects on first use.
func New(opts Options) *Client {
	l := opts.Limits
	d := DefaultLimits
	if l.MaxTools <= 0 {
		l.MaxTools = d.MaxTools
	}
	if l.MaxSchemaBytes <= 0 {
		l.MaxSchemaBytes = d.MaxSchemaBytes
	}
	if l.MaxDescription <= 0 {
		l.MaxDescription = d.MaxDescription
	}
	if l.MaxArgumentBytes <= 0 {
		l.MaxArgumentBytes = d.MaxArgumentBytes
	}
	if l.MaxResultBytes <= 0 {
		l.MaxResultBytes = d.MaxResultBytes
	}
	if l.CallTimeout <= 0 {
		l.CallTimeout = d.CallTimeout
	}
	if l.MaxConcurrentCall <= 0 {
		l.MaxConcurrentCall = d.MaxConcurrentCall
	}
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: l.CallTimeout}
	}
	return &Client{opts: opts, limits: l, calls: make(chan struct{}, l.MaxConcurrentCall)}
}

// Close ends the session.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropLocked()
}

func (c *Client) dropLocked() {
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// sessionFor returns the session, connecting with a token (a new one when force).
func (c *Client) sessionFor(ctx context.Context, force bool) (*mcp.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if force {
		c.dropLocked()
	}
	if c.session != nil {
		return c.session, nil
	}
	if c.opts.Connect != nil {
		transport, err := c.opts.Connect(ctx)
		if err != nil {
			return nil, err
		}
		session, err := c.newClient().Connect(ctx, transport, nil)
		if err != nil {
			return nil, err
		}
		c.session = session
		return session, nil
	}
	token := ""
	if c.opts.Tokens != nil {
		var err error
		if token, err = c.opts.Tokens.Token(ctx, force); err != nil {
			return nil, err
		}
	}
	base := c.opts.HTTP.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	hc := *c.opts.HTTP
	hc.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := base.RoundTrip(r)
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			c.refused.Store(true)
		}
		return resp, err
	})
	client := c.newClient()
	c.refused.Store(false)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: c.opts.Endpoint, HTTPClient: &hc, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return nil, err
	}
	c.session = session
	return session, nil
}

func (c *Client) newClient() *mcp.Client {
	return mcp.NewClient(&mcp.Implementation{Name: "legato", Version: "1"}, &mcp.ClientOptions{
		// No roots, no sampling, no elicitation: nothing is advertised.
		Capabilities:   &mcp.ClientCapabilities{},
		MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			if c.opts.OnToolsChanged != nil {
				c.opts.OnToolsChanged()
			}
		},
	})
}

// with runs fn on a session. A refusal (401) gets a new token and runs fn
// again once: the server did not take the request. A session the server
// ended is reopened and fn run again only when again is true (listing, or
// a call the caller knows to be safe to repeat).
func (c *Client) with(ctx context.Context, again bool, fn func(*mcp.ClientSession) error) error {
	force := false
	for attempt := 0; ; attempt++ {
		session, err := c.sessionFor(ctx, force)
		if err == nil {
			err = fn(session)
		}
		if err == nil {
			return nil
		}
		switch {
		case c.refused.Load() && c.opts.Tokens != nil && attempt == 0:
			force = true
			continue
		case c.refused.Load():
			c.Close()
			return ErrUnauthorized
		case errors.Is(err, mcp.ErrSessionMissing) || errors.Is(err, mcp.ErrConnectionClosed) || (c.opts.Connect != nil && errors.Is(err, io.EOF)):
			// A local server's pipes end at EOF when its process does.
			c.Close()
			if again && attempt == 0 {
				continue
			}
			return ErrSessionEnded
		}
		return err
	}
}

var toolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

const draft2020 = "https://json-schema.org/draft/2020-12/schema"

var draft07 = map[string]bool{"http://json-schema.org/draft-07/schema": true, "https://json-schema.org/draft-07/schema": true}

// checkSchema parses and resolves a JSON Schema 2020-12 document (or draft-07
// with AcceptDraft07), refusing any other draft, any remote reference and
// anything too large.
func (c *Client) checkSchema(raw any) (json.RawMessage, *jsonschema.Resolved, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, errors.New("the schema is not JSON")
	}
	if len(b) > c.limits.MaxSchemaBytes {
		return nil, nil, fmt.Errorf("the schema is over %d bytes", c.limits.MaxSchemaBytes)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, nil, fmt.Errorf("the schema is not valid JSON Schema: %v", err)
	}
	if s.Schema != "" && strings.TrimSuffix(s.Schema, "#") != draft2020 && !(c.opts.AcceptDraft07 && draft07[strings.TrimSuffix(s.Schema, "#")]) {
		return nil, nil, fmt.Errorf("the schema is %s, not JSON Schema 2020-12", s.Schema)
	}
	// A nil loader refuses every remote reference.
	resolved, err := s.Resolve(&jsonschema.ResolveOptions{})
	if err != nil {
		if bytes.Contains(b, []byte(`"$ref"`)) || bytes.Contains(b, []byte(`"$dynamicRef"`)) {
			return nil, nil, fmt.Errorf("the schema has a reference Legato will not load: %v", err)
		}
		return nil, nil, fmt.Errorf("the schema does not resolve: %v", err)
	}
	return b, resolved, nil
}

func (c *Client) tool(t *mcp.Tool) (Tool, error) {
	if !toolName.MatchString(t.Name) {
		return Tool{}, errors.New("the name is not a valid MCP tool name")
	}
	out := Tool{Name: t.Name, Title: t.Title, Description: t.Description}
	if len(out.Description) > c.limits.MaxDescription {
		out.Description = out.Description[:c.limits.MaxDescription]
	}
	if len(out.Title) > 200 {
		out.Title = out.Title[:200]
	}
	var err error
	if t.InputSchema == nil {
		return Tool{}, errors.New("it has no input schema")
	}
	if out.InputSchema, out.input, err = c.checkSchema(t.InputSchema); err != nil {
		return Tool{}, fmt.Errorf("input schema: %w", err)
	}
	if t.OutputSchema != nil {
		if out.OutputSchema, out.output, err = c.checkSchema(t.OutputSchema); err != nil {
			return Tool{}, fmt.Errorf("output schema: %w", err)
		}
	}
	if t.Annotations != nil {
		out.ReadOnlyHint = t.Annotations.ReadOnlyHint
	}
	def, _ := json.Marshal(struct {
		Name, Title, Description string
		Input, Output            json.RawMessage
		Annotations              *mcp.ToolAnnotations
	}{t.Name, t.Title, t.Description, out.InputSchema, out.OutputSchema, t.Annotations})
	sum := sha256.Sum256(def)
	out.Hash = hex.EncodeToString(sum[:])
	return out, nil
}

// Tools lists the server's tools.
func (c *Client) Tools(ctx context.Context) (*Catalog, error) {
	var cat *Catalog
	err := c.with(ctx, true, func(s *mcp.ClientSession) error {
		cat = &Catalog{}
		seen := 0
		for t, err := range s.Tools(ctx, nil) {
			if err != nil {
				return err
			}
			if seen++; seen > c.limits.MaxTools {
				cat.Truncated = true
				break
			}
			tool, err := c.tool(t)
			if err != nil {
				cat.Rejected = append(cat.Rejected, Rejected{Name: clip(t.Name, 128), Reason: err.Error()})
				continue
			}
			cat.Tools = append(cat.Tools, tool)
		}
		return nil
	})
	return cat, err
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Call calls a tool once. A session the server ended before answering is
// ErrSessionEnded: the call may have run, so it is not sent again.
func (c *Client) Call(ctx context.Context, tool *Tool, args json.RawMessage) (*Result, error) {
	return c.call(ctx, tool, args, false)
}

// CallRead calls a tool the caller knows only reads: it is sent again once
// on a new session if the server ended the old one.
func (c *Client) CallRead(ctx context.Context, tool *Tool, args json.RawMessage) (*Result, error) {
	return c.call(ctx, tool, args, true)
}

func (c *Client) call(ctx context.Context, tool *Tool, args json.RawMessage, again bool) (*Result, error) {
	if tool == nil || tool.input == nil {
		return nil, errors.New("mcpupstream: no such tool in the catalog")
	}
	if len(args) > c.limits.MaxArgumentBytes {
		return nil, fmt.Errorf("%w: the arguments are over %d bytes", ErrTooLarge, c.limits.MaxArgumentBytes)
	}
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	var value any
	if err := json.Unmarshal(args, &value); err != nil {
		return nil, fmt.Errorf("%w: the arguments are not JSON", ErrInvalidArguments)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("%w: the arguments are not an object", ErrInvalidArguments)
	}
	if err := tool.input.Validate(value); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	select {
	case c.calls <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.calls }()
	ctx, cancel := context.WithTimeout(ctx, c.limits.CallTimeout)
	defer cancel()
	var result *mcp.CallToolResult
	err := c.with(ctx, again, func(s *mcp.ClientSession) error {
		var err error
		result, err = s.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: value})
		return err
	})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, errors.New("mcpupstream: the result is not JSON")
	}
	if len(raw) > c.limits.MaxResultBytes {
		return nil, fmt.Errorf("%w: the result is over %d bytes", ErrTooLarge, c.limits.MaxResultBytes)
	}
	if tool.output != nil && !result.IsError {
		if result.StructuredContent == nil {
			return nil, fmt.Errorf("%w: no structured content", ErrInvalidResult)
		}
		var structured any
		b, _ := json.Marshal(result.StructuredContent)
		if err := json.Unmarshal(b, &structured); err != nil {
			return nil, ErrInvalidResult
		}
		if err := tool.output.Validate(structured); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidResult, err)
		}
	}
	return &Result{IsError: result.IsError, Raw: raw}, nil
}
