package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

const learningPromptHeading = "## Learned context"

// LearningPromptCompileInput describes a launch-context resolution without
// coupling compilation to an agent service or adapter implementation.
type LearningPromptCompileInput struct {
	AgentSessionID *int
	TaskID         string
	Adapter        string
	Role           string
	ProjectKey     string
	MaxItems       int
	MaxBytes       int
}

type LearningPromptVersion struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

type LearningPromptExclusion struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type LearningPromptPreview struct {
	Rendered   string                    `json:"rendered"`
	Included   []LearningPromptVersion   `json:"included"`
	Excluded   []LearningPromptExclusion `json:"excluded"`
	ProjectKey string                    `json:"project_key,omitempty"`
}

type CompiledLearningPrompt struct {
	Rendered string                       `json:"rendered"`
	Included []LearningPromptVersion      `json:"included"`
	Excluded []LearningPromptExclusion    `json:"excluded"`
	Manifest store.LearningPromptManifest `json:"manifest"`
}

// CompileLearningPrompt deterministically renders and persists a compact
// standing-context section. It performs no inference and does not launch an
// agent. Only validated/incorporated learnings backed by a currently verified
// prompt_profile incorporation are eligible.
func (s *LearningService) PreviewLearningPrompt(ctx context.Context, projectKey string, maxItems, maxBytes int) (*LearningPromptPreview, error) {
	if maxItems <= 0 || maxBytes <= 0 {
		return nil, fmt.Errorf("positive prompt item and byte limits are required")
	}
	projectKey = strings.TrimSpace(projectKey)
	candidates, err := s.store.ListLearningPromptCandidates(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	result := &LearningPromptPreview{Included: []LearningPromptVersion{}, Excluded: []LearningPromptExclusion{}, ProjectKey: projectKey}
	lines := make([]string, 0, len(candidates))
	for _, item := range candidates {
		if !activePromptLearning(item.Status) {
			result.Excluded = append(result.Excluded, LearningPromptExclusion{ID: item.ID, Reason: "inactive"})
			continue
		}
		if !item.PromptEnabled {
			result.Excluded = append(result.Excluded, LearningPromptExclusion{ID: item.ID, Reason: "disabled"})
			continue
		}
		if len(result.Included) >= maxItems {
			result.Excluded = append(result.Excluded, LearningPromptExclusion{ID: item.ID, Reason: "item_limit"})
			continue
		}
		line := "- " + compactPromptClaim(item.Claim)
		trial := learningPromptHeading + "\n" + strings.Join(append(lines, line), "\n")
		if len([]byte(trial)) > maxBytes {
			result.Excluded = append(result.Excluded, LearningPromptExclusion{ID: item.ID, Reason: "byte_limit"})
			continue
		}
		lines = append(lines, line)
		result.Included = append(result.Included, LearningPromptVersion{ID: item.ID, Revision: item.Revision})
	}
	if len(lines) > 0 {
		result.Rendered = learningPromptHeading + "\n" + strings.Join(lines, "\n")
	}
	return result, nil
}

func (s *LearningService) PreviewLearningPromptForTask(ctx context.Context, taskID string, maxItems, maxBytes int) (*LearningPromptPreview, error) {
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
	return s.PreviewLearningPrompt(ctx, projectKey, maxItems, maxBytes)
}

// CompileLearningPrompt deterministically renders and persists a compact
// standing-context section. It performs no inference and does not launch an
// agent. Only explicitly enabled accepted learnings are eligible.
func (s *LearningService) CompileLearningPrompt(ctx context.Context, input LearningPromptCompileInput) (*CompiledLearningPrompt, error) {
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.Adapter = strings.TrimSpace(input.Adapter)
	input.Role = strings.TrimSpace(input.Role)
	input.ProjectKey = strings.TrimSpace(input.ProjectKey)
	if input.TaskID == "" || input.Adapter == "" {
		return nil, fmt.Errorf("task_id and adapter are required")
	}
	preview, err := s.PreviewLearningPrompt(ctx, input.ProjectKey, input.MaxItems, input.MaxBytes)
	if err != nil {
		return nil, err
	}
	result := &CompiledLearningPrompt{Rendered: preview.Rendered, Included: preview.Included, Excluded: preview.Excluded}
	includedJSON, err := json.Marshal(result.Included)
	if err != nil {
		return nil, err
	}
	excludedJSON, err := json.Marshal(result.Excluded)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(result.Rendered))
	result.Manifest = store.LearningPromptManifest{ID: newLearningID("lpm-"), AgentSessionID: input.AgentSessionID,
		TaskID: input.TaskID, Adapter: input.Adapter, Role: input.Role, ProjectKey: input.ProjectKey,
		IncludedJSON: string(includedJSON), ExcludedJSON: string(excludedJSON), RenderedDigest: "sha256:" + hex.EncodeToString(digest[:])}
	if err := s.store.CreateLearningPromptManifest(ctx, result.Manifest); err != nil {
		return nil, err
	}
	persisted, err := s.store.GetLearningPromptManifest(ctx, result.Manifest.ID)
	if err != nil {
		return nil, err
	}
	result.Manifest = *persisted
	return result, nil
}

// ShowPromptManifest returns the persisted audit record for a compiled prompt.
func (s *LearningService) ShowPromptManifest(ctx context.Context, id string) (*store.LearningPromptManifest, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("prompt manifest ID is required")
	}
	return s.store.GetLearningPromptManifest(ctx, id)
}

func activePromptLearning(status string) bool {
	return status == "validated" || status == "incorporated"
}

func compactPromptClaim(claim string) string {
	return strings.Join(strings.Fields(claim), " ")
}
