package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type LearningService struct {
	store    *store.Store
	bus      *events.Bus
	resolver *ProjectIdentityResolver
}

// NewLearningService accepts an optional project resolver. Omitting it keeps
// the lightweight default used by tests while still resolving durable task
// CodeContext and Git common-directory identity when available.
func NewLearningService(s *store.Store, bus *events.Bus, resolvers ...*ProjectIdentityResolver) *LearningService {
	resolver := NewProjectIdentityResolver(nil)
	if len(resolvers) > 0 && resolvers[0] != nil {
		resolver = resolvers[0]
	}
	return &LearningService{store: s, bus: bus, resolver: resolver}
}

type LearningRecordInput struct {
	Scope        string `json:"scope"`
	ProjectKey   string `json:"project_key,omitempty"`
	ProjectLabel string `json:"project_label,omitempty"`
	Kind         string `json:"kind"`
	Claim        string `json:"claim"`
	Rationale    string `json:"rationale,omitempty"`
	TaskID       string `json:"task_id,omitempty"`
	SourceKind   string `json:"source_kind"`
	SourceRef    string `json:"source_ref"`
	Author       string `json:"author"`
}

type LearningView = store.LearningView

var ErrLearningConflict = errors.New("learning changed; refresh and try again")

func (s *LearningService) Record(ctx context.Context, input LearningRecordInput) (*LearningView, error) {
	input.Claim = strings.TrimSpace(input.Claim)
	input.Rationale = strings.TrimSpace(input.Rationale)
	if input.Scope != "user" && input.Scope != "project" {
		return nil, LearningValidationError("scope must be user or project")
	}
	if input.Scope == "project" && strings.TrimSpace(input.ProjectKey) == "" {
		return nil, LearningValidationError("project_key is required for project scope")
	}
	if input.Kind == "" || input.Claim == "" || input.SourceKind == "" || input.SourceRef == "" || input.Author == "" {
		return nil, LearningValidationError("kind, claim, source_kind, source_ref, and author are required")
	}
	digest := sha256.Sum256([]byte(input.Claim))
	digestText := "sha256:" + hex.EncodeToString(digest[:])
	if existing, err := s.store.GetLearningByEvidenceSource(ctx, input.SourceKind, input.SourceRef, digestText); err == nil {
		return s.store.GetLearningView(ctx, existing.ID)
	} else if err != store.ErrNotFound {
		return nil, err
	}
	learningID := newLearningID("learn-")
	evidenceID := newLearningID("lev-")
	evidence := store.LearningEvidence{ID: evidenceID, SourceKind: input.SourceKind, SourceRef: input.SourceRef,
		Content: input.Claim, ContentDigest: digestText, Author: input.Author, TaskID: input.TaskID,
		ProjectKey: input.ProjectKey, ProjectLabel: input.ProjectLabel, MetadataJSON: `{}`}
	learning := store.Learning{ID: learningID, Scope: input.Scope, ProjectKey: input.ProjectKey, Kind: input.Kind,
		Claim: input.Claim, Rationale: input.Rationale, Status: "candidate", Revision: 1}
	if err := s.store.InsertExplicitLearning(ctx, evidence, learning); err != nil {
		return nil, err
	}
	s.publish(learningID, "recorded")
	return s.store.GetLearningView(ctx, learningID)
}

func (s *LearningService) Show(ctx context.Context, id string) (*LearningView, error) {
	return s.store.GetLearningView(ctx, id)
}

func (s *LearningService) Accept(ctx context.Context, id string, expectedRevision int, claim, scope, projectKey string) (*LearningView, error) {
	claim = strings.TrimSpace(claim)
	if scope != "" && scope != "user" && scope != "project" {
		return nil, LearningValidationError("scope must be user or project")
	}
	if scope == "project" && strings.TrimSpace(projectKey) == "" {
		return nil, LearningValidationError("project_key is required for project scope")
	}
	if err := s.store.UpdateLearningDecision(ctx, id, expectedRevision, "validated", claim, scope, projectKey); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrLearningConflict
		}
		return nil, err
	}
	s.publish(id, "accepted")
	return s.Show(ctx, id)
}

