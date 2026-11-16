package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/mcpupstream"
)

// mcpLocalSeparator joins a connection and a tool into the name an agent
// sees. Connection names have no underscore, so it is unambiguous; local
// and upstream names never mix.
const mcpLocalSeparator = "__"

// mcpLocalNameMax is the longest tool name agent tools accept.
const mcpLocalNameMax = 64

// mcpCatalogTTL is how long a listing is trusted without list_changed.
const mcpCatalogTTL = 5 * time.Minute

// MCPDenied is a call the connection or its grants refused (audited). An
// ApprovalID means the operator was asked and has not allowed it yet.
type MCPDenied struct {
	Reason     string
	ApprovalID string
}

func (e *MCPDenied) Error() string {
	if e.ApprovalID != "" {
		return e.Reason + " (approval " + e.ApprovalID + ")"
	}
	return e.Reason
}

// MCPBrokerTool is an upstream tool as an agent is offered it.
type MCPBrokerTool struct {
	// Name is the local name, <connection>__<tool>.
	Name         string          `json:"name"`
	Connection   string          `json:"connection"`
	Tool         string          `json:"tool"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

// MCPCatalogProblem is a tool or connection Legato does not offer, and why.
type MCPCatalogProblem struct {
	Connection string `json:"connection"`
	Tool       string `json:"tool,omitempty"`
	Reason     string `json:"reason"`
}

// MCPCallResult is a tool's answer, the server's own words (untrusted).
type MCPCallResult struct {
	IsError bool
	Raw     json.RawMessage
}

// MCPBroker lists and calls upstream MCP tools for agents: one client and
// one authorization per connection, shared by every permitted execution,
// each call authorized on its own through MCPGrants. Agents never see a
// token; Legato's own token source gives it to the client.
type MCPBroker struct {
	conns  *MCPConnectionService
	grants *MCPGrants
	flows  *OAuthFlowService
	mu     sync.Mutex
	conn   map[string]*mcpBrokerConn
}

type mcpBrokerConn struct {
	revision int64
	client   *mcpupstream.Client
	mu       sync.Mutex
	catalog  *mcpupstream.Catalog
	fetched  time.Time
	stale    bool
}

// NewMCPBroker makes a broker. flows may be nil when only anonymous
// connections are used.
func NewMCPBroker(conns *MCPConnectionService, grants *MCPGrants, flows *OAuthFlowService) *MCPBroker {
	return &MCPBroker{conns: conns, grants: grants, flows: flows, conn: map[string]*mcpBrokerConn{}}
}

// Close ends every upstream session.
func (b *MCPBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for name, c := range b.conn {
		c.client.Close()
		delete(b.conn, name)
	}
}

type mcpTokens struct {
	flows      *OAuthFlowService
	connection string
}

func (t mcpTokens) Token(ctx context.Context, force bool) (string, error) {
	if force {
		return t.flows.RefreshAccessToken(ctx, t.connection)
	}
	return t.flows.AccessToken(ctx, t.connection)
}

// client is the connection's client at its current revision: a changed
// connection gets a new one.
func (b *MCPBroker) client(ctx context.Context, name string) (*mcpBrokerConn, error) {
	st, err := b.conns.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	policy, err := b.conns.EgressPolicy(ctx, name)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if c, ok := b.conn[name]; ok {
		if c.revision == st.Revision {
			return c, nil
		}
		c.client.Close()
		delete(b.conn, name)
	}
	c := &mcpBrokerConn{revision: st.Revision}
	opts := mcpupstream.Options{Endpoint: st.URL, HTTP: brokerHTTP(policy), OnToolsChanged: func() {
		c.mu.Lock()
		c.stale = true
		c.mu.Unlock()
	}}
	if st.Auth == "oauth" {
		if b.flows == nil {
			return nil, ErrOAuthNotAuthorized
		}
		opts.Tokens = mcpTokens{flows: b.flows, connection: name}
	}
	c.client = mcpupstream.New(opts)
	b.conn[name] = c
	return c, nil
}

func brokerHTTP(p egress.Policy) *http.Client { return p.Client() }

// catalog lists the connection's tools when the last listing is old or the
// server said they changed. A tool whose definition changed, or that went
// away, ends the connection's grants.
func (b *MCPBroker) catalog(ctx context.Context, name string, c *mcpBrokerConn, force bool) (*mcpupstream.Catalog, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.catalog != nil && !c.stale && time.Since(c.fetched) < mcpCatalogTTL {
		return c.catalog, nil
	}
	cat, err := c.client.Tools(ctx)
	b.recordContact(ctx, name, c.revision, err)
	if err != nil {
		return nil, err
	}
	hashes := make(map[string]string, len(cat.Tools))
	for _, t := range cat.Tools {
		hashes[t.Name] = t.Hash
	}
	changed, err := b.grants.db.RecordMCPTools(ctx, name, hashes)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := b.grants.EndGrants(ctx, name); err != nil {
			return nil, err
		}
	}
	c.catalog, c.fetched, c.stale = cat, time.Now(), false
	return cat, nil
}

// Refresh lists a connection's tools again now.
func (b *MCPBroker) Refresh(ctx context.Context, name string) error {
	c, err := b.client(ctx, name)
	if err != nil {
		return err
	}
	_, err = b.catalog(ctx, name, c, true)
	return err
}

func (b *MCPBroker) recordContact(ctx context.Context, name string, revision int64, err error) {
	code := ""
	switch {
	case err == nil:
	case errors.Is(err, mcpupstream.ErrUnauthorized), errors.Is(err, ErrOAuthReauthRequired), errors.Is(err, ErrOAuthNotAuthorized):
		code = "unauthorized"
	case errors.Is(err, mcpupstream.ErrTooLarge), errors.Is(err, egress.ErrTooLarge):
		code = "too_large"
	case errors.Is(err, mcpupstream.ErrInvalidResult):
		code = "protocol"
	case errors.Is(err, mcpupstream.ErrInvalidArguments), errors.Is(err, context.Canceled):
		return // the caller's, not the server's
	default:
		code = egress.Classify(err)
	}
	_ = b.conns.RecordContact(context.WithoutCancel(ctx), name, revision, code)
}

// ListTools is what a caller is offered: for each connection usable by it,
// the tools a rule could let it call, under local names. Tools Legato will
// not offer (invalid schemas, names too long) and connections it could not
// list are reported, not hidden in silence.
func (b *MCPBroker) ListTools(ctx context.Context, caller MCPCaller) ([]MCPBrokerTool, []MCPCatalogProblem, error) {
	all, err := b.conns.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	var tools []MCPBrokerTool
	var problems []MCPCatalogProblem
	for _, st := range all {
		if use, err := b.conns.Usable(ctx, st.Name, caller); err != nil || !use.Usable {
			continue
		}
		c, err := b.client(ctx, st.Name)
		if err != nil {
			problems = append(problems, MCPCatalogProblem{Connection: st.Name, Reason: safeMCPError(err)})
			continue
		}
		cat, err := b.catalog(ctx, st.Name, c, false)
		if err != nil {
			problems = append(problems, MCPCatalogProblem{Connection: st.Name, Reason: safeMCPError(err)})
			continue
		}
		for _, r := range cat.Rejected {
			problems = append(problems, MCPCatalogProblem{Connection: st.Name, Tool: r.Name, Reason: r.Reason})
		}
		if cat.Truncated {
			problems = append(problems, MCPCatalogProblem{Connection: st.Name, Reason: "the server offers more tools than Legato lists"})
		}
		for _, t := range cat.Tools {
			local := st.Name + mcpLocalSeparator + t.Name
			if len(local) > mcpLocalNameMax {
				problems = append(problems, MCPCatalogProblem{Connection: st.Name, Tool: t.Name, Reason: "the tool's name is too long for agent tools"})
				continue
			}
			ok, err := b.grants.Visible(ctx, st.Name, caller, t.Name)
			if err != nil {
				return nil, nil, err
			}
			if !ok {
				continue
			}
			tools = append(tools, MCPBrokerTool{Name: local, Connection: st.Name, Tool: t.Name, Title: t.Title,
				Description: "[" + st.Name + "] " + t.Description, InputSchema: t.InputSchema, OutputSchema: t.OutputSchema})
		}
	}
	return tools, problems, nil
}

func splitMCPLocal(local string) (string, string, bool) {
	conn, tool, ok := strings.Cut(local, mcpLocalSeparator)
	if !ok || !mcpConnectionName.MatchString(conn) || !mcpToolName.MatchString(tool) {
		return "", "", false
	}
	return conn, tool, true
}

// CallTool authorizes and makes one call. It is authorized on its own,
// whatever the agent was offered before. A call that may have run is never
// sent again; one to a tool the operator called read-only is, once, when the
// server ended its session.
func (b *MCPBroker) CallTool(ctx context.Context, caller MCPCaller, local string, args json.RawMessage) (*MCPCallResult, error) {
	name, tool, ok := splitMCPLocal(local)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not an MCP broker tool", ErrMCPToolName, local)
	}
	decision, err := b.grants.Authorize(ctx, name, caller, tool)
	if err != nil {
		return nil, err
	}
	if !decision.Allowed {
		return nil, &MCPDenied{Reason: decision.Reason, ApprovalID: decision.ApprovalID}
	}
	c, err := b.client(ctx, name)
	if err != nil {
		return nil, errors.New(safeMCPError(err))
	}
	cat, err := b.catalog(ctx, name, c, false)
	if err != nil {
		return nil, errors.New(safeMCPError(err))
	}
	t := cat.Find(tool)
	if t == nil {
		if cat, err = b.catalog(ctx, name, c, true); err != nil {
			return nil, errors.New(safeMCPError(err))
		}
		if t = cat.Find(tool); t == nil {
			return nil, &MCPDenied{Reason: "the server no longer offers " + tool}
		}
	}
	var result *mcpupstream.Result
	if b.grants.operation(ctx, name, tool) == mcpRead {
		result, err = c.client.CallRead(ctx, t, args)
	} else {
		result, err = c.client.Call(ctx, t, args)
	}
	b.recordContact(ctx, name, c.revision, err)
	if err != nil {
		return nil, errors.New(safeMCPError(err))
	}
	return &MCPCallResult{IsError: result.IsError, Raw: result.Raw}, nil
}

// safeMCPError is what an agent or the operator may read of a failure: the
// client's own messages, never a token or a response body.
func safeMCPError(err error) string {
	switch {
	case errors.Is(err, mcpupstream.ErrInvalidArguments), errors.Is(err, mcpupstream.ErrInvalidResult),
		errors.Is(err, mcpupstream.ErrTooLarge), errors.Is(err, mcpupstream.ErrSessionEnded), errors.Is(err, mcpupstream.ErrUnauthorized),
		errors.Is(err, ErrOAuthReauthRequired), errors.Is(err, ErrOAuthNotAuthorized), errors.Is(err, ErrOAuthRefreshUnavailable):
		return err.Error()
	case errors.Is(err, egress.ErrDenied):
		return "Legato's network policy does not allow this server's address"
	case errors.Is(err, context.DeadlineExceeded):
		return "the MCP server did not answer in time"
	}
	switch egress.Classify(err) {
	case "tls":
		return "the MCP server's certificate was not trusted"
	default:
		return "the MCP server could not be reached"
	}
}
