package handofftest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// withoutFeatures stands in for a worker built before some handoff
// features: its /api/meta lists the rest, or none at all when every
// feature is dropped.
func withoutFeatures(drop ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/meta" {
				next.ServeHTTP(w, r)
				return
			}
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)
			var meta map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &meta)
			var kept []any
			if features, ok := meta["handoff_features"].([]any); ok {
				for _, f := range features {
					if !slices.Contains(drop, f.(string)) {
						kept = append(kept, f)
					}
				}
			}
			if len(kept) == 0 {
				delete(meta, "handoff_features")
			} else {
				meta["handoff_features"] = kept
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(meta)
		})
	}
}

func TestAWorkerWithoutTheTicketFeatureIsNotSentATicket(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "legato-1", Handler: withoutFeatures(service.HandoffFeatureTicket)})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	jira := newFakeJira(checkoutTicket)
	jiraTask(t, main, jira, "Start with the cart serializer.", "")

	_, err := handOff(t, main, paired.InstanceID, checkoutTicket.ID)

	if !errors.Is(err, service.ErrWorkerTooOld) || !strings.Contains(err.Error(), "legato-1 is too old for ticket") {
		t.Fatalf("handing a Jira task to a worker without tickets: %v", err)
	}
	if cards := workerCards(t, worker); len(cards) != 0 {
		t.Fatalf("the worker made %+v", cards)
	}
}

func TestAWorkerWithoutThePushedBranchFeatureIsNotSentOne(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Handler: withoutFeatures(service.HandoffFeaturePushedBranch)})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Finish the cart", "", "")

	_, err := handOff(t, main, paired.InstanceID, taskID, func(r *service.TaskHandoffRequest) {
		r.Kind, r.Branch = service.HandoffPushedBranch, "feature/cart"
	})

	if !errors.Is(err, service.ErrWorkerTooOld) || !strings.Contains(err.Error(), "pushed_branch") {
		t.Fatalf("handing off from a pushed branch: %v", err)
	}
	stillHome(t, main, worker, taskID)
}

func TestAWorkerThatListsNoFeaturesStillTakesAPlainHandoff(t *testing.T) {
	// A worker built before features were advertised takes what protocol 2
	// always carried: a local task on a new branch.
	worker := StartWorker(t, WorkerOptions{Handler: withoutFeatures(service.HandoffFeatures()...)})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")

	if _, err := handOff(t, main, paired.InstanceID, taskID); err != nil {
		t.Fatal(err)
	}
}

func TestTheWorkerRefusesAFieldItDoesNotKnow(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	token := mainToken(t, main, paired.InstanceID)

	body, _ := json.Marshal(service.HandoffCall{HandoffID: "ho-1", MainTaskID: "T-1", Title: "Fix it", Notes: []service.HandoffCallNote{},
		Repo: "acme/widget", Purpose: service.HandoffMakeChange, Kind: service.HandoffNewBranch, Branch: "legato/t-1", AgentKind: "claude-code"})
	body = bytes.Replace(body, []byte(`{`), []byte(`{"base_branch":"develop",`), 1)
	resp := worker.Do(http.MethodPost, service.HandoffCallPath, token, string(body))
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var refused struct{ Error, Message string }
	_ = json.Unmarshal(raw, &refused)
	if resp.StatusCode != http.StatusBadRequest || refused.Error != "unknown_field" || !strings.Contains(refused.Message, "base_branch") {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
	if cards := workerCards(t, worker); len(cards) != 0 {
		t.Fatalf("the worker made %+v", cards)
	}
}
