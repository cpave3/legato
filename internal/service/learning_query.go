package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// LearningFilter controls scoped list and search retrieval.
type LearningFilter struct {
	Status     string `json:"status,omitempty"`
	Scope      string `json:"scope,omitempty"`
	ProjectKey string `json:"project_key,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

func validateLearningFilter(filter LearningFilter) error {
	if filter.Scope != "" && filter.Scope != "user" && filter.Scope != "project" {
		return LearningValidationError("scope must be user or project")
	}
	if filter.Scope == "project" && strings.TrimSpace(filter.ProjectKey) == "" {
		return LearningValidationError("project_key is required for project scope")
	}
	if filter.Limit < 0 {
		return LearningValidationError("limit must not be negative")
	}
	return nil
}

func storeLearningQuery(filter LearningFilter) store.LearningQuery {
	return store.LearningQuery{Status: strings.TrimSpace(filter.Status), Scope: filter.Scope,
		ProjectKey: strings.TrimSpace(filter.ProjectKey), Limit: filter.Limit}
}

func (s *LearningService) List(ctx context.Context, filter LearningFilter) ([]store.Learning, error) {
	if err := validateLearningFilter(filter); err != nil {
		return nil, err
	}
	return s.store.ListLearnings(ctx, storeLearningQuery(filter))
}

func (s *LearningService) Search(ctx context.Context, text string, filter LearningFilter) ([]store.Learning, error) {
	if strings.TrimSpace(text) == "" {
		return nil, LearningValidationError("search text is required")
	}
	if err := validateLearningFilter(filter); err != nil {
		return nil, err
	}
	return s.store.SearchLearnings(ctx, text, storeLearningQuery(filter))
}

// Context returns only standing active user and matching-project learnings.
func (s *LearningService) Context(ctx context.Context, projectKey string, limit int) ([]store.Learning, error) {
	if limit < 0 {
		return nil, LearningValidationError("limit must not be negative")
	}
	return s.store.ListLearningContext(ctx, strings.TrimSpace(projectKey), limit)
}

// ContextForTask uses the same durable repository, checkout inspection, and Git
// common-directory fallback as automatic curation. An unresolved checkout does
// not invent project scope, so user context remains available.
func (s *LearningService) ContextForTask(ctx context.Context, taskID string, limit int) ([]store.Learning, error) {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	identity, resolved, err := s.projectIdentityForTask(task)
	if err != nil {
		return nil, err
	}
	projectKey := ""
	if resolved {
		projectKey = identity.Key
	}
	return s.Context(ctx, projectKey, limit)
}

func (s *LearningService) projectIdentityForTask(task *store.Task) (ProjectIdentity, bool, error) {
	codeContext, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		return ProjectIdentity{}, false, fmt.Errorf("parse task project context: %w", err)
	}
	directory := ""
	if codeContext != nil {
		directory = strings.TrimSpace(codeContext.WorkingDir)
	}
	for _, candidate := range []*string{task.WorktreePath, task.WorktreePrimaryDir, task.SwarmWorkingDir} {
		if directory == "" && candidate != nil {
			directory = strings.TrimSpace(*candidate)
		}
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = NewProjectIdentityResolver(nil)
	}
	identity, err := resolver.Resolve(codeContext, directory)
	if err != nil {
		return ProjectIdentity{}, false, nil
	}
	return identity, identity.Key != "", nil
}
