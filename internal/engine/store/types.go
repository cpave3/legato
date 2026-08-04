package store

import (
	"encoding/json"
	"time"
)

// CodeContext identifies the local repository state associated with a task.
type CodeContext struct {
	Repo         string `json:"repo,omitempty"`
	Branch       string `json:"branch,omitempty"`
	HeadSHA      string `json:"head_sha,omitempty"`
	WorkingDir   string `json:"working_dir,omitempty"`
	BaseBranch   string `json:"base_branch,omitempty"`
	RepoSource   string `json:"repo_source,omitempty"`
	BranchSource string `json:"branch_source,omitempty"`
	LinkedAt     string `json:"linked_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// MarshalCodeContext serializes task code context for storage.
func MarshalCodeContext(context *CodeContext) (*string, error) {
	if context == nil {
		return nil, nil
	}
	data, err := json.Marshal(context)
	if err != nil {
		return nil, err
	}
	raw := string(data)
	return &raw, nil
}

// ParseCodeContext deserializes task code context.
func ParseCodeContext(raw *string) (*CodeContext, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var context CodeContext
	if err := json.Unmarshal([]byte(*raw), &context); err != nil {
		return nil, err
	}
	return &context, nil
}

// PRMeta holds PR tracking metadata stored as JSON in the pr_meta column.
type PRMeta struct {
	Repo           string `json:"repo,omitempty"` // owner/repo format
	Branch         string `json:"branch"`
	PRNumber       int    `json:"pr_number,omitempty"`
	PRURL          string `json:"pr_url,omitempty"`
	State          string `json:"state,omitempty"` // OPEN, MERGED, CLOSED, or ""
	IsDraft        bool   `json:"is_draft,omitempty"`
	ReviewDecision string `json:"review_decision,omitempty"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or ""
	CheckStatus    string `json:"check_status,omitempty"`    // pass, fail, pending, or ""
	CommentCount   int    `json:"comment_count,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"` // RFC3339
	HeadSHA        string `json:"head_sha,omitempty"`   // commit SHA recorded at link time — anchors PR discovery
	LinkedAt       string `json:"linked_at,omitempty"`  // RFC3339 time the link was created — PRs created before this are rejected during discovery
	Pinned         bool   `json:"pinned,omitempty"`     // explicit/legacy numbered links are refreshed but not rediscovered
	MatchMethod    string `json:"match_method,omitempty"`
	SyncOutcome    string `json:"sync_outcome,omitempty"`
	SyncError      string `json:"sync_error,omitempty"`
}

