package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type PlanService struct {
	store *store.Store
	tmux  TmuxManager
	bus   *events.Bus
}

func NewPlanService(s *store.Store, tmux TmuxManager, bus *events.Bus) *PlanService {
	return &PlanService{store: s, tmux: tmux, bus: bus}
}

type PlanOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type PlanQuestionSpec struct {
	ID                 string       `json:"id"`
	Kind               string       `json:"kind"`
	Prompt             string       `json:"prompt"`
	Rationale          string       `json:"rationale,omitempty"`
	Required           bool         `json:"required,omitempty"`
	Options            []PlanOption `json:"options,omitempty"`
	RecommendedOptions []string     `json:"recommended_options,omitempty"`
}

type PlanManifest struct {
	SchemaVersion int                `json:"schema_version"`
	Title         string             `json:"title"`
	Summary       string             `json:"summary,omitempty"`
	Questions     []PlanQuestionSpec `json:"questions,omitempty"`
}

type PlanView struct {
	Plan      store.Plan           `json:"plan"`
	Revision  store.PlanRevision   `json:"revision"`
	Questions []store.PlanQuestion `json:"questions"`
	Responses []store.PlanResponse `json:"responses"`
	Comments  []store.PlanComment  `json:"comments"`
	Messages  []store.PlanMessage  `json:"messages"`
}

type PlanQueueItem struct {
	PlanID             string `json:"plan_id"`
	TaskID             string `json:"task_id"`
	Name               string `json:"name"`
	Title              string `json:"title"`
	Summary            string `json:"summary"`
	Status             string `json:"status"`
	Revision           int    `json:"revision"`
	UnansweredRequired int    `json:"unanswered_required"`
	UpdatedAt          string `json:"updated_at"`
}

func (p *PlanService) Submit(ctx context.Context, taskID, name, bundleDir string) (*PlanView, error) {
	if _, err := p.store.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	markdown, err := os.ReadFile(filepath.Join(bundleDir, "plan.md"))
	if err != nil {
		return nil, fmt.Errorf("read plan.md: %w", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(bundleDir, "plan.json"))
	if err != nil {
		return nil, fmt.Errorf("read plan.json: %w", err)
	}
	var manifest PlanManifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode plan.json: %w", err)
	}
	if err := validatePlanManifest(manifest, string(markdown)); err != nil {
		return nil, err
	}

	planID := "pl-" + taskID
	if name != "" {
		planID += "-" + name
	}
	revisionNumber := 1
	if existing, err := p.store.GetPlanByTaskName(ctx, taskID, name); err == nil {
		planID = existing.ID
		revisionNumber = existing.LatestRevision + 1
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	revisionID := randomPlanID("pr-")
	questions := make([]store.PlanQuestion, 0, len(manifest.Questions))
	for _, spec := range manifest.Questions {
		options, _ := json.Marshal(spec.Options)
		recommended, _ := json.Marshal(spec.RecommendedOptions)
		questions = append(questions, store.PlanQuestion{
			ID: randomPlanID("pq-"), PlanID: planID, RevisionID: revisionID,
			Key: spec.ID, Kind: spec.Kind, Prompt: spec.Prompt, Rationale: spec.Rationale,
			Required: spec.Required, OptionsJSON: string(options), RecommendedJSON: string(recommended),
		})
	}
	plan := store.Plan{ID: planID, TaskID: taskID, Name: name, Title: manifest.Title,
		Summary: manifest.Summary, Status: "proposed", LatestRevision: revisionNumber}
	revision := store.PlanRevision{ID: revisionID, PlanID: planID, Revision: revisionNumber,
		Markdown: string(markdown), ManifestJSON: string(manifestBytes)}
	if err := p.store.InsertPlanRevision(ctx, plan, revision, questions); err != nil {
		return nil, err
	}
	p.publish(plan, revisionID, "submitted")
	return p.Plan(ctx, planID)
}

func validatePlanManifest(manifest PlanManifest, markdown string) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("plan.json schema_version must be 1")
	}
	if strings.TrimSpace(manifest.Title) == "" {
		return fmt.Errorf("plan.json title is required")
	}
	if strings.TrimSpace(markdown) == "" {
		return fmt.Errorf("plan.md must not be empty")
	}
	seen := map[string]bool{}
	for _, question := range manifest.Questions {
		if question.ID == "" || seen[question.ID] {
			return fmt.Errorf("plan question IDs must be non-empty and unique")
		}
		seen[question.ID] = true
		if question.Kind != "single_choice" && question.Kind != "multiple_choice" && question.Kind != "free_text" {
			return fmt.Errorf("question %q has invalid kind %q", question.ID, question.Kind)
		}
		if strings.TrimSpace(question.Prompt) == "" {
			return fmt.Errorf("question %q prompt is required", question.ID)
		}
		if question.Kind == "free_text" {
			if len(question.Options) != 0 || len(question.RecommendedOptions) != 0 {
				return fmt.Errorf("free-text question %q cannot define options", question.ID)
			}
			continue
		}
		if len(question.Options) == 0 {
			return fmt.Errorf("question %q requires options", question.ID)
		}
		optionIDs := map[string]bool{}
		for _, option := range question.Options {
			if option.ID == "" || strings.TrimSpace(option.Label) == "" || optionIDs[option.ID] {
				return fmt.Errorf("question %q option IDs must be unique and labels are required", question.ID)
			}
			optionIDs[option.ID] = true
		}
		for _, recommendation := range question.RecommendedOptions {
			if !optionIDs[recommendation] {
				return fmt.Errorf("question %q recommends unknown option %q", question.ID, recommendation)
			}
		}
		if question.Kind == "single_choice" && len(question.RecommendedOptions) > 1 {
			return fmt.Errorf("single-choice question %q can recommend at most one option", question.ID)
		}
	}
	return nil
}

