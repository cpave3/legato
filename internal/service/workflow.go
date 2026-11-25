package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type ResolvedWorkflowStep struct {
	RunID         string
	AttemptID     string
	TaskID        string
	RelatedTaskID string
	// StepID is the step instance's path: the step's ID at the top of the
	// run, its scope's path and its ID within a loop pass. It is how an
	// agent names the step it completes.
	StepID  string
	Title   string
	Type    string
	With    map[string]any
	Outputs map[string]workflowengine.Output
	// Agent is the agent an agent.message or agent.stop step's agent:
	// {from: <step>} names, which the service finds before the step starts.
	Agent *WorkflowStepAgent
	// SchemaVersion is the run's definition's: clones are explicit and
	// branches granted from version 2 (LEG-485).
	SchemaVersion int
	// WaitFor are the signals that end the step (EffectiveWaitFor), which
	// its contract names.
	WaitFor []string
	// Clone is the job clone an agent.run step's with.clone names, which the
	// service finds before the step starts; nil for a fresh clone.
	Clone *store.JobClone
}

type WorkflowExecutionResult struct {
	Status       string
	Outputs      map[string]any
	AgentSession *store.AgentSession
	WorkingDir   string
	Error        string
	// TaskID is the task whose agent the step waits on, when it is not the
	// run's own: an agent.run step with task (LEG-540).
	TaskID string
}

type WorkflowExecutor interface {
	Type() string
	Start(context.Context, ResolvedWorkflowStep) (WorkflowExecutionResult, error)
	Cancel(context.Context, store.WorkflowStepAttempt) error
}

type WorkflowService struct {
	store        *store.Store
	executors    map[string]WorkflowExecutor
	collector    *WorkflowOutputCollector
	repositories []workflowengine.RepositoryPreset
	// repositorySource, when set, is read for every run in place of
	// repositories: repositories can be added while Legato runs.
	repositorySource func(context.Context) ([]workflowengine.RepositoryPreset, error)
	agentKinds       []string
	// recordOnly is set in processes that are not a host (the CLI an agent
	// runs in its pane): a completion records its step, and a host's
	// advance loop moves the run on (spec LEG-482 §5.4).
	recordOnly bool
	// resumer starts a run's card agent with a macro (Resume agent), and
	// resumedAgents stops the one it started (LEG-614).
	resumer       AgentResumer
	resumedAgents AgentKiller
	// interrupted resumes and dismisses the interrupted agents of
	// suspended steps (workflow_suspend.go).
	interrupted InterruptedAgents
	// bus and changeListener are told of every change to a run's state
	// (workflow_events.go).
	bus            *events.Bus
	changeListener func(events.WorkflowChangedPayload)
	// endedListeners are told of every run's end (OnRunEnded,
	// workflow_run_ended.go).
	endedMu        sync.Mutex
	endedListeners []WorkflowRunEndedListener
	// agentOutputSources fill agent steps' outputs at their signal
	// (workflow_agent_wait.go).
	agentOutputSources AgentOutputSources
	// discover finds the workflows workflow.run steps call by name
	// (SetDefinitions, workflow_call.go).
	discover func() workflowengine.DiscoveryResult
}

func NewWorkflowService(s *store.Store, executors []WorkflowExecutor) *WorkflowService {
	return NewWorkflowServiceWithOutputs(s, executors, "")
}

func NewWorkflowServiceWithOutputs(s *store.Store, executors []WorkflowExecutor, dataDir string) *WorkflowService {
	registry := make(map[string]WorkflowExecutor, len(executors))
	for _, executor := range executors {
		registry[executor.Type()] = executor
	}
	service := &WorkflowService{store: s, executors: registry, collector: NewWorkflowOutputCollector(s, dataDir)}
	// The gate needs nothing but the run: every workflow service has it.
	if _, given := registry[workflowengine.GateStepType]; !given {
		registry[workflowengine.GateStepType] = &gateExecutor{workflows: service}
	}
	// So does a call: its child is a run of this service, and the
	// run-ended hook settles the step that waits on it.
	if _, given := registry[workflowengine.WorkflowRunStepType]; !given {
		registry[workflowengine.WorkflowRunStepType] = &workflowRunExecutor{workflows: service}
	}
	service.OnRunEnded(childRuns{workflows: service})
	return service
}

