package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// ApprovalSourceMCPStdio is an operator's request to run a local MCP server:
// adding one, or changing what it runs. Its resolver saves the change once
// it is allowed.
const ApprovalSourceMCPStdio = "mcp_stdio"

// MCPApprovalRequired is a change that was not made: it runs a command on
// the host, so it waits for the operator's approval, ApprovalID.
type MCPApprovalRequired struct {
	ApprovalID string `json:"approval_id"`
	Signed     bool   `json:"signed"`
}

func (e *MCPApprovalRequired) Error() string {
	return "this runs a command on this computer, so it waits for the operator's approval (" + e.ApprovalID + ")"
}

var (
	mcpEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	// errMCPStdioNoApprovals is a Legato that cannot ask the operator.
	errMCPStdioNoApprovals = errors.New("local MCP servers need approvals, which this Legato process does not have")
)

const (
	mcpMaxArgs     = 64
	mcpMaxEnv      = 64
	mcpMaxArgBytes = 4096
	mcpMaxEnvValue = 8192
)

// SetApprovals lets the registry add and change local servers: each such
// change is an approval of source mcp_stdio, applied once allowed.
func (s *MCPConnectionService) SetApprovals(a *ApprovalService) {
	s.approvals = a
	a.RegisterResolver(ApprovalSourceMCPStdio, mcpStdioResolver{s})
}

// homeDir turns a leading ~ (alone or ~/...) into the home folder.
func homeDir(dir string) string {
	if dir == "" {
		return ""
	}
	if dir == "~" {
		dir = "~/"
	}
	return filepath.Clean(expandHome(dir))
}

func hasControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r < ' ' || r == 0x7f })
}

func encodeStdio(c *store.MCPConnection, args []string, env map[string]string) error {
	if args == nil {
		args = []string{}
	}
	if env == nil {
		env = map[string]string{}
	}
	a, err := json.Marshal(args)
	if err != nil {
		return err
	}
	e, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.ArgsJSON, c.EnvJSON = string(a), string(e)
	return nil
}

func decodeStdio(c store.MCPConnection) ([]string, map[string]string) {
	args := []string{}
	env := map[string]string{}
	_ = json.Unmarshal([]byte(c.ArgsJSON), &args)
	_ = json.Unmarshal([]byte(c.EnvJSON), &env)
	return args, env
}

// checkStdio checks what a local server runs.
func checkStdio(c store.MCPConnection) error {
	invalid := func(format string, a ...any) error {
		return errors.Join(ErrMCPConnectionInvalid, fmt.Errorf(format, a...))
	}
	if c.URL != "" || c.Issuer != "" || c.Networks != "" {
		return invalid("url, issuer, private_networks: a local server has none")
	}
	if c.Auth != "none" || c.StaticClientID != "" || c.StaticRedirectURI != "" || c.StaticSecretSet {
		return invalid("auth: a local server has no authorization")
	}
	cmd := c.Command
	switch {
	case strings.TrimSpace(cmd) == "" || cmd != strings.TrimSpace(cmd):
		return invalid("command: the program to run, an absolute path or a name found in PATH")
	case len(cmd) > mcpMaxArgBytes || hasControl(cmd):
		return invalid("command: at most %d characters, no control characters", mcpMaxArgBytes)
	case strings.ContainsRune(cmd, '/') && !filepath.IsAbs(cmd):
		return invalid("command: give an absolute path, or a bare name found in PATH")
	}
	args, env := decodeStdio(c)
	if len(args) > mcpMaxArgs {
		return invalid("args: at most %d", mcpMaxArgs)
	}
	for i, a := range args {
		if len(a) > mcpMaxArgBytes || strings.ContainsRune(a, 0) {
			return invalid("args[%d]: at most %d characters, no NUL", i, mcpMaxArgBytes)
		}
	}
	if !filepath.IsAbs(c.Dir) {
		return invalid("dir: the working directory, an absolute path or one under ~")
	}
	if info, err := os.Stat(c.Dir); err != nil || !info.IsDir() {
		return invalid("dir: %s is not a directory on this computer", c.Dir)
	}
	if len(env) > mcpMaxEnv {
		return invalid("env: at most %d variables", mcpMaxEnv)
	}
	for k, v := range env {
		if !mcpEnvName.MatchString(k) {
			return invalid("env: %q is not a variable name", k)
		}
		if len(v) > mcpMaxEnvValue || strings.ContainsRune(v, 0) {
			return invalid("env.%s: at most %d characters, no NUL", k, mcpMaxEnvValue)
		}
		if v == "legato://" {
			return invalid("env.%s: legato://<secret name>", k)
		}
	}
	return nil
}

