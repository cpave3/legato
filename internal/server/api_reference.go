package server

import (
	"fmt"
	"strings"
)

// The manual's API page (site/src/content/docs/reference/api.md) is written
// from routeClassTable by apiReference: every route with its method, its
// class and what it does, and the body of the routes most called. Run
// go test ./internal/server -run TestTheAPIReferencePageIsCurrent -update
// after changing a route.

// everydayCall is a common job and the call that does it, at the top of the
// API page in everyday words, so the Reeve's docs search finds it.
type everydayCall struct {
	To      string
	Method  string
	Pattern string
	Query   string
	Body    string
}

var everydayCalls = []everydayCall{
	{"Read the board: its columns and cards", "GET", "/api/board", "", ""},
	{"Read a task in brief: its column, pull requests and latest note", "GET", "/api/tasks/{id}", "?view=summary", ""},
	{"Read a task in full, with its description and notes", "GET", "/api/tasks/{id}", "", ""},
	{"Create a task (a card)", "POST", "/api/tasks", "", `{"title": "…", "column": "Backlog"}`},
	{"Move a card to another column", "PATCH", "/api/tasks/{id}", "", `{"column": "Done"}`},
	{"Rename a task or change its description", "PATCH", "/api/tasks/{id}", "", `{"title": "…", "description": "…"}`},
	{"Archive a done task", "POST", "/api/tasks/{id}/archive", "", ""},
	{"Link a pull request to a task", "POST", "/api/tasks/{id}/link-pr", "", `{"owner": "…", "repo": "…", "pr_number": 12}`},
	{"List the agents", "GET", "/api/agents", "", ""},
	{"Check on some agents in brief", "GET", "/api/agents", "?view=compact&status=running&task=<id>,<id>&workspace=<name>", ""},
	{"Start an agent on a task", "POST", "/api/tasks/{id}/agent", "", `{"agent_kind": "claude-code", "prompt": "…"}`},
	{"Message an agent that is running", "POST", "/api/tasks/{id}/agent", "", `{"prompt": "…"}`},
	{"Read what an agent said and did last", "GET", "/api/agents/{task_id}/conversation", "", ""},
	{"Stop an agent", "POST", "/api/agents/kill", "", `{"task_id": "<id>"}`},
	{"Restart an agent, keeping its conversation", "POST", "/api/agents/{task_id}/restart", "", ""},
	{"List what needs attention", "GET", "/api/attention", "", ""},
	{"Change a setting", "PUT", "/api/config/settings/{key}", "", `{"value": …}`},
}

// routeBodies is the request body of the routes most called, by
// "METHOD pattern"; … stands for a value of your own.
var routeBodies = map[string]string{
	"POST /api/tasks":                         `{"title": "…", "description": "…", "column": "Backlog", "priority": "", "workspace_id": 1}`,
	"PATCH /api/tasks/{id}":                   `{"title": "…", "description": "…", "column": "Done", "workspace_id": 1}`,
	"POST /api/tasks/{id}/agent":              `{"agent_kind": "claude-code", "prompt": "…", "working_dir": "…", "repo": "owner/name", "resources": ["…"]}`,
	"POST /api/tasks/{id}/link-pr":            `{"owner": "…", "repo": "…", "pr_number": 12}`,
	"POST /api/agents/spawn":                  `{"title": "…", "agent_kind": "claude-code", "working_dir": "…", "prompt": "…"}`,
	"POST /api/agents/kill":                   `{"task_id": "<id>"}`,
	"POST /api/agents/{task_id}/restart":      `{"switch_sandbox": false}`,
	"PATCH /api/agents/{task_id}/notify":      `{"enabled": true}`,
	"PUT /api/agents/{task_id}/group":         `{"group": "…"}`,
	"PUT /api/agent-groups/{name}":            `{"name": "…"}`,
	"POST /api/agents/{task_id}/chimera/send": `{"content": "…"}`,
	"PUT /api/config/settings/{key}":          `{"value": …}`,
	"PUT /api/config/lists/{list}/{name}":     `{"value": {…}}`,
	"POST /api/reeve/message":                 `{"text": "…"}`,
}

