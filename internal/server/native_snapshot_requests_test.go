package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// A panel that opens, or re-reads its snapshot, while the agent is waiting on
// the person has only the snapshot to draw the request from.
func TestNativeSnapshotKeepsWhatAPendingRequestOffers(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/TASK-42":
			fmt.Fprint(w, `{
				"id":"TASK-42","name":"TASK-42","cwd":"/work/repo","status":"waiting_for_permission",
				"messages":[],"toolCalls":[],
				"pendingPermissions":[{
					"requestId":"request-7","tool":"bash","target":"host","command":"npm run build","cwd":"/work/repo",
					"plan":"# The plan\n\n- One step",
					"options":[{"id":"accept","label":"Allow once"},{"id":"acceptForSession","label":"Allow for this session"}]
				}],
				"pendingQuestions":[{"requestId":"request-9","questions":[
					{"id":"color","header":"Color","question":"Which color?","options":[{"label":"Red"},{"label":"Blue"}],"multiSelect":false,"allowOther":true}
				]}]
			}`)
		case "/v1/sessions/TASK-42/context":
			fmt.Fprint(w, `{"contextWindow":200000,"lastPromptTokens":1200}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(endpoint.Close)
	sessionID, url := "TASK-42", endpoint.URL
	agents := &chimeraSnapshotAgentFake{
		session: &store.AgentSession{ID: 17, TaskID: "TASK-42", AgentKind: "codex", ChimeraSessionID: &sessionID, ChimeraEndpoint: &url},
	}
	server := New(nil, agents, nil, ":0")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var snapshot struct {
		PendingPermissions []struct {
			RequestID string `json:"requestId"`
			Command   string `json:"command"`
			Plan      string `json:"plan"`
			Options   []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"options"`
		} `json:"pendingPermissions"`
		PendingQuestions []struct {
			RequestID string `json:"requestId"`
			Questions []struct {
				ID       string `json:"id"`
				Question string `json:"question"`
				Options  []struct {
					Label string `json:"label"`
				} `json:"options"`
				AllowOther bool `json:"allowOther"`
			} `json:"questions"`
		} `json:"pendingQuestions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.PendingPermissions) != 1 {
		t.Fatalf("pending permissions = %+v", snapshot.PendingPermissions)
	}
	permission := snapshot.PendingPermissions[0]
	if permission.Command != "npm run build" || permission.Plan != "# The plan\n\n- One step" {
		t.Fatalf("permission = %+v", permission)
	}
	if len(permission.Options) != 2 || permission.Options[1].ID != "acceptForSession" || permission.Options[1].Label != "Allow for this session" {
		t.Fatalf("options = %+v", permission.Options)
	}
	if len(snapshot.PendingQuestions) != 1 || snapshot.PendingQuestions[0].RequestID != "request-9" {
		t.Fatalf("pending questions = %+v", snapshot.PendingQuestions)
	}
	if asked := snapshot.PendingQuestions[0].Questions; len(asked) != 1 || asked[0].Question != "Which color?" || len(asked[0].Options) != 2 || !asked[0].AllowOther {
		t.Fatalf("questions = %+v", asked)
	}
}

// An endpoint that says nothing of questions (an older Chimera) must not be
// read as saying there are none: the panel keeps a question it knows of.
func TestNativeSnapshotLeavesOutQuestionsTheEndpointDoesNotReport(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/TASK-42":
			fmt.Fprint(w, `{"id":"TASK-42","name":"TASK-42","cwd":"/work/repo","status":"idle","messages":[],"toolCalls":[]}`)
		case "/v1/sessions/TASK-42/context":
			fmt.Fprint(w, `{"contextWindow":200000,"lastPromptTokens":null}`)
		}
	}))
	t.Cleanup(endpoint.Close)
	sessionID, url := "TASK-42", endpoint.URL
	agents := &chimeraSnapshotAgentFake{
		session: &store.AgentSession{ID: 17, TaskID: "TASK-42", AgentKind: "chimera", ChimeraSessionID: &sessionID, ChimeraEndpoint: &url},
	}
	server := New(nil, agents, nil, ":0")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/TASK-42/chimera/snapshot", nil))

	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"pendingQuestions", "pendingPermissions"} {
		if _, present := snapshot[field]; present {
			t.Fatalf("snapshot reports %s although the endpoint did not: %s", field, snapshot[field])
		}
	}
}
