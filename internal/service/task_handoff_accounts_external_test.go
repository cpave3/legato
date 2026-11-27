package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/handofftest"
	"github.com/cpave3/legato/internal/service"
)

func TestMainHandoffSendsOnlyLogicalAccountSelection(t *testing.T) {
	var sent map[string]any
	worker := handofftest.StartWorker(t, handofftest.WorkerOptions{Handler: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == service.HandoffCallPath {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if err := json.Unmarshal(body, &sent); err != nil {
					t.Error(err)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
			next.ServeHTTP(w, r)
		})
	}})
	main := handofftest.NewMain(t, handofftest.MainOptions{Clock: worker.Clock})
	paired := handofftest.Pair(t, main, worker)
	ctx := context.Background()
	card, err := main.Board.CreateTask(ctx, "Account handoff", "Work", "Ready", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: card.ID, Worker: paired.InstanceID, Purpose: service.HandoffMakeChange, Repo: "acme/widget", AgentKind: "claude-code", Account: "default"}); err != nil {
		t.Fatal(err)
	}
	if sent["account"] != "default" {
		t.Fatalf("wire account = %v", sent["account"])
	}
	for _, forbidden := range []string{"account_identity", "config_dir", "resolved_dir"} {
		if _, found := sent[forbidden]; found {
			t.Fatalf("wire includes %s", forbidden)
		}
	}
}