// apiGroups are the page's sections, each the routes whose pattern starts
// with one of its prefixes; the first group that matches takes a route.
var apiGroups = []struct {
	Title    string
	Prefixes []string
}{
	{"Tasks and cards", []string{"/api/tasks"}},
	{"Agents", []string{"/api/agents", "/api/preparing-agents", "/api/ui-items", "/agent-ui-"}},
	{"Board", []string{"/api/board", "/api/workspaces", "/api/remote", "/api/repo/", "/api/ticket-providers"}},
	{"Attention and reports", []string{"/api/attention", "/api/report", "/api/navigation-summary"}},
	{"The Reeve", []string{"/api/reeve", "/api/integrations/reeve"}},
	{"Approvals and devices", []string{"/api/approvals", "/api/devices"}},
	{"Credentials", []string{"/api/credentials"}},
	{"Plans, reviews and learnings", []string{"/api/plans", "/api/review", "/api/learning", "/plan-images", "/review-images"}},
	{"Workflows and swarms", []string{"/api/workflow", "/api/swarm"}},
	{"Settings, updates and the machine", []string{"/api/settings", "/api/config", "/api/updates", "/api/restart", "/api/hot-reload", "/api/engram", "/api/adapters", "/api/macros", "/api/directories", "/api/repositories", "/api/resources", "/api/job-clones", "/api/voice"}},
	{"Instances and integrations", []string{"/api/integrations", "/api/workers", "/api/pair", "/oauth"}},
	{"Other", []string{""}},
}

func apiGroupOf(pattern string) int {
	for i, g := range apiGroups {
		for _, p := range g.Prefixes {
			if strings.HasPrefix(pattern, p) {
				return i
			}
		}
	}
	return len(apiGroups) - 1
}

// apiReference is the API page.
func apiReference() string {
	var b strings.Builder
	b.WriteString(`---
title: API
description: Every route of Legato's web API, with its method, its body and what the Reeve needs to call it. How to move a card, stop or kill an agent, or message an agent.
sidebar:
  order: 25
---

<!-- Written by go test ./internal/server -run TestTheAPIReferencePageIsCurrent -update from the route table. Do not edit. -->

The web UI, the phone and the Reeve all use this API. Each call carries the web token as ` + "`Authorization: Bearer <token>`" + ` (see [Web authentication](/legato/security/web-auth/)). Bodies are JSON.

The **class** says what a call needs when the Reeve makes it with its ` + "`legato_api`" + ` tool:

- **read** runs at once. Every ` + "`GET`" + ` is a read, except the few marked otherwise below. A route Legato does not serve answers 404.
- **change** and **risky** become a change set for you to approve. A risky one can need a signed approval. See [The Reeve](/legato/using/reeve/).
- **never** is refused, whatever you approve.

A card on the board is a task: its ID is the task's. An agent is addressed by its task's ID.

## Everyday calls

| To | Call | Body |
| --- | --- | --- |
`)
	for _, c := range everydayCalls {
		fmt.Fprintf(&b, "| %s | `%s %s%s` | %s |\n", c.To, c.Method, displayPath(c.Pattern), c.Query, code(c.Body))
	}
	groups := make([][]routeClassEntry, len(apiGroups))
	for _, e := range routeClassTable {
		if e.Pattern == notServedPattern {
			continue
		}
		i := apiGroupOf(e.Pattern)
		groups[i] = append(groups[i], e)
	}
	for i, g := range apiGroups {
		if len(groups[i]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n| Method | Route | Class | What it does | Body |\n| --- | --- | --- | --- | --- |\n", g.Title)
		for _, e := range groups[i] {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s | %s |\n", e.Method, e.Pattern, e.Class, cell(e.Description), code(routeBodies[e.Method+" "+e.Pattern]))
		}
	}
	b.WriteString("\nThe settings routes (`/api/config/settings/{key}`, `/api/config/lists/{list}/{name}`) take each setting's class from the [configuration reference](/legato/reference/configuration/): the class above is the least a change needs.\n")
	return b.String()
}

// displayPath writes a pattern's wildcards as <id>.
func displayPath(pattern string) string {
	r := strings.NewReplacer("{task_id}", "<id>", "{id}", "<id>", "{", "<", "}", ">")
	return r.Replace(pattern)
}

func code(s string) string {
	if s == "" {
		return ""
	}
	return "`" + cell(s) + "`"
}

func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