func (s *WorkflowService) SetRepositoryPresets(presets []workflowengine.RepositoryPreset) {
	s.repositories = append([]workflowengine.RepositoryPreset(nil), presets...)
}

// SetRepositorySource makes runs read the repository presets each time
// (RepositoryPresetService.WorkflowPresets).
func (s *WorkflowService) SetRepositorySource(source func(context.Context) ([]workflowengine.RepositoryPreset, error)) {
	s.repositorySource = source
}

// repositoryPresets are the presets now.
func (s *WorkflowService) repositoryPresets(ctx context.Context) []workflowengine.RepositoryPreset {
	if s.repositorySource == nil {
		return s.repositories
	}
	presets, err := s.repositorySource(ctx)
	if err != nil {
		log.Printf("workflows: repository presets: %v", err)
	}
	return presets
}

// SetAgentKinds names the agent tools agent.run steps may ask for. A run
// whose agent step names another one is refused when it starts, instead of
// running on the default agent.
func (s *WorkflowService) SetAgentKinds(kinds []string) {
	s.agentKinds = append([]string(nil), kinds...)
}

// cardKey is the key a person knows a task by: its ticket's (LEG-603),
// else its own ID.
func cardKey(task *store.Task) string {
	if task.RemoteID != nil && strings.TrimSpace(*task.RemoteID) != "" {
		return strings.TrimSpace(*task.RemoteID)
	}
	return task.ID
}

func (s *WorkflowService) Start(ctx context.Context, definition *workflowengine.Definition, rawInputs map[string]string) (*store.WorkflowRunView, error) {
	return s.start(ctx, definition, rawInputs, "")
}

func (s *WorkflowService) StartTriggered(ctx context.Context, definition *workflowengine.Definition, rawInputs map[string]string, triggerRequestID string) (*store.WorkflowRunView, error) {
	return s.start(ctx, definition, rawInputs, triggerRequestID)
}

func (s *WorkflowService) start(ctx context.Context, definition *workflowengine.Definition, rawInputs map[string]string, triggerRequestID string) (*store.WorkflowRunView, error) {
	inputs, err := definition.NormalizeInputs(rawInputs)
	if err != nil {
		return nil, err
	}
	created, err := s.createRun(ctx, definition, inputs, store.WorkflowRun{TriggerRequestID: triggerRequestID})
	if err != nil {
		return nil, err
	}
	if err := s.advance(ctx, created.RunID, "", false); err != nil {
		// The run is already durable and the executor records its failure.
		// Return that visible run instead of encouraging an accidental duplicate launch.
		view, loadErr := s.store.GetWorkflowRun(ctx, created.RunID)
		if loadErr != nil {
			return nil, err
		}
		return view, nil
	}
	return s.store.GetWorkflowRun(ctx, created.RunID)
}

// createdRun is a run createRun recorded: its ID, its task and its
// resolved inputs. Created is false for a child run its attempt had
// already (CreateChildWorkflowRun).
type createdRun struct {
	RunID, TaskID string
	Inputs        map[string]any
	Created       bool
}