func (p *PlanService) Resolve(ctx context.Context, taskID, name string) (*store.Plan, error) {
	return p.store.GetPlanByTaskName(ctx, taskID, name)
}

func (p *PlanService) Plan(ctx context.Context, planID string) (*PlanView, error) {
	plan, err := p.store.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	revision, err := p.store.GetPlanRevision(ctx, planID, plan.LatestRevision)
	if err != nil {
		return nil, err
	}
	questions, err := p.store.ListPlanQuestions(ctx, revision.ID)
	if err != nil {
		return nil, err
	}
	responses, err := p.store.ListPlanResponses(ctx, revision.ID)
	if err != nil {
		return nil, err
	}
	comments, err := p.store.ListPlanComments(ctx, planID)
	if err != nil {
		return nil, err
	}
	messages, err := p.store.ListPlanMessages(ctx, planID)
	if err != nil {
		return nil, err
	}
	if questions == nil {
		questions = []store.PlanQuestion{}
	}
	if responses == nil {
		responses = []store.PlanResponse{}
	}
	if comments == nil {
		comments = []store.PlanComment{}
	}
	if messages == nil {
		messages = []store.PlanMessage{}
	}
	return &PlanView{Plan: *plan, Revision: *revision, Questions: questions, Responses: responses, Comments: comments, Messages: messages}, nil
}

func (p *PlanService) Queue(ctx context.Context) ([]PlanQueueItem, error) {
	plans, err := p.store.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	var out []PlanQueueItem
	for _, plan := range plans {
		if plan.Status != "proposed" && plan.Status != "changes_requested" {
			continue
		}
		revision, err := p.store.GetPlanRevision(ctx, plan.ID, plan.LatestRevision)
		if err != nil {
			return nil, err
		}
		count, err := p.store.CountUnansweredRequiredPlanQuestions(ctx, revision.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, PlanQueueItem{PlanID: plan.ID, TaskID: plan.TaskID, Name: plan.Name,
			Title: plan.Title, Summary: plan.Summary, Status: plan.Status, Revision: plan.LatestRevision,
			UnansweredRequired: count, UpdatedAt: plan.UpdatedAt})
	}
	if out == nil {
		out = []PlanQueueItem{}
	}
	return out, nil
}

var ErrPlanQuestionsUnanswered = errors.New("required plan questions must be answered before approval")

type PlanResponseInput struct {
	Values []string `json:"values,omitempty"`
	Text   string   `json:"text,omitempty"`
}

