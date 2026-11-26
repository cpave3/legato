package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type TaskWorkflowInput struct {
	Key         string
	Type        string
	Required    bool
	Description string
	Choices     []string
	Secret      bool
}

type TaskWorkflowOption struct {
	Name        string
	Title       string
	Description string
	TaskInput   string
	Inputs      []TaskWorkflowInput
	Values      map[string]string
	// Loop is the review loop that already holds a review item's pull
	// request, whatever started it: the item opens it instead of starting
	// review-loop. nil for any other item.
	Loop *FoundReviewLoop
}

type TaskWorkflowRun struct {
	ID            string
	Title         string
	RelatedTaskID string
}

type TaskWorkflowLauncher struct {
	store     *store.Store
	workflows *WorkflowService
	discover  func() workflowengine.DiscoveryResult
	// reviewEngine says whether some process runs the review engine; nil
	// counts as running.
	reviewEngine func() ReviewEngineStatus
	// authors derive a review's agent_author; nil makes every pull
	// request a person's.
	authors *ReviewAuthors
}

func NewTaskWorkflowLauncher(s *store.Store, workflows *WorkflowService, discover func() workflowengine.DiscoveryResult) *TaskWorkflowLauncher {
	return &TaskWorkflowLauncher{store: s, workflows: workflows, discover: discover}
}

// SetReviewEngine tells the launcher whether some Legato on the data folder
// runs the review engine. Without it, a card offers no review passes and
// ReviewUnavailable says why.
func (l *TaskWorkflowLauncher) SetReviewEngine(status func() ReviewEngineStatus) {
	l.reviewEngine = status
}

// SetReviewAuthors derives the agent_author a card's review starts
// review-loop with (spec LEG-559 D8).
func (l *TaskWorkflowLauncher) SetReviewAuthors(authors *ReviewAuthors) {
	l.authors = authors
}

// reviewEngineReason is why no review can start, or "" when one can.
func (l *TaskWorkflowLauncher) reviewEngineReason() string {
	if l.reviewEngine == nil {
		return ""
	}
	if status := l.reviewEngine(); !status.Running {
		return status.Reason
	}
	return ""
}

// ReviewUnavailable says why a card's menu offers no review passes when it
// would offer some with the review engine running: "" when it offers them,
// or when the card has nothing to review.
func (l *TaskWorkflowLauncher) ReviewUnavailable(ctx context.Context, taskID string) string {
	reason := l.reviewEngineReason()
	if reason == "" {
		return ""
	}
	task, err := l.store.GetTask(ctx, taskID)
	if err != nil {
		return ""
	}
	if open, _ := l.openPullRequests(ctx, task); len(open) == 0 {
		return ""
	}
	return reason
}

// Has says whether a workflow of a name is discovered.
func (l *TaskWorkflowLauncher) Has(name string) bool {
	for _, definition := range l.discover().Definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

// HasInput says whether a discovered workflow declares an input.
func (l *TaskWorkflowLauncher) HasInput(name, input string) bool {
	for _, definition := range l.discover().Definitions {
		if definition.Name == name {
			_, ok := definition.Inputs[input]
			return ok
		}
	}
	return false
}

func (l *TaskWorkflowLauncher) List(ctx context.Context, taskID string) ([]TaskWorkflowOption, error) {
	task, err := l.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load selected task: %w", err)
	}
	result := l.discover()
	options := make([]TaskWorkflowOption, 0, len(result.Definitions))
	for _, definition := range result.Definitions {
		taskInput, ok := soleTaskInput(definition)
		if !ok {
			continue
		}
		values := workflowPrefillValues(definition, task, taskInput, result.Repositories)
		inputs := make([]TaskWorkflowInput, 0, len(definition.Inputs))
		keys := make([]string, 0, len(definition.Inputs))
		for key := range definition.Inputs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			spec := definition.Inputs[key]
			choices := make([]string, len(spec.Choices))
			for i, choice := range spec.Choices {
				choices[i] = rawWorkflowValue(choice)
			}
			inputs = append(inputs, TaskWorkflowInput{Key: key, Type: spec.Type, Required: spec.Required, Description: spec.Description, Choices: choices, Secret: spec.Secret})
		}
		options = append(options, TaskWorkflowOption{Name: definition.Name, Title: definition.Title, Description: definition.Description, TaskInput: taskInput, Inputs: inputs, Values: values})
	}
	options = append(options, l.reviewPassOptions(ctx, task)...)
	if len(result.Errors) > 0 {
		messages := make([]string, len(result.Errors))
		for i, discoveryErr := range result.Errors {
			messages[i] = discoveryErr.Path + ": " + discoveryErr.Error
		}
		return options, fmt.Errorf("some workflows could not be loaded: %s", strings.Join(messages, "; "))
	}
	return options, nil
}