func (s *LearningService) Reject(ctx context.Context, id string, expectedRevision int) (*LearningView, error) {
	if err := s.store.UpdateLearningDecision(ctx, id, expectedRevision, "rejected", "", "", ""); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrLearningConflict
		}
		return nil, err
	}
	s.publish(id, "rejected")
	return s.Show(ctx, id)
}

func (s *LearningService) ListIncorporations(ctx context.Context, statuses ...string) ([]store.LearningIncorporation, error) {
	return s.store.ListLearningIncorporations(ctx, statuses...)
}

func (s *LearningService) ProposeIncorporation(ctx context.Context, learningID string, expectedRevision int, destination, target, action string) (*store.LearningIncorporation, error) {
	return s.ProposeIncorporationLinked(ctx, learningID, expectedRevision, destination, target, action, "", "", "")
}

// ProposeIncorporationLinked creates the explicit user-approved request and,
// when supplied, ties its implementation to normal Legato lifecycle records.
func (s *LearningService) ProposeIncorporationLinked(ctx context.Context, learningID string, expectedRevision int, destination, target, action, taskID, workflowRunID, reviewTourID string) (*store.LearningIncorporation, error) {
	if strings.TrimSpace(destination) == "" {
		return nil, LearningValidationError("destination is required")
	}
	taskID, workflowRunID, reviewTourID = strings.TrimSpace(taskID), strings.TrimSpace(workflowRunID), strings.TrimSpace(reviewTourID)
	if err := s.store.ValidateLearningIncorporationLinks(ctx, taskID, workflowRunID, reviewTourID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, LearningValidationError("one or more incorporation lifecycle links do not exist")
		}
		return nil, err
	}
	incorporation := store.LearningIncorporation{ID: newLearningID("linc-"), LearningID: learningID,
		Destination: strings.TrimSpace(destination), Target: strings.TrimSpace(target), Action: strings.TrimSpace(action),
		ImplementationTaskID: taskID, WorkflowRunID: workflowRunID, ReviewTourID: reviewTourID,
		Status: "proposed", Revision: 1, ReferencesJSON: `[]`}
	if err := s.store.InsertLearningIncorporation(ctx, incorporation, expectedRevision); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrLearningConflict
		}
		return nil, err
	}
	s.publish(learningID, "incorporation_proposed")
	return s.store.GetLearningIncorporation(ctx, incorporation.ID)
}

func (s *LearningService) SubmitIncorporation(ctx context.Context, id string, expectedRevision int, references []string, verificationKind, result string) (*store.LearningIncorporation, error) {
	encoded, err := json.Marshal(references)
	if err != nil {
		return nil, err
	}
	if len(references) == 0 || strings.TrimSpace(verificationKind) == "" {
		return nil, LearningValidationError("references and verification kind are required")
	}
	if err := s.store.SubmitLearningIncorporation(ctx, id, expectedRevision, string(encoded), verificationKind, result); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrLearningConflict
		}
		return nil, err
	}
	inc, err := s.store.GetLearningIncorporation(ctx, id)
	if err == nil {
		s.publish(inc.LearningID, "verification_pending")
	}
	return inc, err
}

// VerifyIncorporation records an explicit verdict from a trusted user-facing
// authority (currently Attention and authenticated server actions). Automated
// implementation agents must use deterministic target verification instead.
func (s *LearningService) VerifyIncorporation(ctx context.Context, id string, expectedRevision int, passed bool, result string) (*store.LearningIncorporation, error) {
	if err := s.store.VerifyLearningIncorporation(ctx, id, expectedRevision, passed, strings.TrimSpace(result)); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrLearningConflict
		}
		return nil, err
	}
	inc, err := s.store.GetLearningIncorporation(ctx, id)
	if err == nil {
		s.publish(inc.LearningID, "verification_completed")
	}
	return inc, err
}

func (s *LearningService) publish(id, kind string) {
	if s.bus != nil {
		s.bus.Publish(events.Event{Type: events.EventLearningChanged, Payload: events.LearningChangedPayload{LearningID: id, Kind: kind}})
	}
}

func newLearningID(prefix string) string {
	var data [5]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(data[:])
}
