package server

import (
	"flag"
	"os"
	"strings"
	"testing"
)

var updateAPIReference = flag.Bool("update", false, "rewrite the API reference page of the docs site")

// The manual's API page is written from the route table, so it lists the
// routes the server serves, with their classes, and nothing else.
const apiReferencePage = "../../site/src/content/docs/reference/api.md"

func TestTheAPIReferenceAnswersTheEverydayQuestions(t *testing.T) {
	page := apiReference()
	for _, want := range []string{
		"| Move a card to another column | `PATCH /api/tasks/<id>` | `{\"column\": \"Done\"}` |",
		"| Stop an agent | `POST /api/agents/kill` | `{\"task_id\": \"<id>\"}` |",
		"| Message an agent that is running | `POST /api/tasks/<id>/agent` | `{\"prompt\": \"…\"}` |",
		"| `GET` | `/api/agents` | read | List the agents |",
		"| `POST` | `/api/agents/kill` | risky | Stop an agent | `{\"task_id\": \"<id>\"}` |",
		"| `POST` | `/api/approvals/{id}/decision` | never | Decide an approval request |",
		"| Hand a task to a worker, with its review passes | `POST /api/tasks/<id>/handoff` | `{\"worker\": \"<instance_id>\"",
		"| Read what a worker's review passes can do | `GET /api/settings/workers/<instance_id>/review-passes` |",
		"\"review\": {\"enabled\": true, \"agent\": \"codex\", \"model\": \"…\", \"tier\": \"…\", \"skill\": \"…\", \"max_passes\": 3}",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the API page lacks %q", want)
		}
	}
}

// Each everyday task and each body names a route the table classes.
func TestTheAPIReferenceNamesOnlyServedRoutes(t *testing.T) {
	for _, task := range everydayCalls {
		if !hasEntry(task.Method, task.Pattern) {
			t.Errorf("%q: %s %s is not in routeClassTable", task.To, task.Method, task.Pattern)
		}
	}
	for key := range routeBodies {
		method, pattern, _ := strings.Cut(key, " ")
		if !hasEntry(method, pattern) {
			t.Errorf("a body for %s, which routeClassTable does not class", key)
		}
	}
}

func TestTheAPIReferencePageIsCurrent(t *testing.T) {
	want := apiReference()
	if *updateAPIReference {
		if err := os.WriteFile(apiReferencePage, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(apiReferencePage)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/server -run TestTheAPIReferencePageIsCurrent -update)", err)
	}
	if string(got) != want {
		t.Fatalf("%s is out of date with the route table (run go test ./internal/server -run TestTheAPIReferencePageIsCurrent -update)", apiReferencePage)
	}
}