// createRun records a run of definition with its inputs normalized (a
// task input resolved to its task, a repository to the preset or the
// object), and its task, without starting any step. link carries what the
// run is linked to: its trigger request, or for a child run its parent's
// step and attempt.
func (s *WorkflowService) createRun(ctx context.Context, definition *workflowengine.Definition, inputs map[string]any, link store.WorkflowRun) (createdRun, error) {
	relatedTaskID, relatedKey := "", ""
	for key, spec := range definition.Inputs {
		switch spec.Type {
		case "task":
			id, _ := inputs[key].(string)
			task, taskErr := s.store.GetTask(ctx, id)
			if taskErr != nil {
				return createdRun{}, fmt.Errorf("workflow input %q: task %q not found: %w", key, id, taskErr)
			}
			inputs[key] = workflowTaskContext(task)
			if relatedTaskID == "" {
				relatedTaskID, relatedKey = task.ID, cardKey(task)
			}
		case "repository":
			if resolved, ok := inputs[key].(map[string]any); ok {
				// A caller's repository, as its run resolved it (LEG-489).
				inputs[key] = resolved
				continue
			}
			value, _ := inputs[key].(string)
			if strings.HasPrefix(strings.TrimSpace(value), "{") {
				var custom map[string]any
				if err := json.Unmarshal([]byte(value), &custom); err != nil || fmt.Sprint(custom["repo"]) == "" || fmt.Sprint(custom["primary_dir"]) == "" {
					return createdRun{}, fmt.Errorf("workflow input %q: custom repository requires repo and primary_dir", key)
				}
				custom["name"] = ""
				inputs[key] = custom
				continue
			}
			preset, ok := findRepositoryPreset(s.repositoryPresets(ctx), value)
			if !ok {
				return createdRun{}, fmt.Errorf("workflow input %q: repository preset %q not found", key, value)
			}
			inputs[key] = map[string]any{"name": preset.Name, "repo": preset.Repo, "primary_dir": preset.PrimaryDir, "base_branch": preset.BaseBranch}
		}
	}
	runTitle, err := definition.ResolveRunTitle(inputs)
	if err != nil {
		return createdRun{}, err
	}
	if definition.RunTitle == "" && relatedKey != "" {
		// A run for a card is named after it: several runs of one
		// workflow tell apart on the board and the run page.
		runTitle += " · " + relatedKey
	}
	if err := s.checkExecutors(definition.Steps, inputs); err != nil {
		return createdRun{}, err
	}
	color := "#6366f1"
	id, workspaceErr := s.store.EnsureWorkspace(ctx, store.Workspace{Name: store.WorkflowWorkspaceName, Color: &color, SortOrder: 1000})
	if workspaceErr != nil {
		return createdRun{}, fmt.Errorf("ensure workflow workspace: %w", workspaceErr)
	}
	workspaceID := &id
	runID := workflowID("wr-")
	taskID := store.GenerateTaskID()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.store.CreateTask(ctx, store.Task{ID: taskID, Title: runTitle, Description: "Workflow run " + runID, DescriptionMD: "Workflow run " + runID, Status: "Backlog", WorkspaceID: workspaceID, CreatedAt: now, UpdatedAt: now}); err != nil {
		return createdRun{}, err
	}
	// A Learn run's own task never starts another: the learn template's
	// trigger skips tasks with this mark.
	if definition.UsesLearning() {
		if err := s.store.SetTaskMetadata(ctx, taskID, []string{"legato", "learning_workflow"}, true); err != nil {
			return createdRun{}, fmt.Errorf("mark learning workflow task: %w", err)
		}
	}
	definitionJSON, _ := json.Marshal(definition)
	inputsJSON, _ := json.Marshal(inputs)
	run := store.WorkflowRun{ID: runID, WorkflowName: definition.Name, WorkflowTitle: runTitle, DefinitionSource: definition.Source, DefinitionDigest: digestBytes(definitionJSON), DefinitionJSON: string(definitionJSON), InputsJSON: string(inputsJSON), Status: "running", CurrentStepID: definition.Steps[0].ID, TaskID: taskID, RelatedTaskID: relatedTaskID,
		TriggerRequestID: link.TriggerRequestID, ParentRunID: link.ParentRunID, ParentStepPath: link.ParentStepPath, ParentAttempt: link.ParentAttempt, SchemaVersion: definition.SchemaVersion}
	// The run reaches its own steps when it starts; a nested scope's steps
	// are created when the run reaches them.
	steps := stepInstances(runID, "", nil, definition.Steps)
	if link.ParentRunID == "" {
		if err := s.store.CreateWorkflowRun(ctx, run, steps); err != nil {
			return createdRun{}, err
		}
	} else {
		existing, created, err := s.store.CreateChildWorkflowRun(ctx, run, steps)
		if err != nil {
			return createdRun{}, err
		}
		if !created {
			// The attempt's child exists: the task made for this one is
			// not needed.
			if err := s.store.DeleteTask(ctx, taskID); err != nil {
				log.Printf("workflows: removing the task of a child run not made: %v", err)
			}
			return createdRun{RunID: existing}, nil
		}
	}
	s.changed(ctx, runID, "", WorkflowChangeRunStarted)
	return createdRun{RunID: runID, TaskID: taskID, Inputs: inputs, Created: true}, nil
}

