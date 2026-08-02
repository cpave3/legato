package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/store"
)

// MaxCollectedEvidenceBytes bounds each independently attributable artifact.
// Collection never includes plan markdown, diffs, generated review narration,
// or general agent transcripts.
const MaxCollectedEvidenceBytes = 16 * 1024

// CollectTaskEvidence collects deliberately authored plan and review feedback.
// Project may be nil when repository identity is not known. Recollection is
// idempotent by the exact source identity and digest of the bounded content.
func (s *LearningService) CollectTaskEvidence(ctx context.Context, taskID string, project *ProjectIdentity) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("task_id is required")
	}
	projectKey, projectLabel := "", ""
	if project != nil {
		projectKey = strings.TrimSpace(project.Key)
		projectLabel = strings.TrimSpace(project.Label)
	}
	put := func(kind, ref, author, content string) error {
		content = boundEvidenceText(content)
		if strings.TrimSpace(content) == "" {
			return nil
		}
		sum := sha256.Sum256([]byte(content))
		_, err := s.store.UpsertLearningEvidence(ctx, store.LearningEvidence{
			ID: newLearningID("lev-"), SourceKind: kind, SourceRef: ref,
			Content: content, ContentDigest: "sha256:" + hex.EncodeToString(sum[:]), Author: author,
			TaskID: taskID, ProjectKey: projectKey, ProjectLabel: projectLabel, MetadataJSON: `{}`,
		})
		return err
	}

	plans, err := s.store.ListPlansByTask(ctx, taskID)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		comments, err := s.store.ListPlanComments(ctx, plan.ID)
		if err != nil {
			return err
		}
		for _, comment := range comments {
			// Draft comments are mutable UI state, not deliberately submitted feedback.
			if comment.SubmittedAt != nil {
				if err := put("plan_comment", comment.ID, "user", comment.Body); err != nil {
					return err
				}
			}
		}
		revisions, err := s.store.ListPlanRevisions(ctx, plan.ID)
		if err != nil {
			return err
		}
		for _, revision := range revisions {
			responses, err := s.store.ListPlanResponses(ctx, revision.ID)
			if err != nil {
				return err
			}
			for _, response := range responses {
				content := response.Text
				if strings.TrimSpace(content) == "" {
					content = response.ValuesJSON
				}
				if err := put("plan_response", response.ID, "user", content); err != nil {
					return err
				}
			}
		}
	}

	tours, err := s.store.ListReviewToursByTask(ctx, taskID)
	if err != nil {
		return err
	}
	for _, tour := range tours {
		passes, err := s.store.ListReviewPasses(ctx, tour.ID)
		if err != nil {
			return err
		}
		for _, pass := range passes {
			if err := put("review_guidance", pass.ID, "user", pass.Guidance); err != nil {
				return err
			}
			messages, err := s.store.ListReviewMessagesByPass(ctx, pass.ID)
			if err != nil {
				return err
			}
			for _, message := range messages {
				if message.Kind != "question" && message.Kind != "answer" {
					continue
				}
				if err := put("review_"+message.Kind, fmt.Sprint(message.ID), message.Author, message.Body); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func boundEvidenceText(text string) string {
	if len(text) <= MaxCollectedEvidenceBytes {
		return text
	}
	text = text[:MaxCollectedEvidenceBytes]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}
