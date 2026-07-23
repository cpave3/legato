package service

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
)

type artifactPlanReader interface {
	Plan(context.Context, string) (*PlanView, error)
}

type artifactReviewReader interface {
	Tour(context.Context, string) (*ReviewTourView, error)
}

// ArtifactRef identifies one node in a task's artifact lineage graph.
type ArtifactRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// ArtifactLineageEdge describes a typed relationship between task artifacts.
type ArtifactLineageEdge struct {
	Type string      `json:"type"`
	From ArtifactRef `json:"from"`
	To   ArtifactRef `json:"to"`
}

// TaskArtifacts is the complete plan and review history for one task.
type TaskArtifacts struct {
	TaskID      string                `json:"task_id"`
	Plans       []PlanView            `json:"plans"`
	ReviewTours []ReviewTourView      `json:"review_tours"`
	Edges       []ArtifactLineageEdge `json:"edges"`
}

type ArtifactService struct {
	store   *store.Store
	plans   artifactPlanReader
	reviews artifactReviewReader
}

func NewArtifactService(s *store.Store, plans artifactPlanReader, reviews artifactReviewReader) *ArtifactService {
	return &ArtifactService{store: s, plans: plans, reviews: reviews}
}

func (a *ArtifactService) TaskArtifacts(ctx context.Context, taskID string) (*TaskArtifacts, error) {
	if _, err := a.store.GetTask(ctx, taskID); err != nil {
		return nil, err
	}

	storedPlans, err := a.store.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	plans := make([]PlanView, 0)
	edges := make([]ArtifactLineageEdge, 0)
	for _, storedPlan := range storedPlans {
		if storedPlan.TaskID != taskID {
			continue
		}
		plan, err := a.plans.Plan(ctx, storedPlan.ID)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *plan)
		seenPass := make(map[string]bool)
		for _, origin := range plan.Origins {
			edges = append(edges, ArtifactLineageEdge{
				Type: "finding_originated_plan",
				From: ArtifactRef{Type: "review_finding", ID: origin.Finding.ID},
				To:   ArtifactRef{Type: "plan", ID: plan.Plan.ID},
			})
			if !seenPass[origin.ReviewPassID] {
				edges = append(edges, ArtifactLineageEdge{
					Type: "review_pass_originated_plan",
					From: ArtifactRef{Type: "review_pass", ID: origin.ReviewPassID},
					To:   ArtifactRef{Type: "plan", ID: plan.Plan.ID},
				})
				seenPass[origin.ReviewPassID] = true
			}
		}
	}

	storedTours, err := a.store.ListReviewToursByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	tours := make([]ReviewTourView, 0, len(storedTours))
	for _, storedTour := range storedTours {
		tour, err := a.reviews.Tour(ctx, storedTour.ID)
		if err != nil {
			return nil, err
		}
		tours = append(tours, *tour)
		for _, pass := range tour.Passes {
			if pass.CapturedPlan != nil {
				edges = append(edges, ArtifactLineageEdge{
					Type: "plan_captured_by_review_pass",
					From: ArtifactRef{Type: "plan", ID: pass.CapturedPlan.PlanID},
					To:   ArtifactRef{Type: "review_pass", ID: pass.Pass.ID},
				})
			}
			for _, request := range pass.PlanRequests {
				for _, findingID := range request.FindingIDs {
					edges = append(edges, ArtifactLineageEdge{
						Type: "finding_selected_for_plan_request",
						From: ArtifactRef{Type: "review_finding", ID: findingID},
						To:   ArtifactRef{Type: "review_plan_request", ID: request.ID},
					})
				}
			}
		}
	}

	return &TaskArtifacts{TaskID: taskID, Plans: plans, ReviewTours: tours, Edges: edges}, nil
}