// checkExecutors refuses a run whose steps (a loop's body and next:
// query included) name an executor not registered, or an agent tool not
// configured.
func (s *WorkflowService) checkExecutors(steps []workflowengine.Step, inputs map[string]any) error {
	for _, step := range steps {
		if step.Loop != nil {
			if step.Loop.Next != nil {
				if err := s.checkExecutors([]workflowengine.Step{{ID: workflowengine.NextStepID, Type: step.Loop.Next.Type}}, inputs); err != nil {
					return err
				}
			}
			if err := s.checkExecutors(step.Steps, inputs); err != nil {
				return err
			}
			continue
		}
		if _, ok := s.executors[step.Type]; !ok {
			return fmt.Errorf("workflow executor %q is not registered", step.Type)
		}
		if err := s.checkAgentKind(step, inputs); err != nil {
			return err
		}
	}
	return nil
}

// stepInstances are the pending step instances of one scope's steps: the
// run's own (parent "") or a loop pass's.
func stepInstances(runID, parent string, pass *int, steps []workflowengine.Step) []store.WorkflowStep {
	instances := make([]store.WorkflowStep, len(steps))
	for i, step := range steps {
		encoded, _ := json.Marshal(step)
		instances[i] = store.WorkflowStep{RunID: runID, Path: workflowengine.StepPath(parent, step.ID), ParentPath: parent, StepID: step.ID, Pass: pass, StepOrder: i, Title: step.Title, ExecutorType: step.Type, DefinitionJSON: string(encoded), Status: "pending"}
	}
	return instances
}

func findRepositoryPreset(presets []workflowengine.RepositoryPreset, name string) (workflowengine.RepositoryPreset, bool) {
	for _, preset := range presets {
		if preset.Name == name {
			return preset, true
		}
	}
	return workflowengine.RepositoryPreset{}, false
}

func workflowTaskContext(task *store.Task) map[string]any {
	provider, remoteID := "", ""
	if task.Provider != nil {
		provider = *task.Provider
	}
	if task.RemoteID != nil {
		remoteID = *task.RemoteID
	}
	description := taskDescriptionMD(task.Provider, task.RemoteDescriptionMD, task.DescriptionMD)
	metadata, _ := store.ParseTaskMetadata(task.Metadata)
	workingDir := ""
	if codeContext, err := store.ParseCodeContext(task.CodeContext); err == nil && codeContext != nil {
		workingDir = codeContext.WorkingDir
	}
	if workingDir == "" && task.WorktreePath != nil {
		workingDir = *task.WorktreePath
	}
	return map[string]any{"id": task.ID, "title": task.Title, "description": description, "status": task.Status, "priority": task.Priority, "provider": provider, "remote_id": remoteID, "working_dir": workingDir, "metadata": metadata}
}

func (s *WorkflowService) Artifact(ctx context.Context, artifactID string) (*store.WorkflowArtifact, error) {
	return s.store.GetWorkflowArtifact(ctx, artifactID)
}

