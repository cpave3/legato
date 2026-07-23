package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestTaskArtifactsReturnsCompleteTypedLineage(t *testing.T) {
	f := newReviewServerFixture(t)
	ctx := context.Background()

	finding, err := f.svc.CreateFinding(ctx, f.tourID, service.ReviewFindingInput{Body: "Plan this fix"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	passID := initial.Passes[0].Pass.ID

	bundle := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundle, "plan.md"), []byte("# Follow-up\n\nFix the finding.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "plan.json"), []byte(`{"schema_version":1,"title":"Follow-up"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plans := service.NewPlanService(f.store, nil, nil)
	plan, err := plans.SubmitWithOrigin(ctx, "task-1", "follow-up", bundle, service.PlanOriginInput{
		ReviewPassID: passID,
		FindingIDs:   []string{finding.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := plans.Approve(ctx, plan.Plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AdvancePass(ctx, f.tourID, "verify the follow-up"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.EnsureReviewTour(ctx, "task-1", "security"); err != nil {
		t.Fatal(err)
	}

	artifacts := service.NewArtifactService(f.store, plans, f.svc)
	f.server.SetArtifactService(artifacts)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-1/artifacts", nil)
	res := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var got service.TaskArtifacts
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "task-1" || len(got.Plans) != 1 || len(got.ReviewTours) != 2 {
		t.Fatalf("incomplete artifacts: %+v", got)
	}
	if len(got.Plans[0].Origins) != 1 || got.Plans[0].Origins[0].Finding.ID != finding.ID {
		t.Fatalf("plan origin missing: %+v", got.Plans[0].Origins)
	}
	var foundHistory, foundCaptured bool
	for _, tour := range got.ReviewTours {
		if tour.Tour.ID == f.tourID && len(tour.Passes) == 2 {
			foundHistory = true
		}
		for _, pass := range tour.Passes {
			if pass.CapturedPlan != nil && pass.CapturedPlan.PlanID == plan.Plan.ID {
				foundCaptured = true
			}
		}
	}
	if !foundHistory || !foundCaptured {
		t.Fatalf("review history or reciprocal plan link missing: %+v", got.ReviewTours)
	}
	assertLineageEdge(t, got.Edges, "finding_originated_plan", "review_finding", finding.ID, "plan", plan.Plan.ID)
	assertLineageEdge(t, got.Edges, "plan_captured_by_review_pass", "plan", plan.Plan.ID, "review_pass", "")
}

func assertLineageEdge(t *testing.T, edges []service.ArtifactLineageEdge, edgeType, fromType, fromID, toType, toID string) {
	t.Helper()
	for _, edge := range edges {
		if edge.Type == edgeType && edge.From.Type == fromType && edge.From.ID == fromID && edge.To.Type == toType && (toID == "" || edge.To.ID == toID) {
			return
		}
	}
	t.Fatalf("lineage edge %s %s:%s -> %s:%s missing from %+v", edgeType, fromType, fromID, toType, toID, edges)
}