func (l *TaskWorkflowLauncher) Start(ctx context.Context, name string, inputs map[string]string) (*TaskWorkflowRun, error) {
	if name == workflowengine.ReviewLoopWorkflowName {
		return l.startReviewLoop(ctx, inputs)
	}
	result := l.discover()
	for _, definition := range result.Definitions {
		if definition.Name != name {
			continue
		}
		_, ok := soleTaskInput(definition)
		if !ok && !reviewLoopWorkflow(name) {
			return nil, fmt.Errorf("workflow %q does not accept exactly one task input", name)
		}
		view, err := l.workflows.Start(ctx, definition, inputs)
		if err != nil {
			return nil, err
		}
		// A review loop refused at start (one already running on the pull
		// request, say) says why, naming that loop.
		if reviewLoopWorkflow(name) && view.Run.Status == "failed" {
			return nil, failedAtStart(view)
		}
		return &TaskWorkflowRun{ID: view.Run.ID, Title: view.Run.WorkflowTitle, RelatedTaskID: view.Run.RelatedTaskID}, nil
	}
	return nil, fmt.Errorf("workflow %q not found", name)
}

// startReviewLoop starts the built-in review-loop on a pull request no loop
// holds. One that a loop holds, whatever started it, is refused before any
// run is made, naming the loop and its link (review_loop_active); a refusal
// the start itself meets says the same. The start only writes the run: the
// review engine's holder opens the loop, so it runs whether or not this
// process has the engine.
func (l *TaskWorkflowLauncher) startReviewLoop(ctx context.Context, inputs map[string]string) (*TaskWorkflowRun, error) {
	repo := strings.TrimSpace(inputs["repo"])
	if number, err := strconv.Atoi(strings.TrimSpace(inputs["pr_number"])); err == nil && CheckReviewLoopQuery(repo, number) == nil {
		holder, err := l.store.ActiveReviewLoop(ctx, repo, number)
		if err != nil {
			return nil, err
		}
		if holder != nil {
			name, link := reviewLoopName(holder)
			return nil, reviewStepError(&ReviewLoopActiveError{Repo: repo, PR: number, Name: name, Link: link, LoopID: holder.ID})
		}
	}
	view, err := l.workflows.Start(ctx, workflowengine.BuiltInReviewLoop(), inputs)
	if err != nil {
		return nil, err
	}
	if view.Run.Status == "failed" {
		return nil, failedAtStart(view)
	}
	return &TaskWorkflowRun{ID: view.Run.ID, Title: view.Run.WorkflowTitle, RelatedTaskID: view.Run.RelatedTaskID}, nil
}

// failedAtStart is why a run failed as it started: its failed step's error,
// with its code and link (the loop that holds the pull request), else the
// run's.
func failedAtStart(view *store.WorkflowRunView) error {
	for _, step := range view.Steps {
		if step.Status == "failed" && step.Error != "" {
			if step.ErrorCode == "" && step.ErrorLink == "" {
				return errors.New(step.Error)
			}
			return &WorkflowStepError{Code: step.ErrorCode, Link: step.ErrorLink, Message: step.Error}
		}
	}
	return errors.New(view.Run.Error)
}

func soleTaskInput(definition *workflowengine.Definition) (string, bool) {
	key := ""
	for candidate, input := range definition.Inputs {
		if input.Type != "task" {
			continue
		}
		if key != "" {
			return "", false
		}
		key = candidate
	}
	return key, key != ""
}

func workflowPrefillValues(definition *workflowengine.Definition, task *store.Task, taskInput string, repositories []workflowengine.RepositoryPreset) map[string]string {
	values := map[string]string{taskInput: task.ID}
	for key, input := range definition.Inputs {
		if input.Default != nil {
			values[key] = rawWorkflowValue(input.Default)
		}
	}
	metadata, _ := store.ParseTaskMetadata(task.Metadata)
	for key, input := range definition.Inputs {
		if input.Type != "repository" || values[key] != "" {
			continue
		}
		if repository := repositoryFromMetadata(metadata, repositories); repository != "" {
			values[key] = repository
		} else if len(repositories) == 1 {
			values[key] = repositories[0].Name
		}
	}
	return values
}

func repositoryFromMetadata(metadata map[string]any, presets []workflowengine.RepositoryPreset) string {
	value, ok := metadata["repository"]
	if !ok {
		return ""
	}
	if name, ok := value.(string); ok {
		for _, preset := range presets {
			if name == preset.Name {
				return name
			}
		}
		return ""
	}
	repository, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if name := fmt.Sprint(repository["name"]); name != "" {
		for _, preset := range presets {
			if name == preset.Name {
				return name
			}
		}
	}
	if fmt.Sprint(repository["repo"]) == "" || fmt.Sprint(repository["primary_dir"]) == "" {
		return ""
	}
	encoded, _ := json.Marshal(repository)
	return string(encoded)
}

func rawWorkflowValue(value any) string {
	switch value.(type) {
	case map[string]any, []any:
		encoded, _ := json.Marshal(value)
		return string(encoded)
	default:
		return fmt.Sprint(value)
	}
}

// ReviewPassesWorkflow is the example workflow of review passes before
// review-loop, which an operator's copy may still run.
const ReviewPassesWorkflow = "review-pr-passes"

