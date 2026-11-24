package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

func legacyWorkflowDirPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".legato", "workflows")
}

type WorkflowService interface {
	Start(context.Context, *workflowengine.Definition, map[string]string) (*store.WorkflowRunView, error)
	ListRuns(context.Context, store.WorkflowRunFilter) ([]store.WorkflowRun, error)
	Show(context.Context, string) (*store.WorkflowRunView, error)
	Cancel(context.Context, string, string) (*store.WorkflowRunView, error)
	Retry(context.Context, string, string, bool) (*store.WorkflowRunView, error)
	TriggerStep(context.Context, string, string, bool) (*store.WorkflowRunView, error)
	PromptStep(context.Context, string, string) (*store.WorkflowRunView, error)
	Artifact(context.Context, string) (*store.WorkflowArtifact, error)
}

// SetBuiltInWorkflows adds Legato's own workflows to those found on disk. A
// file with the same workflow name replaces a built-in one.
func (s *Server) SetBuiltInWorkflows(definitions ...*workflowengine.Definition) {
	s.builtInWorkflows = append([]*workflowengine.Definition(nil), definitions...)
}

func (s *Server) discoverWorkflows() workflowengine.DiscoveryResult {
	return s.discoverWorkflowsIn(context.Background())
}

// discoverWorkflowsIn reads every workflow folder, repositories' included,
// then adds the built-ins no file replaces.
func (s *Server) discoverWorkflowsIn(ctx context.Context) workflowengine.DiscoveryResult {
	return workflowengine.WithBuiltIns(workflowengine.DiscoverFolders(s.workflowFolderList(ctx)), s.builtInWorkflows...)
}

// definitionWithGraph is a definition as GET /api/workflows gives it: with
// its workflow graph. The graph is never stored with the definition, whose
// JSON a run keeps and digests.
type definitionWithGraph struct {
	*workflowengine.Definition
	Graph *workflowengine.Graph `json:"graph"`
	// Where its file is and whether the web UI may write it, the files of
	// the same name it replaces, and the warnings its file has (such as a
	// with key its step's executor does not read).
	Folder         string                      `json:"folder,omitempty"`
	Editable       bool                        `json:"editable"`
	ReadOnlyReason string                      `json:"read_only_reason,omitempty"`
	Shadows        []string                    `json:"shadows,omitempty"`
	Warnings       []workflowengine.Diagnostic `json:"warnings,omitempty"`
}

func (s *Server) definitionWithGraph(definition *workflowengine.Definition, folders []workflowengine.Folder, shadows map[string][]string) definitionWithGraph {
	info := workflowengine.FileFor(definition, folders)
	out := definitionWithGraph{Definition: definition, Graph: workflowengine.BuildGraph(definition), Folder: info.Folder, Editable: info.Editable, ReadOnlyReason: info.Reason, Shadows: shadows[definition.Name]}
	if info.Path != "" {
		if data, err := os.ReadFile(info.Path); err == nil {
			_, out.Warnings = workflowengine.Check(info.Path, data)
		}
	}
	return out
}

// runWithGraph is a run as GET /api/workflow-runs/{id} gives it: with the
// workflow graph of the run's own copy of its definition (never the current
// file), and whether the current file differs from that copy or is gone.
type runWithGraph struct {
	*store.WorkflowRunView
	Graph             *workflowengine.Graph `json:"graph,omitempty"`
	DefinitionChanged bool                  `json:"definition_changed"`
	DefinitionMissing bool                  `json:"definition_missing"`
}

func (s *Server) runWithGraph(view *store.WorkflowRunView) runWithGraph {
	out := runWithGraph{WorkflowRunView: view}
	if view == nil {
		return out
	}
	var copy workflowengine.Definition
	parsed := json.Unmarshal([]byte(view.Run.DefinitionJSON), &copy) == nil && len(copy.Steps) > 0
	if parsed {
		out.Graph = workflowengine.BuildGraph(&copy)
	}
	out.DefinitionMissing = true
	for _, current := range s.discoverWorkflows().Definitions {
		if current.Name == view.Run.WorkflowName {
			out.DefinitionMissing = false
			// Compared without their sources: the same workflow in a file
			// that moved has not changed.
			if parsed {
				here := *current
				here.Source, copy.Source = "", ""
				out.DefinitionChanged = workflowengine.Digest(&here) != workflowengine.Digest(&copy)
			} else {
				out.DefinitionChanged = workflowengine.Digest(current) != view.Run.DefinitionDigest
			}
			break
		}
	}
	return out
}

