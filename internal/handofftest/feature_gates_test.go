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
	return releaseWithout("", drop...)
}

// releaseWithout is withoutFeatures for a worker that also says it runs
// release version ("" keeps the version it says).
func releaseWithout(version string, drop ...string) func(http.Handler) http.Handler {
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
			if version != "" {
				meta["version"] = version
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

func TestAWorkerThatListsNoFeaturesTakesWhatItsReleaseTook(t *testing.T) {
	// v0.21.0 was built before features were advertised, and took tickets,
	// pushed branches and review overrides on protocol 2.
	worker := StartWorker(t, WorkerOptions{Name: "legato-1", Handler: releaseWithout("v0.21.0", service.HandoffFeatures()...)})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	jira := newFakeJira(checkoutTicket)
	jiraTask(t, main, jira, "", "")
	if _, err := handOff(t, main, paired.InstanceID, checkoutTicket.ID); err != nil {
		t.Fatalf("a Jira task to a v0.21.0 worker: %v", err)
	}
}

func TestAWorkerFromBeforeTicketsIsNotSentOne(t *testing.T) {
	// v0.7.1 dropped a ticket and answered 201 (the LEG-224 audit).
	worker := StartWorker(t, WorkerOptions{Name: "legato-1", Handler: releaseWithout("v0.7.1", service.HandoffFeatures()...)})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	jira := newFakeJira(checkoutTicket)
	jiraTask(t, main, jira, "", "")
	if _, err := handOff(t, main, paired.InstanceID, checkoutTicket.ID); !errors.Is(err, service.ErrWorkerTooOld) || !strings.Contains(err.Error(), "too old for ticket") {
		t.Fatalf("a Jira task to a v0.7.1 worker: %v", err)
	}
	if cards := workerCards(t, worker); len(cards) != 0 {
		t.Fatalf("the worker made %+v", cards)
	}
	// A local task on a new branch is what every protocol-2 worker takes.
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")
	if _, err := handOff(t, main, paired.InstanceID, taskID); err != nil {
		t.Fatal(err)
	}
}

func TestAWorkerWithoutTheReviewOverrideFeatureIsSentNoOverride(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "legato-1", GitHubRepositories: []string{"acme/*"}, ReviewPasses: &ReviewPassOptions{},
		Handler: withoutFeatures(service.HandoffFeatureReviewOverride)})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)

	// A change to the passes it would drop is refused.
	taskID, _ := localTask(t, main, "Fix it", "", "")
	_, err := handOff(t, main, paired.InstanceID, taskID, func(r *service.TaskHandoffRequest) {
		r.Review = &service.ReviewPassOverride{MaxPasses: 2}
	})
	if !errors.Is(err, service.ErrWorkerTooOld) || !strings.Contains(err.Error(), "too old for review_override") {
		t.Fatalf("an override to a worker without the feature: %v", err)
	}
	stillHome(t, main, worker, taskID)

	// Passes only turned off is left out, harmlessly: the handoff goes.
	off := false
	if _, err := handOff(t, main, paired.InstanceID, taskID, func(r *service.TaskHandoffRequest) {
		r.Review = &service.ReviewPassOverride{Enabled: &off}
	}); err != nil {
		t.Fatalf("an override that only turns passes off: %v", err)
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