// reviewLoopWorkflow says whether a workflow is one a launcher starts a
// review loop with, which takes no task input: the built-in review-loop, or
// the review-pr-passes example.
func reviewLoopWorkflow(name string) bool {
	return name == ReviewPassesWorkflow || name == workflowengine.ReviewLoopWorkflowName
}

// reviewPassOptions are a card's review items: one for each of its open
// pull requests (spec LEG-559 stories 1 to 4). A pull request a review loop
// holds, whatever started it, offers that loop (Loop); any other starts the
// built-in review-loop with agent_author derived from who opened it (D8),
// and no ready or steward. When the card has several open pull requests in
// one repository (its tracked branches are in stack order) and no loop
// holds any, the first item reviews the whole stack, started on the bottom
// one, as an agent's only when every one is, and the others each pull
// request alone. They are offered only while some process runs the review
// engine.
func (l *TaskWorkflowLauncher) reviewPassOptions(ctx context.Context, task *store.Task) []TaskWorkflowOption {
	if l.reviewEngineReason() != "" {
		return nil
	}
	return l.reviewPassItems(ctx, task)
}

// openPullRequests are a card's open pull requests, in stack order, and
// its tracked branches.
func (l *TaskWorkflowLauncher) openPullRequests(ctx context.Context, task *store.Task) ([]PRMetaView, []store.TaskBranch) {
	tracked, err := l.store.ListTrackedTaskBranches(ctx)
	if err != nil {
		return nil, nil
	}
	var open []PRMetaView
	for _, pr := range TaskPullRequests(task, tracked[task.ID]) {
		if pr.Repo == "" || strings.EqualFold(pr.State, "MERGED") || strings.EqualFold(pr.State, "CLOSED") {
			continue
		}
		open = append(open, pr)
	}
	return open, tracked[task.ID]
}

// reviewPassItems are the review items a card offers.
func (l *TaskWorkflowLauncher) reviewPassItems(ctx context.Context, task *store.Task) []TaskWorkflowOption {
	open, rows := l.openPullRequests(ctx, task)
	lookup := NewReviewLoopLookup(l.store)
	loops := make([]*FoundReviewLoop, len(open))
	agents := make([]bool, len(open))
	held, allAgents := false, true
	for i, pr := range open {
		loops[i], _ = lookup.ActiveReviewLoop(ctx, pr.Repo, pr.PRNumber)
		if loops[i] != nil {
			held = true
			continue
		}
		agents[i] = l.authors.Card(ctx, rows, []PRMetaView{pr})
		allAgents = allAgents && agents[i]
	}
	stack := len(open) > 1
	for _, pr := range open {
		stack = stack && strings.EqualFold(pr.Repo, open[0].Repo)
	}
	var options []TaskWorkflowOption
	if stack && !held {
		bottom, top := open[0], open[len(open)-1]
		options = append(options, TaskWorkflowOption{
			Name: workflowengine.ReviewLoopWorkflowName, Title: fmt.Sprintf("Review passes · stack #%d–#%d · %s", bottom.PRNumber, top.PRNumber, authorMode(allAgents)),
			Description: fmt.Sprintf("Review every open pull request of the stack of %s#%d in passes posted to GitHub; %s.", bottom.Repo, bottom.PRNumber, whoFixes(allAgents)),
			Values:      map[string]string{"repo": bottom.Repo, "pr_number": fmt.Sprint(bottom.PRNumber), "agent_author": fmt.Sprint(allAgents), "scope": ReviewScopeStack},
		})
	}
	for i, pr := range open {
		if loops[i] != nil {
			options = append(options, TaskWorkflowOption{
				Name: workflowengine.ReviewLoopWorkflowName, Title: fmt.Sprintf("Open review loop · #%d", pr.PRNumber),
				Description: fmt.Sprintf("%s#%d is in review: loop %s (%s).", pr.Repo, pr.PRNumber, loops[i].LoopID, loops[i].Link),
				Loop:        loops[i],
			})
			continue
		}
		values := map[string]string{"repo": pr.Repo, "pr_number": fmt.Sprint(pr.PRNumber), "agent_author": fmt.Sprint(agents[i])}
		if stack {
			values["scope"] = ReviewScopePR
		}
		options = append(options, TaskWorkflowOption{
			Name: workflowengine.ReviewLoopWorkflowName, Title: fmt.Sprintf("Review passes · #%d · %s", pr.PRNumber, authorMode(agents[i])),
			Description: fmt.Sprintf("Review %s#%d in passes posted to GitHub; %s.", pr.Repo, pr.PRNumber, whoFixes(agents[i])),
			Values:      values,
		})
	}
	return options
}

// authorMode is what a review item says of whose pull request it is.
func authorMode(agent bool) string {
	if agent {
		return "agent"
	}
	return "person"
}

// whoFixes says who is asked to fix a review's findings.
func whoFixes(agent bool) string {
	if agent {
		return "this card's agent fixes the findings"
	}
	return "its author answers the findings on GitHub"
}