// stdioDefinitionChanged is whether a change touches what a local server
// runs, which only an approval may change.
func stdioDefinitionChanged(old, now store.MCPConnection) bool {
	return old.Command != now.Command || old.ArgsJSON != now.ArgsJSON || old.Dir != now.Dir || old.EnvJSON != now.EnvJSON || old.Kind != now.Kind
}

// stdioCommandLine is what the operator is shown: the command line, then
// each variable of the environment.
func stdioCommandLine(c store.MCPConnection) string {
	args, env := decodeStdio(c)
	words := []string{shellWord(c.Command)}
	for _, a := range args {
		words = append(words, shellWord(a))
	}
	lines := []string{strings.Join(words, " ")}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, k+"="+env[k])
	}
	return strings.Join(lines, "\n")
}

// mcpStdioProposal is what an mcp_stdio approval applies.
type mcpStdioProposal struct {
	Change     string               `json:"change"` // add or edit
	Connection string               `json:"connection"`
	Command    string               `json:"command"`
	Cwd        string               `json:"cwd"`
	Input      *MCPConnectionInput  `json:"input,omitempty"`
	Edit       *MCPConnectionChange `json:"edit,omitempty"`
}

// requestStdio asks the operator to approve a local server's definition.
func (s *MCPConnectionService) requestStdio(ctx context.Context, p mcpStdioProposal, c store.MCPConnection) error {
	if s.approvals == nil {
		return errMCPStdioNoApprovals
	}
	p.Connection, p.Command, p.Cwd = c.Name, stdioCommandLine(c), c.Dir
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	title := "Run a local MCP server: " + c.Name
	if p.Change == "edit" {
		title = "Change what the local MCP server " + c.Name + " runs"
	}
	devices, err := s.db.ListApprovalDevices(ctx, store.DeviceActive)
	if err != nil {
		return err
	}
	a, _, err := s.approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceMCPStdio, SourceKey: p.Change + ":" + c.Name + ":" + hex.EncodeToString(sum[:8]),
		Title:   title,
		Summary: "Legato will run this command on this computer as you, outside any sandbox, for the agents you grant " + c.Name + " to.",
		Details: p,
		Options: []ApprovalOption{{ID: "allow", Label: "Allow", Kind: ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: ApprovalDeny}},
		Signed:  len(devices) > 0,
	})
	if err != nil {
		return err
	}
	return &MCPApprovalRequired{ApprovalID: a.ID, Signed: a.Signed}
}

type mcpStdioResolver struct{ s *MCPConnectionService }

func (r mcpStdioResolver) Resolve(ctx context.Context, a Approval, d ApprovalDecision) error {
	if d.Decision != ApprovalAllow {
		return nil
	}
	var p mcpStdioProposal
	if err := json.Unmarshal(a.Details, &p); err != nil {
		return fmt.Errorf("reading the approved change: %w", err)
	}
	switch {
	case p.Change == "add" && p.Input != nil:
		_, err := r.s.add(ctx, *p.Input, true)
		return err
	case p.Change == "edit" && p.Edit != nil:
		_, err := r.s.update(ctx, p.Connection, *p.Edit, true)
		return err
	}
	return errors.New("the approved change is not one Legato knows")
}
