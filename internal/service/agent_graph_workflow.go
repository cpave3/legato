package service

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// workflowPlaces puts each running agent in the active workflow runs it
// works for, read from the runs' records: the agent an agent.run step
// started, the agent an agent.interview step borrows, a review loop's
// reviewer and author, and the task's agent a run waits on to end. Only
// the runs some agent works for go in graph.Runs.
func (s *AgentGraphService) workflowPlaces(ctx context.Context, agents []AgentSession, graph *AgentGraph) error {
	runs := map[string]store.WorkflowRun{}
	for _, status := range []string{"running", "waiting"} {
		listed, err := s.opts.Runs.ListRuns(ctx, store.WorkflowRunFilter{Status: status, Limit: math.MaxInt, WithSteps: true})
		if err != nil {
			return err
		}
		for _, run := range listed {
			runs[run.ID] = run
		}
	}
	if len(runs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(runs))
	for id := range runs {
		ids = append(ids, id)
	}
	attempts, err := s.opts.Store.ListWorkflowAttemptsForRuns(ctx, ids)
	if err != nil {
		return err
	}

	running := map[string]bool{}
	for _, agent := range agents {
		running[agent.TaskID] = true
	}
	type found struct {
		agent string
		place AgentWorkflowPlace
	}
	var all []found
	add := func(agent, runID, path, role string) {
		if _, ok := runs[runID]; !ok || !running[agent] {
			return
		}
		all = append(all, found{agent, AgentWorkflowPlace{RunID: runID, StepID: topStep(path), Role: role}})
	}

	byID := map[string]store.WorkflowStepAttempt{}
	bySession := map[int][]store.WorkflowStepAttempt{}
	passes := map[string]int{}
	for _, attempt := range attempts {
		byID[attempt.ID] = attempt
		if attempt.Status == "waiting_agent" && attempt.AgentSessionID != nil {
			bySession[*attempt.AgentSessionID] = append(bySession[*attempt.AgentSessionID], attempt)
		}
	}
	for _, agent := range agents {
		if run, ok := runs[agent.WorkflowRunID]; ok {
			path := run.CurrentStepID
			if attempt, ok := byID[agent.WorkflowAttemptID]; ok {
				path = attempt.Path
			}
			add(agent.TaskID, agent.WorkflowRunID, path, AgentRoleAgent)
		}
		for _, attempt := range bySession[agent.ID] {
			if attempt.ExecutorType == "agent.interview" {
				add(agent.TaskID, attempt.RunID, attempt.Path, AgentRoleInterview)
			}
		}
	}
	for _, attempt := range attempts {
		if attempt.ExecutorType != ReviewPassesStepType || attempt.Status != "waiting" || attempt.TaskID == "" {
			continue
		}
		loop, err := readReviewRun(ctx, s.opts.Store, attempt.TaskID)
		if err != nil {
			continue
		}
		review, err := s.opts.Store.GetHandoffReview(ctx, attempt.TaskID)
		if err != nil {
			return err
		}
		if review == nil || review.Phase == ReviewPhaseEnded {
			continue
		}
		passes[attempt.RunID] = review.Pass
		add(AgentSlotKey(attempt.TaskID, SlotReviewer), attempt.RunID, attempt.Path, AgentRoleReviewer)
		author := loop.AuthorTask
		if author == "" {
			author = loop.LinkedTask
		}
		if author != "" {
			add(author, attempt.RunID, attempt.Path, AgentRoleAuthor)
		}
	}
	for _, run := range runs {
		if run.Status == "waiting" && run.TaskID != "" && waitsForAgentToEnd(run) {
			add(run.TaskID, run.ID, run.CurrentStepID, AgentRoleAwaited)
		}
	}

	// One place for each agent in each run: the one on the step the run is
	// at, else the first found.
	chosen := map[[2]string]int{}
	var places []found
	for _, f := range all {
		key := [2]string{f.agent, f.place.RunID}
		i, seen := chosen[key]
		if !seen {
			chosen[key] = len(places)
			places = append(places, f)
			continue
		}
		current := topStep(runs[f.place.RunID].CurrentStepID)
		if places[i].place.StepID != current && f.place.StepID == current {
			places[i] = f
		}
	}
	for _, f := range places {
		place := graph.Agents[f.agent]
		place.Workflows = append(place.Workflows, f.place)
		graph.Agents[f.agent] = place
		if _, ok := graph.Runs[f.place.RunID]; !ok {
			graph.Runs[f.place.RunID] = graphRun(runs[f.place.RunID], passes[f.place.RunID])
		}
	}
	for agent, place := range graph.Agents {
		if len(place.Workflows) < 2 {
			continue
		}
		sort.SliceStable(place.Workflows, func(i, j int) bool {
			a, b := graph.Runs[place.Workflows[i].RunID], graph.Runs[place.Workflows[j].RunID]
			if a.NeedsAttention != b.NeedsAttention {
				return a.NeedsAttention
			}
			if a.CreatedAt != b.CreatedAt {
				return a.CreatedAt > b.CreatedAt
			}
			return a.ID > b.ID
		})
		graph.Agents[agent] = place
	}
	return nil
}

// graphRun is a run as the graph shows it.
func graphRun(run store.WorkflowRun, pass int) AgentGraphRun {
	steps := run.Steps
	if steps == nil {
		steps = []store.WorkflowStepSummary{}
	}
	out := AgentGraphRun{ID: run.ID, WorkflowName: run.WorkflowName, WorkflowTitle: run.WorkflowTitle, Status: run.Status,
		CurrentStepID: topStep(run.CurrentStepID), NeedsAttention: run.NeedsAttention, AttentionReason: run.AttentionReason,
		Steps: steps, CreatedAt: run.CreatedAt}
	if pass > 0 {
		out.Pass = pass
	}
	return out
}

// topStep is the top-level step of a step path.
func topStep(path string) string {
	top, _, _ := strings.Cut(path, "/")
	return top
}
