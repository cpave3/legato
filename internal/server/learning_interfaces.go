package server

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// LearningService is the narrow surface used by the learning HTTP API.
type LearningService interface {
	List(context.Context, service.LearningFilter) ([]store.Learning, error)
	Search(context.Context, string, service.LearningFilter) ([]store.Learning, error)
	Context(context.Context, string, int) ([]store.Learning, error)
	ContextForTask(context.Context, string, int) ([]store.Learning, error)
	Record(context.Context, service.LearningRecordInput) (*service.LearningView, error)
	Show(context.Context, string) (*service.LearningView, error)
	Accept(context.Context, string, int, string, string, string) (*service.LearningView, error)
	Reject(context.Context, string, int) (*service.LearningView, error)
	SetPromptEnabled(context.Context, string, int, bool) (*service.LearningView, error)
	ProposeIncorporationLinked(context.Context, string, int, string, string, string, string, string, string) (*store.LearningIncorporation, error)
	SubmitIncorporation(context.Context, string, int, []string, string, string) (*store.LearningIncorporation, error)
	VerifyIncorporation(context.Context, string, int, bool, string) (*store.LearningIncorporation, error)
	VerifyIncorporationTarget(context.Context, string, int, string, bool) (*store.LearningIncorporation, error)
	CheckIncorporationDrift(context.Context, string) ([]service.IncorporationDriftResult, error)
	SubmitReflectionJSON(context.Context, string, []byte) (*service.ReflectionSubmission, error)
	ShowPromptManifest(context.Context, string) (*store.LearningPromptManifest, error)
	PreviewLearningPromptForTask(context.Context, string, int, int) (*service.LearningPromptPreview, error)
}

// ReflectionWorkflowService advances the exact workflow attempt stored on a
// successfully persisted reflection request.
type ReflectionWorkflowService interface {
	Complete(context.Context, string, string, string, map[string]string, map[string]string) (*store.WorkflowRunView, error)
}