func (p *PlanService) Respond(ctx context.Context, planID, questionKey string, input PlanResponseInput) error {
	view, err := p.Plan(ctx, planID)
	if err != nil {
		return err
	}
	question, err := p.store.GetPlanQuestionByKey(ctx, view.Revision.ID, questionKey)
	if err != nil {
		return err
	}
	if err := validatePlanResponse(*question, input); err != nil {
		return err
	}
	values, _ := json.Marshal(input.Values)
	if err := p.store.UpsertPlanResponse(ctx, store.PlanResponse{ID: randomPlanID("pres-"), PlanID: planID,
		RevisionID: view.Revision.ID, QuestionID: question.ID, ValuesJSON: string(values), Text: input.Text}); err != nil {
		return err
	}
	p.publish(view.Plan, view.Revision.ID, "response")
	return nil
}

func validatePlanResponse(question store.PlanQuestion, input PlanResponseInput) error {
	if question.Kind == "free_text" {
		if strings.TrimSpace(input.Text) == "" {
			return fmt.Errorf("question %q requires text", question.Key)
		}
		return nil
	}
	if len(input.Values) == 0 || (question.Kind == "single_choice" && len(input.Values) != 1) {
		return fmt.Errorf("question %q requires %s selection", question.Key, strings.ReplaceAll(question.Kind, "_", " "))
	}
	var options []PlanOption
	if err := json.Unmarshal([]byte(question.OptionsJSON), &options); err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, option := range options {
		allowed[option.ID] = true
	}
	for _, value := range input.Values {
		if !allowed[value] {
			return fmt.Errorf("question %q has no option %q", question.Key, value)
		}
	}
	return nil
}

type PlanCommentInput struct {
	Body           string `json:"body"`
	SelectionStart *int   `json:"selection_start,omitempty"`
	SelectionEnd   *int   `json:"selection_end,omitempty"`
	SelectedText   string `json:"selected_text,omitempty"`
	Prefix         string `json:"prefix,omitempty"`
	Suffix         string `json:"suffix,omitempty"`
}

func (p *PlanService) AddComment(ctx context.Context, planID string, input PlanCommentInput) (*store.PlanComment, error) {
	view, err := p.Plan(ctx, planID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Body) == "" {
		return nil, fmt.Errorf("comment body is required")
	}
	if input.SelectionStart != nil || input.SelectionEnd != nil {
		if input.SelectionStart == nil || input.SelectionEnd == nil || *input.SelectionStart < 0 || *input.SelectionEnd <= *input.SelectionStart || *input.SelectionEnd > len(view.Revision.Markdown) {
			return nil, fmt.Errorf("invalid plan text selection")
		}
		if view.Revision.Markdown[*input.SelectionStart:*input.SelectionEnd] != input.SelectedText {
			return nil, fmt.Errorf("selected text no longer matches plan revision")
		}
	}
	comment := store.PlanComment{ID: randomPlanID("pc-"), PlanID: planID, RevisionID: view.Revision.ID,
		Body: input.Body, SelectionStart: input.SelectionStart, SelectionEnd: input.SelectionEnd,
		SelectedText: input.SelectedText, Prefix: input.Prefix, Suffix: input.Suffix}
	if err := p.store.InsertPlanComment(ctx, comment); err != nil {
		return nil, err
	}
	p.publish(view.Plan, view.Revision.ID, "comment")
	return &comment, nil
}

func (p *PlanService) RequestChanges(ctx context.Context, planID string) error {
	view, err := p.Plan(ctx, planID)
	if err != nil {
		return err
	}
	if view.Plan.Status != "proposed" {
		return fmt.Errorf("plan must be proposed to request changes")
	}
	if err := p.store.SubmitPlanComments(ctx, view.Revision.ID); err != nil {
		return err
	}
	if err := p.store.UpdatePlanStatus(ctx, planID, "changes_requested"); err != nil {
		return err
	}
	p.deliver(ctx, view.Plan.TaskID, fmt.Sprintf("[legato plan] Changes requested on plan %s. Run `legato plan feedback --json` to read responses and comments, revise the bundle, then resubmit it.", planID))
	p.publish(view.Plan, view.Revision.ID, "changes_requested")
	return nil
}

func (p *PlanService) Approve(ctx context.Context, planID string) error {
	view, err := p.Plan(ctx, planID)
	if err != nil {
		return err
	}
	if view.Plan.Status != "proposed" {
		return fmt.Errorf("plan must be proposed to approve")
	}
	count, err := p.store.CountUnansweredRequiredPlanQuestions(ctx, view.Revision.ID)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrPlanQuestionsUnanswered
	}
	if err := p.store.UpdatePlanStatus(ctx, planID, "approved"); err != nil {
		return err
	}
	p.deliver(ctx, view.Plan.TaskID, fmt.Sprintf("[legato plan] Plan %s was approved. You may begin implementation.", planID))
	p.publish(view.Plan, view.Revision.ID, "approved")
	return nil
}