func (s *Server) workflowsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		folders := s.workflowFolderList(r.Context())
		result := s.discoverWorkflowsIn(r.Context())
		result.Repositories = s.repositories(r.Context())
		var states map[string][]service.WorkflowTriggerState
		if s.workflowTriggers != nil {
			var err error
			if states, err = s.workflowTriggers.TriggerStates(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		definitions := make([]definitionWithGraph, 0, len(result.Definitions))
		for _, definition := range result.Definitions {
			definitions = append(definitions, s.definitionWithGraph(definition, folders, result.Shadows))
		}
		s.writeJSON(w, http.StatusOK, struct {
			workflowengine.DiscoveryResult
			Definitions []definitionWithGraph `json:"definitions"`
			// TriggerStates is each workflow's triggers, on or paused here.
			TriggerStates map[string][]service.WorkflowTriggerState `json:"trigger_states,omitempty"`
			// Folders are where a new workflow can be made.
			Folders []workflowengine.Folder `json:"folders"`
		}{result, definitions, states, s.writableFolders(r.Context())})
	}
}

// workflowTriggerSwitchHandler serves PUT /api/workflows/{name}/triggers/
// {trigger} {enabled}: the trigger on or paused in this Legato, never in
// its file; DELETE leaves it to the file again.
func (s *Server) workflowTriggerSwitchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflowTriggers == nil {
			s.writeError(w, http.StatusServiceUnavailable, "workflow triggers are not available")
			return
		}
		name, trigger := r.PathValue("name"), r.PathValue("trigger")
		var err error
		switch r.Method {
		case http.MethodPut:
			var body struct {
				Enabled *bool `json:"enabled"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Enabled == nil {
				s.writeError(w, http.StatusBadRequest, "want {\"enabled\": true|false}")
				return
			}
			err = s.workflowTriggers.SetTriggerEnabled(r.Context(), name, trigger, *body.Enabled)
		case http.MethodDelete:
			err = s.workflowTriggers.ResetTrigger(r.Context(), name, trigger)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		switch {
		case errors.Is(err, service.ErrNoSuchTrigger):
			s.writeError(w, http.StatusNotFound, err.Error())
		case err != nil:
			s.writeError(w, http.StatusInternalServerError, err.Error())
		default:
			s.notifyWorkflowDefinitionsChanged()
			s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
	}
}
func (s *Server) workflowStartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := r.PathValue("name")
		result := s.discoverWorkflows()
		var definition *workflowengine.Definition
		for _, item := range result.Definitions {
			if item.Name == name {
				definition = item
				break
			}
		}
		if definition == nil {
			http.Error(w, "workflow not found", http.StatusNotFound)
			return
		}
		var body struct {
			Inputs map[string]string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		view, err := s.workflows.Start(r.Context(), definition, body.Inputs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.NotifyWorkflowChanged(view.Run.ID, view.Run.CurrentStepID)
		s.writeJSON(w, http.StatusCreated, view)
	}
}

var workflowInputKeyPattern = regexp.MustCompile(`^[^[:cntrl:].\[\]$]+$`)

func parseWorkflowRunFilter(r *http.Request) (store.WorkflowRunFilter, error) {
	query := r.URL.Query()
	for _, name := range []string{"status", "workflow_name", "limit"} {
		if len(query[name]) > 1 {
			return store.WorkflowRunFilter{}, fmt.Errorf("query parameter %q must not be repeated", name)
		}
	}
	filter := store.WorkflowRunFilter{
		Status:       query.Get("status"),
		WorkflowName: query.Get("workflow_name"),
		Inputs:       map[string]string{},
		Limit:        100,
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return filter, fmt.Errorf("limit must be between 1 and 100")
		}
		filter.Limit = limit
	}
	for name, values := range query {
		if !strings.HasPrefix(name, "input.") {
			continue
		}
		key := strings.TrimPrefix(name, "input.")
		if !workflowInputKeyPattern.MatchString(key) || len(values) != 1 || values[0] == "" {
			return filter, fmt.Errorf("invalid workflow input filter %q", name)
		}
		filter.Inputs[key] = values[0]
	}
	return filter, nil
}

func (s *Server) workflowRunsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		filter, err := parseWorkflowRunFilter(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		filter.WithSteps = true
		runs, err := s.workflows.ListRuns(r.Context(), filter)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if runs == nil {
			runs = []store.WorkflowRun{}
		}
		s.writeJSON(w, 200, runs)
	}
}
func (s *Server) workflowRunHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		view, err := s.workflows.Show(r.Context(), r.PathValue("run_id"))
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		s.writeJSON(w, 200, s.runWithGraph(view))
	}
}
func (s *Server) workflowCancelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", 503)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		view, err := s.workflows.Cancel(r.Context(), r.PathValue("run_id"), "cancelled from web")
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		s.NotifyWorkflowChanged(view.Run.ID, view.Run.CurrentStepID)
		s.writeJSON(w, 200, view)
	}
}

type workflowStepOptions struct {
	Force bool `json:"force"`
}

func decodeWorkflowStepOptions(r *http.Request) (workflowStepOptions, error) {
	var options workflowStepOptions
	if r.Body == nil || r.ContentLength == 0 {
		return options, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
		return options, err
	}
	return options, nil
}

func (s *Server) workflowRetryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", 503)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		options, err := decodeWorkflowStepOptions(r)
		if err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		view, err := s.workflows.Retry(r.Context(), r.PathValue("run_id"), r.PathValue("step_id"), options.Force)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		s.NotifyWorkflowChanged(view.Run.ID, view.Run.CurrentStepID)
		s.writeJSON(w, 200, view)
	}
}
func (s *Server) workflowTriggerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		options, err := decodeWorkflowStepOptions(r)
		if err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		view, err := s.workflows.TriggerStep(r.Context(), r.PathValue("run_id"), r.PathValue("step_id"), options.Force)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		s.NotifyWorkflowChanged(view.Run.ID, view.Run.CurrentStepID)
		s.writeJSON(w, http.StatusOK, view)
	}
}

func (s *Server) workflowPromptHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		view, err := s.workflows.PromptStep(r.Context(), r.PathValue("run_id"), r.PathValue("step_id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		s.NotifyWorkflowChanged(view.Run.ID, view.Run.CurrentStepID)
		s.writeJSON(w, http.StatusOK, view)
	}
}

// WorkflowAgentResumer is a workflow service whose runs can resume their
// card's agent with a macro (service.WorkflowService.ResumeAgent).
type WorkflowAgentResumer interface {
	ResumeAgent(ctx context.Context, runID, macro string) (service.MacroStartResult, error)
}

// workflowResumeAgentHandler is POST /api/workflow-runs/{run_id}/resume-agent
// {"macro": name}: the run's card agent starts with the macro, or the one
// running gets it as its next message. 409 when the run cannot (finished,
// a step's agent at work, the agent waiting on the operator, its folder
// gone with no repository), 404 for an unknown run or macro.
func (s *Server) workflowResumeAgentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resumer, ok := s.workflows.(WorkflowAgentResumer)
		if !ok {
			http.Error(w, "resuming agents is unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Macro string `json:"macro"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil || strings.TrimSpace(body.Macro) == "" {
			http.Error(w, "name the macro to resume the agent with", http.StatusBadRequest)
			return
		}
		runID := r.PathValue("run_id")
		result, err := resumer.ResumeAgent(r.Context(), runID, body.Macro)
		switch {
		case err == nil:
		case errors.Is(err, store.ErrNotFound), errors.Is(err, service.ErrMacroNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		case errors.Is(err, store.ErrWorkflowStateConflict), errors.Is(err, service.ErrResumeAgentWaiting), errors.Is(err, service.ErrMacroFolderGone):
			http.Error(w, err.Error(), http.StatusConflict)
			return
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.NotifyWorkflowChanged(runID, "")
		s.writeJSON(w, http.StatusOK, result)
	}
}

func (s *Server) workflowArtifactHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", 503)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		artifact, err := s.workflows.Artifact(r.Context(), r.PathValue("artifact_id"))
		if err != nil {
			http.Error(w, "artifact not found", http.StatusNotFound)
			return
		}
		if artifact.MediaType == "text/markdown" || artifact.MediaType == "text/plain" {
			w.Header().Set("Content-Type", artifact.MediaType)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
			filename := filepath.Base(artifact.OriginalPath)
			if filename == "." || filename == string(filepath.Separator) || filename == "" {
				filename = artifact.OutputKey
			}
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
		}
		http.ServeFile(w, r, artifact.ManagedPath)
	}
}

func (s *Server) NotifyWorkflowChanged(runID, stepID string) {
	s.NotifyWorkflowChangedKind(runID, stepID, "")
}

// NotifyWorkflowChangedKind is NotifyWorkflowChanged with what changed, such
// as completed_step from a CLI process, which the advance loop acts on.
// It is published on the bus, and StartWorkflowEvents sends it to the web
// UI from there, once; without that forwarder it is sent here.
func (s *Server) NotifyWorkflowChangedKind(runID, stepID, kind string) {
	if s.bus != nil {
		s.bus.Publish(events.Event{Type: events.EventWorkflowChanged, Payload: events.WorkflowChangedPayload{RunID: runID, StepID: stepID, Kind: kind}})
	}
	if s.bus == nil || !s.workflowForwarding.Load() {
		s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged, WorkflowRunID: runID, WorkflowStepID: stepID, Kind: kind})
	}
}
