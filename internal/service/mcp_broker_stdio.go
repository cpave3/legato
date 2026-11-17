package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/mcpstdio"
	"github.com/cpave3/legato/internal/engine/mcpupstream"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpStdioLimits bound a local server: its answers may carry pictures, and
// its first call may wait while it starts what it needs (a browser). Its
// clients also take draft-07 schemas (AcceptDraft07), which servers built on
// the TypeScript SDK declare; remote connections keep to 2020-12.
var mcpStdioLimits = mcpupstream.Limits{MaxResultBytes: 8 << 20, CallTimeout: 2 * time.Minute}

// mcpStdioBaseEnv is what a local server gets of Legato's own environment,
// before the connection's: where home and the user's folders are, the
// locale and the time zone. Not Legato's tokens or the rest.
var mcpStdioBaseEnv = []string{"HOME", "USER", "LOGNAME", "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "TZ", "TMPDIR",
	"XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "PATH"}

// errMCPStdioNotFound and errMCPStdioFailed are what an agent is told: the
// operator's Test shows the rest.
var (
	errMCPStdioNotFound = errors.New("the local MCP server could not be started: its command was not found. The operator can see why under Settings › MCP › Test")
	errMCPStdioFailed   = errors.New("the local MCP server could not be started, or stopped. The operator can see why under Settings › MCP › Test")
)

// stdioSpec is how to start a local server: its command and arguments, in
// its working directory, with the base environment then its own (a value
// legato://name read from the secret store).
func (s *MCPConnectionService) stdioSpec(ctx context.Context, c store.MCPConnection) (mcpstdio.Spec, error) {
	args, own := decodeStdio(c)
	env := map[string]string{}
	for _, k := range mcpStdioBaseEnv {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	for k, v := range own {
		if name, ok := strings.CutPrefix(v, "legato://"); ok {
			if s.opts.Secrets == nil {
				return mcpstdio.Spec{}, fmt.Errorf("env.%s names a secret, and this Legato cannot read secrets", k)
			}
			secret, err := s.opts.Secrets.Read(ctx, name)
			if err != nil {
				return mcpstdio.Spec{}, fmt.Errorf("env.%s: reading legato://%s: %w", k, name, err)
			}
			v = secret
		}
		env[k] = v
	}
	list := make([]string, 0, len(env))
	for k, v := range env {
		list = append(list, k+"="+v)
	}
	sort.Strings(list)
	return mcpstdio.Spec{Command: c.Command, Args: args, Dir: c.Dir, Env: list}, nil
}

// stdioConnect starts a new process of the connection for each session of
// a client, its stderr logged under label (an execution, or "list").
func (b *MCPBroker) stdioConnect(c store.MCPConnection, label string) func(context.Context) (mcp.Transport, error) {
	return func(ctx context.Context) (mcp.Transport, error) {
		p, err := b.startStdio(ctx, c, label)
		if err != nil {
			return nil, err
		}
		return p.Transport(), nil
	}
}

func (b *MCPBroker) startStdio(ctx context.Context, c store.MCPConnection, label string) (*mcpstdio.Process, error) {
	spec, err := b.conns.stdioSpec(ctx, c)
	if err != nil {
		return nil, err
	}
	spec.Grace = b.stdioGrace
	spec.Stderr = func(line string) { log.Printf("mcp stdio %s %s: %s", c.Name, label, line) }
	p, err := mcpstdio.Start(spec)
	if err != nil {
		log.Printf("mcp stdio %s %s: %v", c.Name, label, err)
		return nil, err
	}
	return p, nil
}

// session is the execution's own client of a local server, made on its
// first call; its process starts with its first session.
func (b *MCPBroker) session(conn *mcpBrokerConn, executionID string) *mcpupstream.Client {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if s, ok := conn.sessions[executionID]; ok {
		return s
	}
	s := mcpupstream.New(mcpupstream.Options{Connect: b.stdioConnect(conn.row, executionID), Limits: mcpStdioLimits, AcceptDraft07: true})
	conn.sessions[executionID] = s
	return s
}

// closeConn ends a connection's clients and every session's server.
func (c *mcpBrokerConn) close() {
	c.client.Close()
	c.mu.Lock()
	sessions := c.sessions
	c.sessions = map[string]*mcpupstream.Client{}
	c.mu.Unlock()
	for _, s := range sessions {
		s.Close()
	}
}

// MCPExecutions finds an execution, to tell whether it has ended.
type MCPExecutions interface {
	GetExecution(ctx context.Context, id string) (*store.Execution, error)
}

// Sweep stops the local servers of sessions that ended, and of connections
// removed, turned off or changed. The process that serves the broker runs
// it every few seconds.
func (b *MCPBroker) Sweep(ctx context.Context, executions MCPExecutions) {
	b.mu.Lock()
	conns := make(map[string]*mcpBrokerConn, len(b.conn))
	for name, c := range b.conn {
		conns[name] = c
	}
	b.mu.Unlock()
	for name, c := range conns {
		st, err := b.conns.Get(ctx, name)
		if (err != nil && errors.Is(err, store.ErrNotFound)) || (err == nil && (!st.Enabled || st.Revision != c.revision)) {
			b.mu.Lock()
			if b.conn[name] == c {
				delete(b.conn, name)
			}
			b.mu.Unlock()
			c.close()
			continue
		}
		c.mu.Lock()
		ids := make([]string, 0, len(c.sessions))
		for id := range c.sessions {
			ids = append(ids, id)
		}
		c.mu.Unlock()
		for _, id := range ids {
			ex, err := executions.GetExecution(ctx, id)
			if err == nil && ex.EndedAt == nil && ex.Status == store.ExecutionRunning {
				continue
			}
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				continue
			}
			b.EndExecution(id)
		}
	}
}

// EndExecution stops every local server of an execution.
func (b *MCPBroker) EndExecution(executionID string) {
	b.mu.Lock()
	conns := make([]*mcpBrokerConn, 0, len(b.conn))
	for _, c := range b.conn {
		conns = append(conns, c)
	}
	b.mu.Unlock()
	for _, c := range conns {
		c.mu.Lock()
		s, ok := c.sessions[executionID]
		delete(c.sessions, executionID)
		c.mu.Unlock()
		if ok {
			s.Close()
		}
	}
}

// MCPTestTool is a tool a Test found.
type MCPTestTool struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// MCPTestResult is what testing a connection found: its tools, the ones
// Legato will not offer, or why it failed, with a local server's last
// error output.
type MCPTestResult struct {
	Tools    []MCPTestTool          `json:"tools"`
	Rejected []mcpupstream.Rejected `json:"rejected,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Stderr   string                 `json:"stderr,omitempty"`
}

// Test reaches a saved connection now and lists its tools. A local server
// is started for the test and stopped after it; its error output is part
// of the result. Only a saved, so approved, definition is ever run.
func (b *MCPBroker) Test(ctx context.Context, name string) (MCPTestResult, error) {
	st, err := b.conns.Get(ctx, name)
	if err != nil {
		return MCPTestResult{}, err
	}
	res := MCPTestResult{Tools: []MCPTestTool{}}
	var cat *mcpupstream.Catalog
	if st.Kind == "stdio" {
		var p *mcpstdio.Process
		client := mcpupstream.New(mcpupstream.Options{Limits: mcpStdioLimits, AcceptDraft07: true, Connect: func(ctx context.Context) (mcp.Transport, error) {
			var err error
			if p, err = b.startStdio(ctx, st.MCPConnection, "test"); err != nil {
				return nil, err
			}
			return p.Transport(), nil
		}})
		cat, err = client.Tools(ctx)
		client.Close()
		if p != nil {
			<-p.Done()
			res.Stderr = p.StderrTail()
		}
		b.recordStdioContact(ctx, name, st.Revision, err)
		if err == nil {
			err = b.recordTools(ctx, name, cat)
		}
	} else {
		c, cerr := b.client(ctx, name)
		if cerr != nil {
			res.Error = safeMCPError(cerr)
			return res, nil
		}
		cat, err = b.catalog(ctx, name, c, true)
	}
	if err != nil {
		res.Error = err.Error()
		if st.Kind != "stdio" {
			res.Error = safeMCPError(err)
		}
		return res, nil
	}
	for _, t := range cat.Tools {
		res.Tools = append(res.Tools, MCPTestTool{Name: t.Name, Title: t.Title, Description: t.Description})
	}
	res.Rejected = cat.Rejected
	return res, nil
}

func (b *MCPBroker) recordStdioContact(ctx context.Context, name string, revision int64, err error) {
	code := ""
	switch {
	case err == nil:
	case errors.Is(err, mcpupstream.ErrInvalidArguments), errors.Is(err, context.Canceled):
		return
	case errors.Is(err, mcpstdio.ErrNotFound):
		code = "command_not_found"
	case errors.Is(err, mcpupstream.ErrTooLarge):
		code = "too_large"
	case errors.Is(err, mcpupstream.ErrInvalidResult):
		code = "protocol"
	default:
		code = "process_failed"
	}
	_ = b.conns.RecordContact(context.WithoutCancel(ctx), name, revision, code)
}

// stdioAgentError is what an agent is told of a local server's failure:
// never a host path or its error output.
func stdioAgentError(err error) error {
	switch {
	case errors.Is(err, mcpupstream.ErrInvalidArguments), errors.Is(err, mcpupstream.ErrInvalidResult), errors.Is(err, mcpupstream.ErrTooLarge):
		return err
	case errors.Is(err, mcpupstream.ErrSessionEnded):
		return errors.New("the local MCP server stopped before it answered; the call may not have run. Calling again starts it afresh")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("the local MCP server did not answer in time")
	case errors.Is(err, mcpstdio.ErrNotFound):
		return errMCPStdioNotFound
	}
	return errMCPStdioFailed
}
