package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func newCredentialsServer(t *testing.T) (*Server, *service.CredentialBroker) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "credentials-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Narrate", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-1", ExecutionTaskID: "task-1", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	broker, err := service.NewCredentialBroker(db, approvals, service.CredentialBrokerOptions{Credentials: []config.CredentialConfig{
		{Name: "eleven", Secret: "env://SERVER_TEST_KEY", Profile: "secret", Policy: []config.CredentialRule{{Operation: config.StringList{"read"}, Approve: "auto"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(nil, nil, nil, "")
	srv.SetCredentialBroker(broker)
	return srv, broker
}

func TestCredentialsAPIListsCredentialsGrantsAndTheAudit(t *testing.T) {
	t.Setenv("SERVER_TEST_KEY", "sk-server")
	srv, broker := newCredentialsServer(t)
	if result, _ := broker.Request(context.Background(), service.AccessRequest{TaskID: "task-1", ExecutionID: "ex-1", Credential: "eleven", Operations: []string{"read"}}); result.Status != service.AccessGranted {
		t.Fatal("not granted")
	}

	rec := serve(srv, http.MethodGet, "/api/credentials", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"eleven"`) || strings.Contains(rec.Body.String(), "sk-server") {
		t.Fatalf("credentials = %d %s", rec.Code, rec.Body)
	}
	var grants struct {
		Grants []service.GrantInfo `json:"grants"`
	}
	rec = serve(srv, http.MethodGet, "/api/credentials/grants", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &grants)
	if rec.Code != http.StatusOK || len(grants.Grants) != 1 {
		t.Fatalf("grants = %d %s", rec.Code, rec.Body)
	}
	if rec := serve(srv, http.MethodDelete, "/api/credentials/grants/"+grants.Grants[0].ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("end grant = %d %s", rec.Code, rec.Body)
	}
	if rec := serve(srv, http.MethodDelete, "/api/credentials/grants/cg-missing", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("end missing grant = %d", rec.Code)
	}
	rec = serve(srv, http.MethodGet, "/api/credentials/audit?credential=eleven&limit=10", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"event":"revoke"`) {
		t.Fatalf("audit = %d %s", rec.Code, rec.Body)
	}
}

func TestCredentialsAPIWithoutABroker(t *testing.T) {
	srv := New(nil, nil, nil, "")
	rec := serve(srv, http.MethodGet, "/api/credentials", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"credentials":[]`) {
		t.Fatalf("credentials = %d %s", rec.Code, rec.Body)
	}
}