func (p *PlanService) Reject(ctx context.Context, planID string) error {
	view, err := p.Plan(ctx, planID)
	if err != nil {
		return err
	}
	if view.Plan.Status != "proposed" && view.Plan.Status != "changes_requested" {
		return fmt.Errorf("plan cannot be rejected from %s", view.Plan.Status)
	}
	if err := p.store.UpdatePlanStatus(ctx, planID, "rejected"); err != nil {
		return err
	}
	p.deliver(ctx, view.Plan.TaskID, fmt.Sprintf("[legato plan] Plan %s was rejected. Do not implement it unless the plan is reopened and later approved.", planID))
	p.publish(view.Plan, view.Revision.ID, "rejected")
	return nil
}

func (p *PlanService) Reopen(ctx context.Context, planID string) error {
	view, err := p.Plan(ctx, planID)
	if err != nil {
		return err
	}
	if view.Plan.Status != "rejected" {
		return fmt.Errorf("only rejected plans can be reopened")
	}
	if err := p.store.UpdatePlanStatus(ctx, planID, "proposed"); err != nil {
		return err
	}
	p.publish(view.Plan, view.Revision.ID, "reopened")
	return nil
}

func (p *PlanService) Revisions(ctx context.Context, planID string) ([]store.PlanRevision, error) {
	return p.store.ListPlanRevisions(ctx, planID)
}

func (p *PlanService) Withdraw(ctx context.Context, planID string) error {
	view, err := p.Plan(ctx, planID)
	if err != nil {
		return err
	}
	if view.Plan.Status == "approved" {
		return fmt.Errorf("approved plans cannot be withdrawn")
	}
	if err := p.store.DeletePlan(ctx, planID); err != nil {
		return err
	}
	p.publish(view.Plan, view.Revision.ID, "withdrawn")
	return nil
}

func (p *PlanService) AskQuestion(ctx context.Context, planID, text string) (string, error) {
	view, err := p.Plan(ctx, planID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("question text is required")
	}
	threadID := randomPlanID("pt-")
	line := fmt.Sprintf("[legato plan] Question on plan %s: %s — reply by running: legato plan answer %s %q", planID, text, threadID, "<your answer>")
	delivered := p.deliver(ctx, view.Plan.TaskID, line)
	if _, err := p.store.InsertPlanMessage(ctx, store.PlanMessage{PlanID: planID, RevisionID: view.Revision.ID,
		ThreadID: threadID, Kind: "question", Author: "user", Body: text}, delivered); err != nil {
		return "", err
	}
	p.publish(view.Plan, view.Revision.ID, "question")
	if !delivered {
		return threadID, ErrAgentOffline
	}
	return threadID, nil
}

func (p *PlanService) Answer(ctx context.Context, planID, threadID, text string) error {
	view, err := p.Plan(ctx, planID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("answer text is required")
	}
	if _, err := p.store.InsertPlanMessage(ctx, store.PlanMessage{PlanID: planID, RevisionID: view.Revision.ID,
		ThreadID: threadID, Kind: "answer", Author: "agent", Body: text}, true); err != nil {
		return err
	}
	p.publish(view.Plan, view.Revision.ID, "answer")
	return nil
}

func (p *PlanService) deliver(ctx context.Context, taskID, line string) bool {
	if p.tmux == nil {
		return false
	}
	session, err := p.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return false
	}
	alive, _ := p.tmux.IsAlive(session.TmuxSession)
	return alive && p.tmux.SendKeysLine(session.TmuxSession, line) == nil
}

func (p *PlanService) publish(plan store.Plan, revisionID, kind string) {
	if p.bus == nil {
		return
	}
	p.bus.Publish(events.Event{Type: events.EventPlanChanged, Payload: events.PlanChangedPayload{
		PlanID: plan.ID, TaskID: plan.TaskID, RevisionID: revisionID, Kind: kind,
	}})
}

func randomPlanID(prefix string) string {
	bytes := make([]byte, 5)
	_, _ = rand.Read(bytes)
	return prefix + hex.EncodeToString(bytes)
}
