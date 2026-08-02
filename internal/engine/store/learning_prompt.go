package store

import (
	"context"
	"database/sql"
)

// LearningPromptCandidate is a scoped ledger entry considered for boot-time
// prompt composition. The service records exclusions for disabled/inactive rows.
type LearningPromptCandidate struct {
	Learning
}

// LearningPromptManifest records the exact ledger versions used to render one
// standing-context section. The JSON fields are canonical documents produced
// by the learning service.
type LearningPromptManifest struct {
	ID             string `db:"id" json:"id"`
	AgentSessionID *int   `db:"agent_session_id" json:"agent_session_id,omitempty"`
	TaskID         string `db:"task_id" json:"task_id"`
	Adapter        string `db:"adapter" json:"adapter"`
	Role           string `db:"role" json:"role,omitempty"`
	ProjectKey     string `db:"project_key" json:"project_key,omitempty"`
	IncludedJSON   string `db:"included_json" json:"included_json"`
	ExcludedJSON   string `db:"excluded_json" json:"excluded_json"`
	RenderedDigest string `db:"rendered_digest" json:"rendered_digest"`
	CreatedAt      string `db:"created_at" json:"created_at"`
}

// ListLearningPromptCandidates returns every learning in the resolved user and
// project scopes. Eligibility is deliberately reported rather than filtered so
// the service can audit why raw and non-prompt learnings were omitted.
func (s *Store) ListLearningPromptCandidates(ctx context.Context, projectKey string) ([]LearningPromptCandidate, error) {
	args := []any{}
	scope := "l.scope = 'user'"
	if projectKey != "" {
		scope += " OR (l.scope = 'project' AND l.project_key = ?)"
		args = append(args, projectKey)
	}
	var candidates []LearningPromptCandidate
	err := s.db.SelectContext(ctx, &candidates, `SELECT l.*
		FROM learnings l WHERE (`+scope+`)
		ORDER BY CASE l.scope WHEN 'user' THEN 0 ELSE 1 END, l.id ASC`, args...)
	if candidates == nil {
		candidates = []LearningPromptCandidate{}
	}
	return candidates, err
}

func (s *Store) CreateLearningPromptManifest(ctx context.Context, manifest LearningPromptManifest) error {
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO learning_prompt_manifests
		(id, agent_session_id, task_id, adapter, role, project_key, included_json, excluded_json, rendered_digest)
		VALUES (:id, :agent_session_id, :task_id, :adapter, :role, :project_key, :included_json, :excluded_json, :rendered_digest)`, manifest)
	return err
}

// BindLearningPromptManifestSession associates a pre-launch prompt resolution
// with the durable session created from that exact prompt.
func (s *Store) BindLearningPromptManifestSession(ctx context.Context, id string, agentSessionID int) error {
	result, err := s.db.ExecContext(ctx, `UPDATE learning_prompt_manifests SET agent_session_id = ? WHERE id = ?`, agentSessionID, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetLearningPromptManifest(ctx context.Context, id string) (*LearningPromptManifest, error) {
	var manifest LearningPromptManifest
	if err := s.db.GetContext(ctx, &manifest, `SELECT * FROM learning_prompt_manifests WHERE id = ?`, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &manifest, nil
}

func (s *Store) ListLearningPromptManifestsBySession(ctx context.Context, agentSessionID int) ([]LearningPromptManifest, error) {
	var manifests []LearningPromptManifest
	err := s.db.SelectContext(ctx, &manifests, `SELECT * FROM learning_prompt_manifests
		WHERE agent_session_id = ? ORDER BY created_at DESC, id ASC`, agentSessionID)
	if manifests == nil {
		manifests = []LearningPromptManifest{}
	}
	return manifests, err
}
