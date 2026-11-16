package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
)

// ErrMCPToolName is a tool name Legato will not route: MCP tool names are
// 1-128 of A-Z a-z 0-9 _ - . and nothing else.
var ErrMCPToolName = errors.New("invalid MCP tool name")

var mcpToolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

const (
	mcpCredentialPrefix = "mcp:"
	// mcpRead and mcpWrite are the broker operations of a tool call: read
	// for a tool the operator listed as read-only, write for any other.
	mcpRead  = "mcp.read"
	mcpWrite = "mcp.write"
	// mcpNoRepository stands for an agent with no repository.
	mcpNoRepository = "-/-"
)

// mcpProfile is the broker profile of an MCP connection. It is not a shipped
// profile, so no configured credential can claim it; Brokered means the
// broker uses the connection for the agent and never hands out a value.
var mcpProfile = CredentialProfile{Name: "mcp", Description: "An MCP connection Legato calls for agents", Brokered: true, Source: "builtin"}

// MCPCredentialName is the broker credential of a connection.
func MCPCredentialName(connection string) string { return mcpCredentialPrefix + connection }

func validToolPattern(p string) bool {
	return p != "" && !strings.Contains(p, "/") && doublestar.ValidatePattern(p)
}

// encodeMCPRules checks rules and read-only tool patterns and stores them.
func encodeMCPRules(rules []MCPRule, readOnly []string) (string, string, error) {
	invalid := func(format string, args ...any) (string, string, error) {
		return "", "", errors.Join(ErrMCPConnectionInvalid, fmt.Errorf(format, args...))
	}
	for i := range rules {
		r := &rules[i]
		r.Tools, r.Repository, r.Access = strings.TrimSpace(r.Tools), strings.TrimSpace(r.Repository), strings.TrimSpace(r.Access)
		if r.Tools == "" {
			r.Tools = "*"
		}
		if r.Repository == "" {
			r.Repository = "*/*"
		}
		if !validToolPattern(r.Tools) {
			return invalid("rules[%d].tools: a pattern of tool names, without /", i)
		}
		if strings.Count(r.Repository, "/") != 1 || !doublestar.ValidatePattern(r.Repository) {
			return invalid("rules[%d].repository: an owner/name pattern", i)
		}
		if r.Access != "" && r.Access != "read" && r.Access != "write" {
			return invalid("rules[%d].access: read, write or empty", i)
		}
		cr := mcpCredentialRule(*r)
		if err := config.NormalizeCredentialRule(&cr); err != nil {
			return invalid("rules[%d]: %v", i, err)
		}
	}
	for _, p := range readOnly {
		if !validToolPattern(strings.TrimSpace(p)) {
			return invalid("read_only_tools: %q is not a pattern of tool names", p)
		}
	}
	if rules == nil {
		rules = []MCPRule{}
	}
	b, err := json.Marshal(rules)
	if err != nil {
		return "", "", err
	}
	trimmed := make([]string, len(readOnly))
	for i, p := range readOnly {
		trimmed[i] = strings.TrimSpace(p)
	}
	return string(b), strings.Join(trimmed, ","), nil
}

func decodeMCPRules(c store.MCPConnection) ([]MCPRule, []string) {
	rules := []MCPRule{}
	_ = json.Unmarshal([]byte(c.RulesJSON), &rules)
	var readOnly []string
	if c.ReadOnlyTools != "" {
		readOnly = strings.Split(c.ReadOnlyTools, ",")
	}
	return rules, readOnly
}

func mcpCredentialRule(r MCPRule) config.CredentialRule {
	cr := config.CredentialRule{Resource: r.Repository + "/" + r.Tools, Approve: r.Approve, Window: r.Window, Limit: r.Limit}
	switch r.Access {
	case "read":
		cr.Operation = config.StringList{mcpRead}
	case "write":
		cr.Operation = config.StringList{mcpWrite}
	}
	return cr
}

// MCPRules are a connection's rules as broker rules, for CredentialBroker.
func (s *MCPConnectionService) MCPRules(ctx context.Context, connection string) ([]config.CredentialRule, bool) {
	c, err := s.db.GetMCPConnection(ctx, connection)
	if err != nil {
		return nil, false
	}
	rules, _ := decodeMCPRules(c)
	out := make([]config.CredentialRule, 0, len(rules))
	for _, r := range rules {
		cr := mcpCredentialRule(r)
		if config.NormalizeCredentialRule(&cr) == nil {
			out = append(out, cr)
		}
	}
	return out, true
}

// fencedResource says whether an edit changed what the connection is (its
// resource, issuer, auth or client), which ends every grant for it.
func fencedResource(before, after store.MCPConnection) bool {
	return before.URL != after.URL || before.Issuer != after.Issuer || before.Auth != after.Auth ||
		before.StaticClientID != after.StaticClientID || before.StaticRedirectURI != after.StaticRedirectURI
}

func (s *MCPConnectionService) endGrants(ctx context.Context, connection string) error {
	if ender, ok := s.grants.(interface {
		EndGrants(ctx context.Context, connection string) error
	}); ok {
		return ender.EndGrants(ctx, connection)
	}
	return nil
}