func (s *WorkflowService) Show(ctx context.Context, runID string) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	s.enrichWorkflowAttention(ctx, view)
	s.addDetails(ctx, view)
	addOutputWarnings(view)
	s.addRunOutputs(ctx, view)
	s.addAttemptAgents(ctx, view)
	s.addResumable(ctx, view)
	if view.Run.TriggerRequestID != "" {
		if request, err := s.store.GetWorkflowTriggerRequest(ctx, view.Run.TriggerRequestID); err == nil {
			view.TriggerKind = request.TriggerKind
		}
	}
	return view, nil
}

// addAttemptAgents names the agent each attempt's session is.
func (s *WorkflowService) addAttemptAgents(ctx context.Context, view *store.WorkflowRunView) {
	for i := range view.Attempts {
		attempt := &view.Attempts[i]
		if attempt.AgentSessionID == nil {
			continue
		}
		if session, err := s.store.GetAgentSessionByID(ctx, *attempt.AgentSessionID); err == nil {
			attempt.AgentID = AgentSlotKey(session.TaskID, session.Slot)
		}
	}
}

func (s *WorkflowService) enrichWorkflowAttention(ctx context.Context, view *store.WorkflowRunView) {
	s.waitingAttention(ctx, view)
	s.addMergeWaits(ctx, view)
	for _, step := range view.Steps {
		if step.Status == WorkflowStepPaused {
			view.Run.NeedsAttention = true
			view.Run.AttentionReason = pausedReason(step)
			break
		}
		if step.Status == "waiting_manual" && waitsForAgentToEnd(view.Run) && s.taskHasRunningAgent(ctx, view.Run.TaskID) {
			break
		}
		if step.Status == "waiting_manual" {
			view.Run.NeedsAttention = true
			view.Run.AttentionReason = "manual action required: " + step.Title
			break
		}
	}
	endsOnEnd := map[string]bool{}
	for _, step := range view.Steps {
		// A step that waits only for its agent to end does not need the
		// operator while the agent sits idle: that is the agent's turn.
		endsOnEnd[step.Path] = slices.Equal(waitSignals(step), []string{workflowengine.SignalEnded})
	}
	for i := range view.Attempts {
		attempt := &view.Attempts[i]
		if attempt.ExecutorType != "agent.run" || attempt.Status != "waiting_agent" || endsOnEnd[attempt.Path] {
			continue
		}
		session, err := attemptAgentSession(ctx, s.store, *attempt)
		if err != nil {
			attempt.NeedsAttention = true
			attempt.AttentionReason = "agent session ended before step completion"
			continue
		}
		attempt.AgentActivity = session.Activity
		if session.Activity != "working" {
			attempt.NeedsAttention = true
			switch session.Activity {
			case store.ActivityWaiting:
				attempt.AttentionReason = "agent waiting for input"
			case store.ActivityError:
				attempt.AttentionReason = "agent error before step completion"
			default:
				attempt.AttentionReason = "agent idle before step completion"
			}
		}
	}
}
func (s *WorkflowService) ActiveAgentAttemptsByTask(ctx context.Context, taskID string) ([]store.WorkflowStepAttempt, error) {
	return s.store.ListActiveWorkflowAgentAttemptsByTask(ctx, taskID)
}

func (s *WorkflowService) AttentionCount(ctx context.Context) (int, error) {
	runs, err := s.ListRuns(ctx, store.WorkflowRunFilter{Limit: math.MaxInt})
	if err != nil {
		return 0, err
	}
	count := 0
	for _, run := range runs {
		if run.NeedsAttention {
			count++
		}
	}
	return count, nil
}

