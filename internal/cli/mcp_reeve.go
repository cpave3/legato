package cli

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/site"
)

// MCPReeve is what the Reeve's own tools need. They are offered only to a
// session of the Reeve, and refused to every other.
type MCPReeve struct {
	// Snapshot is the instance's snapshot (instance_status).
	Snapshot func(ctx context.Context) string
	// LogPath is this instance's log file (read_logs).
	LogPath string
	// APIBase is this instance's web API, as https://127.0.0.1:3080
	// (legato_api); "" when no web server runs.
	APIBase string
	// HTTP calls the API; nil uses a client that trusts the local server's
	// own certificate.
	HTTP *http.Client
}

// reeveSession reports whether the calling session is the Reeve's.
func (m *mcpTools) reeveSession() bool {
	return m.opts.Reeve != nil && m.opts.Store != nil && service.IsReeveTask(context.Background(), m.opts.Store, m.opts.TaskID)
}

func (m *mcpTools) reeveTools() []mcpserver.Tool {
	return []mcpserver.Tool{m.reeveDocs(), m.reeveStatus(), m.reeveLogs(), m.reeveAPI()}
}

// mustBeReeve refuses a call from any session but the Reeve's: the tools
// are listed only to it, and checked again on each call.
func (m *mcpTools) mustBeReeve(ctx context.Context) error {
	if _, err := m.session(ctx); err != nil {
		return err
	}
	if !service.IsReeveTask(ctx, m.opts.Store, m.opts.TaskID) {
		return fmt.Errorf("this tool is the Reeve's, and this session is not")
	}
	return nil
}

type reeveDocsInput struct {
	Action string `json:"action" jsonschema:"list (every page), search (pages with every word of query) or read (one page by path)"`
	Query  string `json:"query,omitempty" jsonschema:"for search: the words to find"`
	Path   string `json:"path,omitempty" jsonschema:"for read: the page's path, as using/reeve"`
}

func (m *mcpTools) reeveDocs() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "docs", Title: "Read Legato's user manual",
		Description: "Lists, searches and reads Legato's user manual (the pages of its documentation site, built into this Legato). Read the page about something before you explain or change it.",
		ReadOnly:    true,
	}, func(ctx context.Context, in reeveDocsInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		switch in.Action {
		case "list", "":
			var b strings.Builder
			for _, p := range site.Pages() {
				fmt.Fprintf(&b, "- %s: %s", p.Path, p.Title)
				if p.Description != "" {
					fmt.Fprintf(&b, " — %s", p.Description)
				}
				b.WriteString("\n")
			}
			return mcpserver.Result{Text: b.String()}, nil
		case "search":
			hits := site.Search(in.Query, 10)
			if len(hits) == 0 {
				return mcpserver.Result{Text: "No page holds every word of " + in.Query + "."}, nil
			}
			var b strings.Builder
			for _, h := range hits {
				fmt.Fprintf(&b, "## %s (%s)\n", h.Title, h.Path)
				for _, line := range h.Lines {
					fmt.Fprintf(&b, "> %s\n", clipLine(line, 300))
				}
				b.WriteString("\n")
			}
			return mcpserver.Result{Text: b.String()}, nil
		case "read":
			text, ok := site.Read(in.Path)
			if !ok {
				return mcpserver.Result{}, fmt.Errorf("no page %q: list the pages first", in.Path)
			}
			return mcpserver.Result{Text: text}, nil
		}
		return mcpserver.Result{}, fmt.Errorf("action %q: use list, search or read", in.Action)
	})
}

func (m *mcpTools) reeveStatus() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "instance_status", Title: "Read this instance's status",
		Description: "Returns a fresh snapshot of this Legato instance: version and updates, how it runs, its workers and mains, open Attention items, running agents and recent errors in its log.",
		ReadOnly:    true,
	}, func(ctx context.Context, _ struct{}) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		if m.opts.Reeve.Snapshot == nil {
			return mcpserver.Result{}, fmt.Errorf("this Legato cannot take a snapshot of itself here")
		}
		return mcpserver.Result{Text: m.opts.Reeve.Snapshot(ctx)}, nil
	})
}

type reeveLogsInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"a regular expression; only lines that match it are returned"`
	Lines  int    `json:"lines,omitempty" jsonschema:"how many of the most recent lines, 100 when left out, 1000 at most"`
}

func (m *mcpTools) reeveLogs() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "read_logs", Title: "Read this instance's log",
		Description: "Returns the most recent lines of this Legato instance's log file, oldest first, filtered by a regular expression when given.",
		ReadOnly:    true,
	}, func(ctx context.Context, in reeveLogsInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		n := in.Lines
		if n <= 0 {
			n = 100
		}
		if n > 1000 {
			n = 1000
		}
		lines, err := service.ReadLogLines(m.opts.Reeve.LogPath, in.Filter, n)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if len(lines) == 0 {
			return mcpserver.Result{Text: "No lines match."}, nil
		}
		return mcpserver.Result{Text: strings.Join(lines, "\n")}, nil
	})
}

type reeveAPIInput struct {
	Method string          `json:"method" jsonschema:"GET, POST, PUT, PATCH or DELETE"`
	Path   string          `json:"path" jsonschema:"the route, starting /api/, with its query string"`
	Body   json.RawMessage `json:"body,omitempty" jsonschema:"the JSON body, for a route that takes one"`
}

func (m *mcpTools) reeveAPI() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "legato_api", Title: "Call Legato's API",
		Description: "Calls a route of this Legato's API with your own token and returns its status and body. Read routes answer at once; a change or risky route answers 403 needs_approval, and a route you may never call answers 403 never.",
	}, func(ctx context.Context, in reeveAPIInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		if m.opts.Reeve.APIBase == "" {
			return mcpserver.Result{}, fmt.Errorf("this Legato runs no web server, so its API cannot be called")
		}
		if !strings.HasPrefix(in.Path, "/api/") {
			return mcpserver.Result{}, fmt.Errorf("path %q: give a route starting /api/", in.Path)
		}
		session, err := m.opts.Store.GetReeveSession(ctx, m.opts.TaskID)
		if err != nil || session.APIToken == "" {
			return mcpserver.Result{}, fmt.Errorf("this session has no API token: it has ended")
		}
		method := strings.ToUpper(strings.TrimSpace(in.Method))
		if method == "" {
			method = http.MethodGet
		}
		var body io.Reader
		if len(in.Body) > 0 {
			body = strings.NewReader(string(in.Body))
		}
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(m.opts.Reeve.APIBase, "/")+in.Path, body)
		if err != nil {
			return mcpserver.Result{}, err
		}
		req.Header.Set("Authorization", "Bearer "+session.APIToken)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		client := m.opts.Reeve.HTTP
		if client == nil {
			client = &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
		}
		resp, err := client.Do(req)
		if err != nil {
			return mcpserver.Result{}, err
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		_ = m.opts.Store.TouchReeveSession(ctx, m.opts.TaskID)
		return mcpserver.Result{Text: fmt.Sprintf("%s %s → %d\n\n%s", method, in.Path, resp.StatusCode, out)}, nil
	})
}

func clipLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
