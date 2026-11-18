package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
}

func NewTaskWorkflowLauncher(s *store.Store, workflows *WorkflowService, discover func() workflowengine.DiscoveryResult) *TaskWorkflowLauncher {
	return &TaskWorkflowLauncher{store: s, workflows: workflows, discover: discover}
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
	options = append(options, l.reviewPassOptions(ctx, task, result)...)
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
	result := l.discover()
	for _, definition := range result.Definitions {
		if definition.Name != name {
			continue
		}
		_, ok := soleTaskInput(definition)
		if !ok && name != ReviewPassesWorkflow {
			return nil, fmt.Errorf("workflow %q does not accept exactly one task input", name)
		}
		view, err := l.workflows.Start(ctx, definition, inputs)
		if err != nil {
			return nil, err
		}
		// A review loop refused at start (one already running on the pull
		// request, say) says why, naming that loop.
		if name == ReviewPassesWorkflow && view.Run.Status == "failed" {
			for _, step := range view.Steps {
				if step.Status == "failed" && step.Error != "" {
					return nil, errors.New(step.Error)
				}
			}
			return nil, errors.New(view.Run.Error)
		}
		return &TaskWorkflowRun{ID: view.Run.ID, Title: view.Run.WorkflowTitle, RelatedTaskID: view.Run.RelatedTaskID}, nil
	}
	return nil, fmt.Errorf("workflow %q not found", name)
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

// ReviewPassesWorkflow is the workflow a card's Review passes action starts.
const ReviewPassesWorkflow = "review-pr-passes"

// reviewPassOptions are a card's Review passes actions: one for each of its
// open pull requests, when the review-pr-passes workflow is there. Each
// starts a review loop with the card's own agent as the author.
func (l *TaskWorkflowLauncher) reviewPassOptions(ctx context.Context, task *store.Task, result workflowengine.DiscoveryResult) []TaskWorkflowOption {
	found := false
	for _, definition := range result.Definitions {
		found = found || definition.Name == ReviewPassesWorkflow
	}
	if !found {
		return nil
	}
	tracked, err := l.store.ListTrackedTaskBranches(ctx)
	if err != nil {
		return nil
	}
	var options []TaskWorkflowOption
	for _, pr := range TaskPullRequests(task, tracked[task.ID]) {
		if pr.Repo == "" || strings.EqualFold(pr.State, "MERGED") || strings.EqualFold(pr.State, "CLOSED") {
			continue
		}
		options = append(options, TaskWorkflowOption{
			Name: ReviewPassesWorkflow, Title: fmt.Sprintf("Review passes · #%d", pr.PRNumber),
			Description: fmt.Sprintf("Review %s#%d in passes posted to GitHub; this card's agent fixes the findings.", pr.Repo, pr.PRNumber),
			Values:      map[string]string{"repo": pr.Repo, "pr_number": fmt.Sprint(pr.PRNumber), "agent_author": "true"},
		})
	}
	return options
}
