package handofftest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// advertSources is what a worker in these tests says it has: claude-code
// installed with two tiers, codex registered but not installed, the
// Legato tools on, one MCP connection and two skills.
func advertSources() service.HandoffAdvertSources {
	return service.HandoffAdvertSources{
		Installed: func(kind string) bool { return kind != "codex" },
		Tiers: func(kind string) []string {
			if kind == "claude-code" {
				return []string{"fast", "deep"}
			}
			return nil
		},
		MCPTools:       func() bool { return true },
		MCPConnections: func(context.Context) ([]string, error) { return []string{"burnlite"}, nil },
		Skills:         func(context.Context) ([]string, error) { return []string{"tdd", "pr"}, nil },
		Repositories:   func() []string { return []string{"acme/*", "cpave3/legato"} },
	}
}

func TestAMainKeepsWhatAWorkerAdvertises(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Wire: func(w *Worker) { w.Handoffs.SetAdvertSources(advertSources()) }})
	main := NewMain(t, MainOptions{Clock: worker.Clock})

	paired := Pair(t, main, worker)

	advert := paired.Advert
	if advert == nil {
		t.Fatal("the main kept no advert of the worker it paired with")
	}
	if paired.AdvertAt == nil || *paired.AdvertAt != "2026-11-05T09:00:00Z" {
		t.Fatalf("advert_at = %v", paired.AdvertAt)
	}
	if advert.Version == "" {
		t.Fatal("the advert names no version")
	}
	for _, f := range []string{service.HandoffFeaturePushedBranch, service.HandoffFeatureTicket, service.HandoffFeatureReviewOverride,
		service.HandoffFeatureReturn, service.HandoffFeatureDrop} {
		if !slices.Contains(advert.Features, f) {
			t.Errorf("features %v lack %s", advert.Features, f)
		}
	}
	if !advert.Sandbox {
		t.Error("the advert says the worker has no sandbox")
	}
	if advert.DefaultAgent != "claude-code" {
		t.Errorf("default agent = %q", advert.DefaultAgent)
	}
	agents := map[string]service.HandoffAdvertAgent{}
	for _, a := range advert.Agents {
		agents[a.Name] = a
	}
	if a := agents["claude-code"]; !a.Installed || !a.PromptsOff || !slices.Equal(a.Tiers, []string{"deep", "fast"}) {
		t.Errorf("claude-code = %+v", a)
	}
	if a, ok := agents["codex"]; !ok || a.Installed {
		t.Errorf("codex = %+v (listed %v): registered, not installed", a, ok)
	}
	if !advert.MCPTools || !slices.Equal(advert.MCPConnections, []string{"burnlite"}) {
		t.Errorf("mcp tools %v, connections %v", advert.MCPTools, advert.MCPConnections)
	}
	if !slices.Equal(advert.Skills, []string{"pr", "tdd"}) {
		t.Errorf("skills = %v", advert.Skills)
	}
	if !slices.Equal(advert.Repositories, []string{"acme/*", "cpave3/legato"}) {
		t.Errorf("repositories = %v", advert.Repositories)
	}

	// The main's list (Settings › Workers) carries it too.
	workers, err := main.Workers.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 1 || workers[0].Advert == nil || !slices.Equal(workers[0].Advert.Skills, []string{"pr", "tdd"}) {
		t.Fatalf("the main lists %+v", workers)
	}
}

func TestAMainRefreshesTheAdvertWhenItConnects(t *testing.T) {
	ctx := context.Background()
	skills := []string{"tdd"}
	sources := advertSources()
	sources.Skills = func(context.Context) ([]string, error) { return skills, nil }
	worker := StartWorker(t, WorkerOptions{Wire: func(w *Worker) { w.Handoffs.SetAdvertSources(sources) }})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	Pair(t, main, worker)

	skills = []string{"tdd", "review"}
	if err := main.Connections.SyncWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	workers, err := main.Workers.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 1 || workers[0].Advert == nil || !slices.Equal(workers[0].Advert.Skills, []string{"review", "tdd"}) {
		t.Fatalf("after a sync the main lists %+v", workers[0].Advert)
	}
}

func TestTheAdvertIsOnlyForAPairedMain(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Wire: func(w *Worker) { w.Handoffs.SetAdvertSources(advertSources()) }})

	// A stranger on the network reads the features and nothing more.
	resp := worker.Do(http.MethodGet, "/api/meta", "", "")
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if _, ok := meta["handoff_features"]; !ok {
		t.Errorf("/api/meta has no handoff_features: %s", raw)
	}
	for _, private := range []string{"burnlite", "acme/*", "cpave3/legato", "tdd"} {
		if strings.Contains(string(raw), private) {
			t.Errorf("/api/meta leaks %q: %s", private, raw)
		}
	}

	// The capabilities route takes a paired main's token, not none and not
	// the web token.
	if resp := worker.Do(http.MethodGet, service.HandoffCapabilitiesPath, "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("without a token: %d", resp.StatusCode)
	}
	if resp := worker.Do(http.MethodGet, service.HandoffCapabilitiesPath, worker.WebToken(), ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("with the web token: %d", resp.StatusCode)
	}
}

func TestAWorkerWithoutTheAdvertStillPairs(t *testing.T) {
	// An older worker has no capabilities route: the main pairs and keeps
	// no advert.
	worker := StartWorker(t, WorkerOptions{Handler: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == service.HandoffCapabilitiesPath {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	if paired := Pair(t, main, worker); paired.Advert != nil {
		t.Fatalf("advert %+v from a worker that has none", paired.Advert)
	}
}

func TestAWorkerThatStopsAdvertisingLosesItsAdvert(t *testing.T) {
	ctx := context.Background()
	var old atomic.Bool
	worker := StartWorker(t, WorkerOptions{Handler: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if old.Load() && r.URL.Path == service.HandoffCapabilitiesPath {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	if paired := Pair(t, main, worker); paired.Advert == nil {
		t.Fatal("no advert at pairing")
	}

	// Downgraded to a build without the route: what it said before is no
	// longer true.
	old.Store(true)
	if err := main.Connections.SyncWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	workers, err := main.Workers.List(ctx)
	if err != nil || len(workers) != 1 || workers[0].Advert != nil || workers[0].AdvertAt != nil {
		t.Fatalf("after the downgrade the main lists %+v, %v", workers, err)
	}
}