// MCPGrants decides agents' use of MCP connections through the credential
// broker: its rules, approvals, grant lifetimes, limits, look-only
// refusal and audit. The caller is attributed from its execution, never
// from what it claims.
type MCPGrants struct {
	broker *CredentialBroker
	conns  *MCPConnectionService
	db     *store.Store
}

// NewMCPGrants wires the broker to the registry's rules.
func NewMCPGrants(broker *CredentialBroker, conns *MCPConnectionService, db *store.Store) *MCPGrants {
	broker.SetMCPRules(conns)
	return &MCPGrants{broker: broker, conns: conns, db: db}
}

// attribution is what Legato knows of a caller.
type mcpAttribution struct {
	executionID, taskID, repository string
}

func (g *MCPGrants) attribute(ctx context.Context, caller MCPCaller) mcpAttribution {
	a := mcpAttribution{executionID: caller.ExecutionID, taskID: caller.TaskID, repository: mcpNoRepository}
	execution, err := g.db.GetExecution(ctx, caller.ExecutionID)
	if err != nil {
		return a
	}
	if a.taskID == "" {
		// A claimed task is passed on so the broker checks it is this
		// execution's; otherwise the execution's own task.
		a.taskID = execution.TaskID
	}
	if task, err := g.db.GetTask(ctx, execution.TaskID); err == nil {
		if cc, err := store.ParseCodeContext(task.CodeContext); err == nil && cc != nil && strings.Count(cc.Repo, "/") == 1 {
			a.repository = cc.Repo
		}
	}
	return a
}

func (g *MCPGrants) operation(ctx context.Context, connection, tool string) string {
	c, err := g.db.GetMCPConnection(ctx, connection)
	if err != nil {
		return mcpWrite
	}
	_, readOnly := decodeMCPRules(c)
	for _, p := range readOnly {
		if credential.Covers(p, tool) {
			return mcpRead
		}
	}
	return mcpWrite
}

// Authorize decides one tool call, as every dispatch must: the connection's
// own state first (instance, switch, authorization), then the broker, which
// may ask the operator and wait. Arguments are never passed here, so they
// never reach the audit log.
func (g *MCPGrants) Authorize(ctx context.Context, connection string, caller MCPCaller, tool string) (UseDecision, error) {
	if !mcpToolName.MatchString(tool) {
		return UseDecision{}, ErrMCPToolName
	}
	a := g.attribute(ctx, caller)
	use := CredentialUse{TaskID: a.taskID, ExecutionID: a.executionID, Credential: MCPCredentialName(connection),
		Operation: g.operation(ctx, connection, tool), Resource: a.repository + "/" + tool, Call: "tools/call " + tool}
	if problem, err := g.connectionProblem(ctx, connection); err != nil {
		return UseDecision{}, err
	} else if problem != nil {
		g.broker.audit(ctx, use.TaskID, use.ExecutionID, store.CredentialAuditEntry{Event: "refuse", Credential: use.Credential, Operation: use.Operation, Resource: use.Resource, Detail: withCall(problem.Message, use.Call)})
		return UseDecision{Reason: problem.Message}, nil
	}
	return g.broker.Authorize(ctx, use)
}

// connectionProblem is why a connection cannot be used by anyone now.
func (g *MCPGrants) connectionProblem(ctx context.Context, connection string) (*MCPProblem, error) {
	st, err := g.conns.Get(ctx, connection)
	if errors.Is(err, store.ErrNotFound) {
		return mcpProblem("not_found"), nil
	}
	if err != nil {
		return nil, err
	}
	if st.Problem != nil && st.Problem.Code != st.LastError {
		return st.Problem, nil
	}
	return nil, nil
}

// Visible says whether a tool may be listed to a caller: a running
// execution, a rule that does not deny it, and nothing that refuses it
// outright. Listing asks no one and spends nothing; every call is still
// authorized on its own.
func (g *MCPGrants) Visible(ctx context.Context, connection string, caller MCPCaller, tool string) (bool, error) {
	if !mcpToolName.MatchString(tool) {
		return false, nil
	}
	if problem, err := g.connectionProblem(ctx, connection); err != nil || problem != nil {
		return false, err
	}
	a := g.attribute(ctx, caller)
	op := g.operation(ctx, connection, tool)
	return g.broker.permits(ctx, a.executionID, a.taskID, MCPCredentialName(connection), op, a.repository+"/"+tool), nil
}

// MCPGranted says whether any rule could let the caller use the connection,
// for MCPConnectionService.Usable.
func (g *MCPGrants) MCPGranted(ctx context.Context, c store.MCPConnection, caller MCPCaller) (bool, error) {
	a := g.attribute(ctx, caller)
	if !g.broker.executionRunning(ctx, a.executionID, a.taskID) {
		return false, nil
	}
	rules, _ := decodeMCPRules(c)
	for _, r := range rules {
		if r.Approve != "deny" && credential.Covers(r.Repository, a.repository) {
			return true, nil
		}
	}
	return false, nil
}

// EndGrants ends every grant of a connection: its resource changed or it is gone.
func (g *MCPGrants) EndGrants(ctx context.Context, connection string) error {
	return g.db.EndCredentialGrantsFor(ctx, MCPCredentialName(connection))
}