func (s *WorkflowService) ListRuns(ctx context.Context, filter store.WorkflowRunFilter) ([]store.WorkflowRun, error) {
	runs, err := s.store.ListWorkflowRuns(ctx, filter)
	if err != nil {
		return nil, err
	}
	s.addRunOutputWarnings(ctx, runs)
	for i := range runs {
		if runs[i].RelatedTaskID != "" {
			if task, err := s.store.GetTask(ctx, runs[i].RelatedTaskID); err == nil {
				runs[i].RelatedTaskKey = cardKey(task)
			}
		}
	}
	for i := range runs {
		if runs[i].Status != "running" && runs[i].Status != "waiting" {
			continue
		}
		view, viewErr := s.store.GetWorkflowRun(ctx, runs[i].ID)
		if viewErr != nil {
			continue
		}
		s.enrichWorkflowAttention(ctx, view)
		if view.Run.NeedsAttention {
			runs[i].NeedsAttention = true
			runs[i].AttentionReason = view.Run.AttentionReason
			continue
		}
		for _, attempt := range view.Attempts {
			if attempt.NeedsAttention {
				runs[i].NeedsAttention = true
				runs[i].AttentionReason = attempt.AttentionReason
				break
			}
		}
	}
	return runs, nil
}

func (s *WorkflowService) TriggerStep(ctx context.Context, runID, stepID string, force bool) (*store.WorkflowRunView, error) {
	at, err := s.loadExecution(ctx, runID, stepID)
	if err != nil {
		return nil, err
	}
	if at.view.Run.Status != "waiting" || at.view.Run.CurrentStepID != stepID || !at.step.WaitsForPerson() || at.row.Status != "waiting_manual" {
		return nil, store.ErrWorkflowStateConflict
	}
	if force && at.step.Type != "worktree.remove" {
		return nil, fmt.Errorf("force is only supported for worktree.remove steps")
	}
	if err := s.store.TriggerWorkflowStep(ctx, runID, stepID); err != nil {
		return nil, err
	}
	s.changed(ctx, runID, stepID, WorkflowChangeStepTriggered)
	if err := s.advance(ctx, runID, stepID, force); err != nil {
		updated, loadErr := s.store.GetWorkflowRun(ctx, runID)
		if loadErr != nil {
			return nil, err
		}
		return updated, nil
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func (s *WorkflowService) PromptStep(ctx context.Context, runID, stepID string) (*store.WorkflowRunView, error) {
	view, err := s.Show(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, attempt := range view.Attempts {
		if attempt.Path != stepID || !attempt.NeedsAttention || attempt.Status != "waiting_agent" {
			continue
		}
		executor := s.executors[attempt.ExecutorType]
		prompter, ok := executor.(interface {
			Prompt(context.Context, store.WorkflowStepAttempt, string) error
		})
		if !ok {
			return nil, fmt.Errorf("workflow step %q cannot be prompted", stepID)
		}
		message := WorkflowMessageTag + "Continue your workflow assignment now. The step is not complete; finish the declared outputs and run the exact completion command from your assignment."
		if err := prompter.Prompt(ctx, attempt, message); err != nil {
			return nil, err
		}
		return s.Show(ctx, runID)
	}
	return nil, fmt.Errorf("workflow step %q does not currently need attention", stepID)
}

// WorkflowStepFailer is an executor whose step keeps what it holds when it
// fails, for a Retry to resume (a review step keeps its loop): Fail calls
// FailStep in place of Cancel.
type WorkflowStepFailer interface {
	FailStep(ctx context.Context, attempt store.WorkflowStepAttempt) error
}

func (s *WorkflowService) Fail(ctx context.Context, runID, stepID, attemptID, reason string) (*store.WorkflowRunView, error) {
	return s.failWith(ctx, runID, stepID, attemptID, errors.New(reason))
}

// failWith is Fail with a typed failure (a WorkflowStepError carries its
// error code and link into the step's outputs), so on_failure sees it.
func (s *WorkflowService) failWith(ctx context.Context, runID, stepID, attemptID string, failure error) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	var active *store.WorkflowStepAttempt
	for i := range view.Attempts {
		attempt := &view.Attempts[i]
		if attempt.ID == attemptID && attempt.Path == stepID && (attempt.Status == "running" || attempt.Status == "waiting_agent" || attempt.Status == WorkflowStepWaiting) {
			active = attempt
			break
		}
	}
	if active == nil {
		return nil, store.ErrWorkflowStateConflict
	}
	if executor := s.executors[active.ExecutorType]; executor != nil {
		stop := executor.Cancel
		if failer, ok := executor.(WorkflowStepFailer); ok {
			stop = failer.FailStep
		}
		if err := stop(ctx, *active); err != nil {
			return nil, fmt.Errorf("stopping failed workflow agent: %w", err)
		}
	}
	if err := s.failRecorded(ctx, runID, stepID, attemptID, failure); err != nil {
		return nil, err
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func (s *WorkflowService) Retry(ctx context.Context, runID, stepID string, force bool) (*store.WorkflowRunView, error) {
	at, err := s.loadExecution(ctx, runID, stepID)
	if err != nil {
		return nil, err
	}
	if force && at.step.Type != "worktree.remove" {
		return nil, fmt.Errorf("force is only supported for worktree.remove steps")
	}
	if at.row.ParentPath != "" && at.view.Run.CurrentStepID != stepID {
		// A pass's step is retried where its run is held; a loop that
		// failed is retried as the loop.
		return nil, store.ErrWorkflowStateConflict
	}
	if err := s.endSuspendedStep(ctx, at.view, stepID); err != nil {
		return nil, err
	}
	if err := s.store.ResetWorkflowStepForRetry(ctx, runID, stepID); err != nil {
		return nil, err
	}
	s.changed(ctx, runID, stepID, WorkflowChangeStepRetried)
	if err := s.advance(ctx, runID, stepID, force); err != nil {
		return nil, err
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

func (s *WorkflowService) Cancel(ctx context.Context, runID, reason string) (*store.WorkflowRunView, error) {
	return s.end(ctx, runID, reason, s.store.CancelWorkflowRun, WorkflowChangeRunCancelled)
}

// Stop ends a running run as stopped, as Cancel ends one cancelled: each
// step under way is cancelled through its executor. It is what a gate's
// Stop does (LEG-486); the run-ended hook tells it apart from a cancel.
func (s *WorkflowService) Stop(ctx context.Context, runID, reason string) (*store.WorkflowRunView, error) {
	return s.end(ctx, runID, reason, s.store.StopWorkflowRun, WorkflowChangeRunStopped)
}

// end cancels a run's steps under way through their executors, then ends
// the run with write (cancelled or stopped).
func (s *WorkflowService) end(ctx context.Context, runID, reason string, write func(ctx context.Context, runID, reason string) error, kind string) (*store.WorkflowRunView, error) {
	view, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, attempt := range view.Attempts {
		if attempt.Status == "running" || attempt.Status == "waiting_agent" || attempt.Status == WorkflowStepWaiting {
			if executor := s.executors[attempt.ExecutorType]; executor != nil {
				if err := executor.Cancel(ctx, attempt); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := write(ctx, runID, reason); err != nil {
		return nil, err
	}
	s.dismissSuspendedAgents(ctx, view)
	s.changed(ctx, runID, "", kind)
	// Its children go after its own end is written, so that their
	// cancellation never settles one of its steps (LEG-489).
	s.cancelChildren(ctx, runID)
	return s.store.GetWorkflowRun(ctx, runID)
}

// Complete records an agent step's outputs. Where it runs inside the
// agent's own pane (a recordOnly service: the CLI), that is all: a host
// process finishes or keeps the agent, then advances the run (AdvanceDue).
// In a host it advances the run at once, in the same way.
func (s *WorkflowService) Complete(ctx context.Context, runID, stepID, attemptID string, values, artifacts map[string]string) (*store.WorkflowRunView, error) {
	at, err := s.loadExecution(ctx, runID, stepID)
	if err != nil {
		return nil, err
	}
	view := at.view
	scope, err := s.scope(ctx, at)
	if err != nil {
		return nil, err
	}
	resolved, err := at.definition.ResolveStepAt(stepID, scope)
	if err != nil {
		return nil, err
	}
	var attempt *store.WorkflowStepAttempt
	for i := range view.Attempts {
		if view.Attempts[i].ID == attemptID {
			attempt = &view.Attempts[i]
			break
		}
	}
	if attempt == nil || attempt.Path != stepID || attempt.Status != "waiting_agent" {
		return nil, store.ErrWorkflowStateConflict
	}
	outputs, err := s.collector.Collect(ctx, WorkflowOutputRequest{RunID: runID, StepID: stepID, AttemptID: attemptID, TaskID: view.Run.TaskID, WorkingDir: attempt.WorkingDir, Declared: resolved.Outputs, Values: values, Artifacts: artifacts})
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(outputs)
	if err := s.store.RecordWorkflowStepCompletion(ctx, runID, stepID, attemptID, string(encoded), at.next); err != nil {
		return nil, err
	}
	s.changed(ctx, runID, stepID, WorkflowChangeStepCompleted)
	// The stage is set where the step is recorded, while the agent's
	// session still exists: the host's Finish removes it.
	s.finishStepStage(ctx, *attempt)
	if !s.recordOnly {
		if _, err := s.advanceRecorded(ctx, runID, stepID); err != nil {
			completed, loadErr := s.store.GetWorkflowRun(ctx, runID)
			if loadErr != nil {
				return nil, err
			}
			return completed, err
		}
	}
	return s.store.GetWorkflowRun(ctx, runID)
}

// ErrWorkflowStepRecorded is matched by a completion whose step was recorded
// although what came after it failed: completing it again is a conflict.
var ErrWorkflowStepRecorded = errors.New("workflow step recorded")

// StepRecordedError is a completion that recorded its step's outputs, then
// could not go on to the next step, which has failed the run.
type StepRecordedError struct {
	StepID, NextStepID string
	Err                error
}

func (e *StepRecordedError) Error() string {
	return fmt.Sprintf("step %q was completed and its outputs recorded, but the next step %q could not start: %v", e.StepID, e.NextStepID, e.Err)
}

func (e *StepRecordedError) Unwrap() []error { return []error{ErrWorkflowStepRecorded, e.Err} }

func workflowID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// checkAgentKind refuses an agent.run step whose agent, known from the inputs
// alone, is not one of the configured agent tools.
func (s *WorkflowService) checkAgentKind(step workflowengine.Step, inputs map[string]any) error {
	raw, ok := step.With["agent"]
	if step.Type != "agent.run" || !ok || s.agentKinds == nil {
		return nil
	}
	resolved, err := workflowengine.ResolveFromInputs(raw, inputs)
	if err != nil {
		return nil
	}
	kind, _ := resolved.(string)
	if strings.TrimSpace(kind) == "" || slices.Contains(s.agentKinds, kind) {
		return nil
	}
	return fmt.Errorf("step %q: unknown agent %q; configured agents: %s", step.ID, kind, strings.Join(s.agentKinds, ", "))
}

// finishStepStage marks the agent of a step that succeeded Finished, on its
// execution's place, whichever process completed it.
func (s *WorkflowService) finishStepStage(ctx context.Context, attempt store.WorkflowStepAttempt) {
	if attempt.AgentSessionID == nil {
		return
	}
	session, err := s.store.GetAgentSessionByID(ctx, *attempt.AgentSessionID)
	if err != nil || session.ExecutionID == "" {
		return
	}
	if _, err := RecordWorkMilestone(ctx, s.store, SessionPlace(*session), WorkMilestone{
		Stage: store.WorkStageFinished, Reason: "step " + attempt.StepID + " done", ExecutionID: session.ExecutionID,
	}); err != nil {
		log.Printf("workflow: stage of step %s: %v", attempt.StepID, err)
	}
}