// MarshalPRMeta serializes PRMeta to a JSON string pointer for storage.
func MarshalPRMeta(m *PRMeta) (*string, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

// ParsePRMeta deserializes a pr_meta JSON string into a PRMeta struct.
func ParsePRMeta(raw *string) (*PRMeta, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var m PRMeta
	if err := json.Unmarshal([]byte(*raw), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

type Task struct {
	ID                  string  `db:"id"`
	Title               string  `db:"title"`
	Description         string  `db:"description"`
	DescriptionMD       string  `db:"description_md"`
	RemoteDescriptionMD string  `db:"remote_description_md"`
	Status              string  `db:"status"`
	Priority            string  `db:"priority"`
	SortOrder           int     `db:"sort_order"`
	Provider            *string `db:"provider"`
	RemoteID            *string `db:"remote_id"`
	RemoteMeta          *string `db:"remote_meta"`
	Metadata            *string `db:"metadata"`
	PRMeta              *string `db:"pr_meta"`
	CodeContext         *string `db:"code_context"`
	WorkspaceID         *int    `db:"workspace_id"`
	ArchivedAt          *string `db:"archived_at"`
	Ephemeral           bool    `db:"ephemeral"`
	SwarmWorkingDir     *string `db:"swarm_working_dir"`
	SwarmActiveStep     int     `db:"swarm_active_step"`
	ChimeraSessionID    *string `db:"chimera_session_id"`
	Group               *string `db:"task_group"`
	WorktreePrimaryDir  *string `db:"worktree_primary_dir"`
	WorktreePath        *string `db:"worktree_path"`
	WorktreeBranch      *string `db:"worktree_branch"`
	WorktreeBaseBranch  *string `db:"worktree_base_branch"`
	CreatedAt           string  `db:"created_at"`
	UpdatedAt           string  `db:"updated_at"`
}

type ReflectionRequestStatus string

const (
	ReflectionRequestPending     ReflectionRequestStatus = "pending"
	ReflectionRequestSubmitted   ReflectionRequestStatus = "submitted"
	ReflectionRequestUnavailable ReflectionRequestStatus = "unavailable"
)

// LearningReflectionRequest is a durable callback target for one workflow attempt.
type LearningReflectionRequest struct {
	ID               string                  `db:"id" json:"id"`
	WorkflowRunID    string                  `db:"workflow_run_id" json:"workflow_run_id"`
	StepID           string                  `db:"step_id" json:"step_id"`
	AttemptID        string                  `db:"attempt_id" json:"attempt_id"`
	TaskID           string                  `db:"task_id" json:"task_id"`
	AgentSessionID   *int                    `db:"agent_session_id" json:"agent_session_id,omitempty"`
	SchemaVersion    int                     `db:"schema_version" json:"schema_version"`
	Prompt           string                  `db:"prompt" json:"prompt"`
	Status           ReflectionRequestStatus `db:"status" json:"status"`
	ObservationsJSON string                  `db:"observations_json" json:"observations_json"`
	Error            string                  `db:"error" json:"error,omitempty"`
	DeliveredAt      *string                 `db:"delivered_at" json:"delivered_at,omitempty"`
	SubmittedAt      *string                 `db:"submitted_at" json:"submitted_at,omitempty"`
	CreatedAt        string                  `db:"created_at" json:"created_at"`
	UpdatedAt        string                  `db:"updated_at" json:"updated_at"`
}

type LearningEvidence struct {
	ID             string `db:"id" json:"id"`
	SourceKind     string `db:"source_kind" json:"source_kind"`
	SourceRef      string `db:"source_ref" json:"source_ref"`
	Content        string `db:"content" json:"content"`
	ContentDigest  string `db:"content_digest" json:"content_digest"`
	Author         string `db:"author" json:"author"`
	TaskID         string `db:"task_id" json:"task_id,omitempty"`
	AgentSessionID *int   `db:"agent_session_id" json:"agent_session_id,omitempty"`
	WorkflowRunID  string `db:"workflow_run_id" json:"workflow_run_id,omitempty"`
	ProjectKey     string `db:"project_key" json:"project_key,omitempty"`
	ProjectLabel   string `db:"project_label" json:"project_label,omitempty"`
	MetadataJSON   string `db:"metadata_json" json:"metadata_json,omitempty"`
	CreatedAt      string `db:"created_at" json:"created_at"`
}

type Learning struct {
	ID                  string   `db:"id" json:"id"`
	Scope               string   `db:"scope" json:"scope"`
	ProjectKey          string   `db:"project_key" json:"project_key,omitempty"`
	Kind                string   `db:"kind" json:"kind"`
	Category            string   `db:"category" json:"category,omitempty"`
	Tags                []string `db:"-" json:"tags"`
	Claim               string   `db:"claim" json:"claim"`
	Rationale           string   `db:"rationale" json:"rationale,omitempty"`
	Status              string   `db:"status" json:"status"`
	Confidence          float64  `db:"confidence" json:"confidence"`
	PromptEnabled       bool     `db:"prompt_enabled" json:"prompt_enabled"`
	PromptDecided       bool     `db:"prompt_decided" json:"prompt_decided"`
	Revision            int      `db:"revision" json:"revision"`
	CanonicalLearningID *string  `db:"canonical_learning_id" json:"canonical_learning_id,omitempty"`
	CuratorRunID        string   `db:"curator_run_id" json:"curator_run_id,omitempty"`
	CreatedAt           string   `db:"created_at" json:"created_at"`
	UpdatedAt           string   `db:"updated_at" json:"updated_at"`
}

type LearningEmbedding struct {
	LearningID    string    `db:"learning_id" json:"learning_id"`
	Revision      int       `db:"revision" json:"revision"`
	ContentDigest string    `db:"content_digest" json:"content_digest"`
	Provider      string    `db:"provider" json:"provider,omitempty"`
	Model         string    `db:"model" json:"model"`
	Dimensions    int       `db:"dimensions" json:"dimensions"`
	VectorJSON    string    `db:"vector_json" json:"-"`
	Vector        []float32 `db:"-" json:"-"`
	Status        string    `db:"status" json:"status"`
	Error         string    `db:"error" json:"error,omitempty"`
	UpdatedAt     string    `db:"updated_at" json:"updated_at"`
}

type SemanticLearningResult struct {
	Learning Learning
	Score    float64
}

// LearningCurationGeneration is the durable, attempt-scoped input and result of
// one curator run. PacketJSON and OperationsJSON are canonical JSON documents.
type LearningCurationGeneration struct {
	ID             string  `db:"id" json:"id"`
	WorkflowRunID  string  `db:"workflow_run_id" json:"workflow_run_id,omitempty"`
	AttemptID      string  `db:"attempt_id" json:"attempt_id"`
	PacketDigest   string  `db:"packet_digest" json:"packet_digest"`
	PacketJSON     string  `db:"packet_json" json:"packet_json"`
	OperationsJSON string  `db:"operations_json" json:"operations_json"`
	Status         string  `db:"status" json:"status"`
	CreatedAt      string  `db:"created_at" json:"created_at"`
	CompletedAt    *string `db:"completed_at" json:"completed_at,omitempty"`
}

// LearningEvidenceLink is an atomic curator mutation.
type LearningEvidenceLink struct {
	LearningID string
	EvidenceID string
	Relation   string
}

type LearningProposal struct {
	ID                     string `db:"id" json:"id"`
	GenerationID           string `db:"generation_id" json:"generation_id"`
	Operation              string `db:"operation" json:"operation"`
	LearningID             string `db:"learning_id" json:"learning_id,omitempty"`
	CanonicalLearningID    string `db:"canonical_learning_id" json:"canonical_learning_id,omitempty"`
	Scope                  string `db:"scope" json:"scope,omitempty"`
	ProjectKey             string `db:"project_key" json:"project_key,omitempty"`
	Claim                  string `db:"claim" json:"claim,omitempty"`
	Rationale              string `db:"rationale" json:"rationale,omitempty"`
	Kind                   string `db:"kind" json:"kind,omitempty"`
	EvidenceIDsJSON        string `db:"evidence_ids_json" json:"evidence_ids_json"`
	Reason                 string `db:"reason" json:"reason,omitempty"`
	Status                 string `db:"status" json:"status"`
	Revision               int    `db:"revision" json:"revision"`
	TargetLearningRevision int    `db:"target_learning_revision" json:"target_learning_revision"`
	CreatedAt              string `db:"created_at" json:"created_at"`
	UpdatedAt              string `db:"updated_at" json:"updated_at"`
}

// LearningCurationApply contains only deterministic mutations already
// validated by the service layer.
type LearningCurationApply struct {
	GenerationID   string
	AttemptID      string
	PacketDigest   string
	OperationsJSON string
	Learnings      []Learning
	Links          []LearningEvidenceLink
	Incorporations []LearningIncorporation
	Proposals      []LearningProposal
}

type LearningIncorporation struct {
	ID                   string  `db:"id" json:"id"`
	LearningID           string  `db:"learning_id" json:"learning_id"`
	Destination          string  `db:"destination" json:"destination"`
	Target               string  `db:"target" json:"target,omitempty"`
	Action               string  `db:"action" json:"action,omitempty"`
	Status               string  `db:"status" json:"status"`
	Revision             int     `db:"revision" json:"revision"`
	ImplementationTaskID string  `db:"implementation_task_id" json:"implementation_task_id,omitempty"`
	WorkflowRunID        string  `db:"workflow_run_id" json:"workflow_run_id,omitempty"`
	ReviewTourID         string  `db:"review_tour_id" json:"review_tour_id,omitempty"`
	ReferencesJSON       string  `db:"references_json" json:"references_json"`
	VerificationKind     string  `db:"verification_kind" json:"verification_kind,omitempty"`
	VerificationResult   string  `db:"verification_result" json:"verification_result,omitempty"`
	TargetDigest         string  `db:"target_digest" json:"target_digest,omitempty"`
	VerifiedAt           *string `db:"verified_at" json:"verified_at,omitempty"`
	CreatedAt            string  `db:"created_at" json:"created_at"`
	UpdatedAt            string  `db:"updated_at" json:"updated_at"`
}

type TaskNote struct {
	ID        string `db:"id" json:"id"`
	TaskID    string `db:"task_id" json:"task_id"`
	Body      string `db:"body" json:"body"`
	Source    string `db:"source" json:"source"`
	RemoteID  string `db:"remote_id" json:"remote_id,omitempty"`
	CreatedAt string `db:"created_at" json:"created_at"`
}

// TaskWorktree is the durable worktree associated with a task.
type TaskWorktree struct {
	PrimaryDir string
	Path       string
	Branch     string
	BaseBranch string
}

type Workspace struct {
	ID        int     `db:"id"`
	Name      string  `db:"name"`
	Color     *string `db:"color"`
	SortOrder int     `db:"sort_order"`
}

type ColumnMapping struct {
	ID               int    `db:"id"`
	ColumnName       string `db:"column_name"`
	RemoteStatuses   string `db:"remote_statuses"`
	RemoteTransition string `db:"remote_transition"`
	SortOrder        int    `db:"sort_order"`
}

type SyncLogEntry struct {
	ID        int    `db:"id"`
	TaskID    string `db:"task_id"`
	Action    string `db:"action"`
	Detail    string `db:"detail"`
	CreatedAt string `db:"created_at"`
}

const (
	ExecutionInteractive    = "interactive"
	ExecutionWorkflowAgent  = "workflow_agent"
	ExecutionSwarmConductor = "swarm_conductor"
	ExecutionSwarmWorker    = "swarm_worker"

	ExecutionRunning   = "running"
	ExecutionSucceeded = "succeeded"
	ExecutionFailed    = "failed"
	ExecutionCancelled = "cancelled"
	ExecutionLost      = "lost"

	ActivityWorking = "working"
	ActivityWaiting = "waiting"
	ActivityError   = "error"
)

// Execution is one independently startable and terminable unit of agent work.
type Execution struct {
	ID                string     `db:"id"`
	Kind              string     `db:"kind"`
	Status            string     `db:"status"`
	TaskID            string     `db:"task_id"`
	ExecutionTaskID   string     `db:"execution_task_id"`
	TaskTitle         string     `db:"task_title"`
	WorkflowRunID     string     `db:"workflow_run_id"`
	WorkflowAttemptID string     `db:"workflow_attempt_id"`
	SwarmID           string     `db:"swarm_id"`
	SwarmSubtaskID    string     `db:"swarm_subtask_id"`
	AgentSessionID    *int       `db:"agent_session_id"`
	AgentKind         string     `db:"agent_kind"`
	Role              string     `db:"role"`
	WorkingDir        string     `db:"working_dir"`
	ProviderSessionID string     `db:"provider_session_id"`
	StartedAt         time.Time  `db:"started_at"`
	EndedAt           *time.Time `db:"ended_at"`
}

type ExecutionActivity struct {
	ID          int        `db:"id"`
	ExecutionID string     `db:"execution_id"`
	State       string     `db:"state"`
	StartedAt   time.Time  `db:"started_at"`
	EndedAt     *time.Time `db:"ended_at"`
	WorkingDir  string     `db:"working_dir"`
}

type AgentSession struct {
	ID               int     `db:"id"`
	TaskID           string  `db:"task_id"`
	ExecutionID      string  `db:"execution_id"`
	TmuxSession      string  `db:"tmux_session"`
	Command          string  `db:"command"`
	AgentKind        string  `db:"agent_kind"`
	Status           string  `db:"status"`
	Activity         string  `db:"activity"`
	Role             string  `db:"role"`
	ParentTaskID     *string `db:"parent_task_id"`
	SubtaskID        *string `db:"subtask_id"`
	StartedAt        string  `db:"started_at"`
	EndedAt          *string `db:"ended_at"`
	ChimeraSessionID *string `db:"chimera_session_id"`
	ChimeraEndpoint  *string `db:"chimera_endpoint"`
	LastEventID      *string `db:"last_event_id"`
}

// SwarmEvent is a single conductor-bound event written to the swarm_events
// inbox. Events are pulled via `legato swarm inbox <parent-id>`, which marks
// them acked. See SwarmService for the producer side.
type SwarmEvent struct {
	ID           int     `db:"id"`
	ParentTaskID string  `db:"parent_task_id"`
	SubtaskID    *string `db:"subtask_id"`
	Kind         string  `db:"kind"`
	WorkerTitle  string  `db:"worker_title"`
	Payload      string  `db:"payload"`
	CreatedAt    string  `db:"created_at"`
	AckedAt      *string `db:"acked_at"`
}

// PendingPlanEntry is a persisted proposed plan awaiting HITL approval.
type PendingPlanEntry struct {
	ID           int    `db:"id"`
	ParentTaskID string `db:"parent_task_id"`
	PlanPath     string `db:"plan_path"`
	ReplySocket  string `db:"reply_socket"`
	CreatedAt    string `db:"created_at"`
}

// ReviewTour is a per-task review packet header. A task may have multiple
// named tours. Status lifecycle: capturing → ready → reviewed.
type ReviewTour struct {
	ID              string  `db:"id" json:"id"`
	TaskID          string  `db:"task_id" json:"task_id"`
	Name            string  `db:"name" json:"name"`
	Status          string  `db:"status" json:"status"`
	Summary         string  `db:"summary" json:"summary"`
	BaseSHA         string  `db:"base_sha" json:"base_sha"`
	HeadSHA         string  `db:"head_sha" json:"head_sha"`
	RepositoryPath  string  `db:"repository_path" json:"repository_path"`
	LastReviewedSHA string  `db:"last_reviewed_sha" json:"last_reviewed_sha"`
	ReadyAt         *string `db:"ready_at" json:"ready_at,omitempty"`
	CreatedAt       string  `db:"created_at" json:"created_at"`
	UpdatedAt       string  `db:"updated_at" json:"updated_at"`
}

// ReviewStep is one reviewable unit of a tour: a commit, the synthetic dirty
// step, or a file-anchored note. Identity is the generated ID — never the SHA —
// so annotations and transcript survive re-syncs.
type ReviewPass struct {
	ID         string  `db:"id" json:"id"`
	TourID     string  `db:"tour_id" json:"tour_id"`
	Number     int     `db:"number" json:"number"`
	Status     string  `db:"status" json:"status"`
	Summary    string  `db:"summary" json:"summary"`
	Guidance   string  `db:"guidance" json:"guidance"`
	HeadSHA    string  `db:"head_sha" json:"head_sha"`
	ReadyAt    *string `db:"ready_at" json:"ready_at,omitempty"`
	ReviewedAt *string `db:"reviewed_at" json:"reviewed_at,omitempty"`
	CreatedAt  string  `db:"created_at" json:"created_at"`
	UpdatedAt  string  `db:"updated_at" json:"updated_at"`
}

type ReviewPlanRequest struct {
	ID          string   `db:"id" json:"id"`
	TaskID      string   `db:"task_id" json:"task_id"`
	TourID      string   `db:"tour_id" json:"tour_id"`
	PassID      string   `db:"pass_id" json:"pass_id"`
	FindingIDs  []string `db:"-" json:"finding_ids"`
	DeliveredAt *string  `db:"delivered_at" json:"delivered_at,omitempty"`
	CreatedAt   string   `db:"created_at" json:"created_at"`
}

type ReviewFinding struct {
	ID         string  `db:"id" json:"id"`
	TaskID     string  `db:"task_id" json:"task_id"`
	TourID     string  `db:"tour_id" json:"tour_id"`
	PassID     string  `db:"pass_id" json:"pass_id"`
	StepID     string  `db:"step_id" json:"step_id,omitempty"`
	FilePath   string  `db:"file_path" json:"file_path,omitempty"`
	HunkAnchor string  `db:"hunk_anchor" json:"hunk_anchor,omitempty"`
	LineStart  *int    `db:"line_start" json:"line_start,omitempty"`
	LineEnd    *int    `db:"line_end" json:"line_end,omitempty"`
	Body       string  `db:"body" json:"body"`
	Status     string  `db:"status" json:"status"`
	ResolvedAt *string `db:"resolved_at" json:"resolved_at,omitempty"`
	CreatedAt  string  `db:"created_at" json:"created_at"`
	UpdatedAt  string  `db:"updated_at" json:"updated_at"`
}

type ReviewPassPlan struct {
	PassID     string `db:"pass_id" json:"pass_id"`
	PlanID     string `db:"plan_id" json:"plan_id"`
	RevisionID string `db:"revision_id" json:"revision_id"`
	Revision   int    `db:"revision" json:"revision"`
	Title      string `db:"title" json:"title"`
	Markdown   string `db:"markdown" json:"markdown"`
	CreatedAt  string `db:"created_at" json:"created_at"`
}

type ReviewStep struct {
	ID               string  `db:"id" json:"id"`
	PassID           string  `db:"pass_id" json:"pass_id"`
	TaskID           string  `db:"task_id" json:"task_id"`
	TourID           string  `db:"tour_id" json:"tour_id"`
	Kind             string  `db:"kind" json:"kind"` // commit|dirty|note|chapter
	CommitSHA        string  `db:"commit_sha" json:"commit_sha"`
	Files            string  `db:"files" json:"files"` // JSON array of paths
	Title            string  `db:"title" json:"title"`
	Narration        string  `db:"narration" json:"narration"`
	Risk             string  `db:"risk" json:"risk"` // ''|low|medium|high|unsure
	OrderHint        *int    `db:"order_hint" json:"order_hint,omitempty"`
	Seq              int     `db:"seq" json:"seq"`
	SubtaskID        string  `db:"subtask_id" json:"subtask_id"`
	DirtyFingerprint string  `db:"dirty_fingerprint" json:"dirty_fingerprint"`
	ReviewedAt       *string `db:"reviewed_at" json:"reviewed_at,omitempty"`
	OrphanedAt       *string `db:"orphaned_at" json:"orphaned_at,omitempty"`
	CreatedAt        string  `db:"created_at" json:"created_at"`
	UpdatedAt        string  `db:"updated_at" json:"updated_at"`
}

// ReviewHunkNote is a durable annotation attached to hunk content within a
// review step's file diff.
type ReviewHunkNote struct {
	ID         string `db:"id" json:"id"`
	PassID     string `db:"pass_id" json:"pass_id"`
	TaskID     string `db:"task_id" json:"task_id"`
	TourID     string `db:"tour_id" json:"tour_id"`
	StepID     string `db:"step_id" json:"step_id"`
	FilePath   string `db:"file_path" json:"file_path"`
	HunkAnchor string `db:"hunk_anchor" json:"hunk_anchor"`
	LineStart  *int   `db:"line_start" json:"line_start,omitempty"`
	LineEnd    *int   `db:"line_end" json:"line_end,omitempty"`
	LineAnchor string `db:"line_anchor" json:"line_anchor,omitempty"`
	Body       string `db:"body" json:"body"`
	CreatedAt  string `db:"created_at" json:"created_at"`
	UpdatedAt  string `db:"updated_at" json:"updated_at"`
}

// ReviewChapterHunk assigns one base-to-head diff hunk to a chapter step.
type ReviewChapterHunk struct {
	ID         string `db:"id" json:"id"`
	PassID     string `db:"pass_id" json:"pass_id"`
	TaskID     string `db:"task_id" json:"task_id"`
	TourID     string `db:"tour_id" json:"tour_id"`
	StepID     string `db:"step_id" json:"step_id"`
	FilePath   string `db:"file_path" json:"file_path"`
	HunkAnchor string `db:"hunk_anchor" json:"hunk_anchor"`
	Seq        int    `db:"seq" json:"seq"`
	Generated  bool   `db:"generated" json:"generated"`
	CreatedAt  string `db:"created_at" json:"created_at"`
	UpdatedAt  string `db:"updated_at" json:"updated_at"`
}

// ReviewMessage is one Q&A transcript entry attached to a review step.
type ReviewMessage struct {
	ID          int     `db:"id" json:"id"`
	PassID      string  `db:"pass_id" json:"pass_id"`
	TaskID      string  `db:"task_id" json:"task_id"`
	TourID      string  `db:"tour_id" json:"tour_id"`
	StepID      string  `db:"step_id" json:"step_id"`
	Kind        string  `db:"kind" json:"kind"`     // question|answer
	Author      string  `db:"author" json:"author"` // user|agent
	Body        string  `db:"body" json:"body"`
	DeliveredAt *string `db:"delivered_at" json:"delivered_at,omitempty"`
	CreatedAt   string  `db:"created_at" json:"created_at"`
}

// Plan is a durable planning artifact attached to a task.
type PlanReviewOrigin struct {
	PlanID       string        `db:"plan_id" json:"plan_id"`
	ReviewPassID string        `db:"review_pass_id" json:"review_pass_id"`
	Finding      ReviewFinding `db:"-" json:"finding"`
	CreatedAt    string        `db:"created_at" json:"created_at"`
}

type Plan struct {
	ID                         string  `db:"id" json:"id"`
	TaskID                     string  `db:"task_id" json:"task_id"`
	Name                       string  `db:"name" json:"name"`
	Title                      string  `db:"title" json:"title"`
	Summary                    string  `db:"summary" json:"summary"`
	Status                     string  `db:"status" json:"status"`
	LatestRevision             int     `db:"latest_revision" json:"latest_revision"`
	ApprovedAt                 *string `db:"approved_at" json:"approved_at,omitempty"`
	RejectedAt                 *string `db:"rejected_at" json:"rejected_at,omitempty"`
	CleanupAfterImplementation bool    `db:"cleanup_after_implementation" json:"cleanup_after_implementation"`
	SourceBundlePath           string  `db:"source_bundle_path" json:"source_bundle_path"`
	CompletedAt                *string `db:"completed_at" json:"completed_at,omitempty"`
	CreatedAt                  string  `db:"created_at" json:"created_at"`
	UpdatedAt                  string  `db:"updated_at" json:"updated_at"`
}

type PlanRevision struct {
	ID           string `db:"id" json:"id"`
	PlanID       string `db:"plan_id" json:"plan_id"`
	Revision     int    `db:"revision" json:"revision"`
	Markdown     string `db:"markdown" json:"markdown"`
	ManifestJSON string `db:"manifest_json" json:"manifest_json"`
	CreatedAt    string `db:"created_at" json:"created_at"`
}

type PlanQuestion struct {
	ID              string `db:"id" json:"id"`
	PlanID          string `db:"plan_id" json:"plan_id"`
	RevisionID      string `db:"revision_id" json:"revision_id"`
	Key             string `db:"question_key" json:"key"`
	Kind            string `db:"kind" json:"kind"`
	Prompt          string `db:"prompt" json:"prompt"`
	Rationale       string `db:"rationale" json:"rationale"`
	Required        bool   `db:"required" json:"required"`
	OptionsJSON     string `db:"options_json" json:"options_json"`
	RecommendedJSON string `db:"recommended_json" json:"recommended_json"`
	CreatedAt       string `db:"created_at" json:"created_at"`
}

type PlanResponse struct {
	ID         string `db:"id" json:"id"`
	PlanID     string `db:"plan_id" json:"plan_id"`
	RevisionID string `db:"revision_id" json:"revision_id"`
	QuestionID string `db:"question_id" json:"question_id"`
	ValuesJSON string `db:"values_json" json:"values_json"`
	Text       string `db:"text" json:"text"`
	CreatedAt  string `db:"created_at" json:"created_at"`
	UpdatedAt  string `db:"updated_at" json:"updated_at"`
}

type PlanComment struct {
	ID             string  `db:"id" json:"id"`
	PlanID         string  `db:"plan_id" json:"plan_id"`
	RevisionID     string  `db:"revision_id" json:"revision_id"`
	Body           string  `db:"body" json:"body"`
	SelectionStart *int    `db:"selection_start" json:"selection_start,omitempty"`
	SelectionEnd   *int    `db:"selection_end" json:"selection_end,omitempty"`
	SelectedText   string  `db:"selected_text" json:"selected_text"`
	Prefix         string  `db:"prefix" json:"prefix"`
	Suffix         string  `db:"suffix" json:"suffix"`
	SubmittedAt    *string `db:"submitted_at" json:"submitted_at,omitempty"`
	CreatedAt      string  `db:"created_at" json:"created_at"`
}

type PlanMessage struct {
	ID          int     `db:"id" json:"id"`
	PlanID      string  `db:"plan_id" json:"plan_id"`
	RevisionID  string  `db:"revision_id" json:"revision_id"`
	ThreadID    string  `db:"thread_id" json:"thread_id"`
	Kind        string  `db:"kind" json:"kind"`
	Author      string  `db:"author" json:"author"`
	Body        string  `db:"body" json:"body"`
	DeliveredAt *string `db:"delivered_at" json:"delivered_at,omitempty"`
	CreatedAt   string  `db:"created_at" json:"created_at"`
}

type ArtifactClaim struct {
	ArtifactKind   string `db:"artifact_kind" json:"artifact_kind"`
	ArtifactID     string `db:"artifact_id" json:"artifact_id"`
	ClaimantTaskID string `db:"claimant_task_id" json:"claimant_task_id"`
	Generation     int    `db:"generation" json:"generation"`
	ClaimedAt      string `db:"claimed_at" json:"claimed_at"`
	UpdatedAt      string `db:"updated_at" json:"updated_at"`
}

type ArtifactInteraction struct {
	ID              string  `db:"id" json:"id"`
	ArtifactKind    string  `db:"artifact_kind" json:"artifact_kind"`
	ArtifactID      string  `db:"artifact_id" json:"artifact_id"`
	RevisionID      string  `db:"revision_id" json:"revision_id,omitempty"`
	ThreadID        string  `db:"thread_id" json:"thread_id,omitempty"`
	Kind            string  `db:"kind" json:"kind"`
	Body            string  `db:"body" json:"body"`
	ClaimantTaskID  string  `db:"claimant_task_id" json:"claimant_task_id"`
	ClaimGeneration int     `db:"claim_generation" json:"claim_generation"`
	AttemptCount    int     `db:"attempt_count" json:"attempt_count"`
	DeliveredAt     *string `db:"delivered_at" json:"delivered_at,omitempty"`
	AcknowledgedAt  *string `db:"acknowledged_at" json:"acknowledged_at,omitempty"`
	LastError       string  `db:"last_error" json:"last_error,omitempty"`
	CreatedAt       string  `db:"created_at" json:"created_at"`
	UpdatedAt       string  `db:"updated_at" json:"updated_at"`
}

type AttentionItem struct {
	ID                string  `db:"id" json:"id"`
	Producer          string  `db:"producer" json:"producer"`
	SourceKey         string  `db:"source_key" json:"source_key"`
	SourceRevision    string  `db:"source_revision" json:"source_revision"`
	TaskID            string  `db:"task_id" json:"task_id,omitempty"`
	WorkspaceID       *int    `db:"workspace_id" json:"workspace_id,omitempty"`
	Title             string  `db:"title" json:"title"`
	Reason            string  `db:"reason" json:"reason"`
	Detail            string  `db:"detail" json:"detail,omitempty"`
	Category          string  `db:"category" json:"category"`
	Urgency           string  `db:"urgency" json:"urgency"`
	NavigationJSON    string  `db:"navigation_json" json:"-"`
	ActionsJSON       string  `db:"actions_json" json:"-"`
	Status            string  `db:"status" json:"status"`
	DismissedRevision string  `db:"dismissed_revision" json:"dismissed_revision,omitempty"`
	CreatedAt         string  `db:"created_at" json:"created_at"`
	UpdatedAt         string  `db:"updated_at" json:"updated_at"`
	ResolvedAt        *string `db:"resolved_at" json:"resolved_at,omitempty"`
}

// Subtask represents a swarm sub-task: a unit of work parented to a task,
// owned by one worker agent, scoped to a set of file globs.
//
// Lifecycle: queued → dispatched → in_progress → reporting → done | cancelled.
// BuilderAgentID/ReviewerAgentID are kept (under their original column names)
// for migration compatibility — v1 only uses BuilderAgentID, repurposed as the
// single worker_agent_id slot.
type Subtask struct {
	ID              string  `db:"id"`
	ParentTaskID    string  `db:"parent_task_id"`
	Title           string  `db:"title"`
	Description     string  `db:"description"`
	Prompt          string  `db:"prompt"`
	ScopeGlobs      string  `db:"scope_globs"`
	Role            string  `db:"role"`
	AgentKind       string  `db:"agent_kind"`
	Tier            string  `db:"tier"`
	Status          string  `db:"status"`
	StepIndex       int     `db:"step_index"`
	BuilderAgentID  *int    `db:"builder_agent_id"`
	ReviewerAgentID *int    `db:"reviewer_agent_id"`
	CreatedAt       string  `db:"created_at"`
	DispatchedAt    *string `db:"dispatched_at"`
	StartedAt       *string `db:"started_at"`
	CompletedAt     *string `db:"completed_at"`
}
