package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// ErrRepositoryInvalid is a repository that cannot be added as given.
var ErrRepositoryInvalid = errors.New("invalid repository")

// RepositoryPreset is a repository the spawn dialogs and workflows offer: a
// GitHub repository to clone fresh, a checkout on this machine, or both.
type RepositoryPreset struct {
	// ID is set for one added in the web UI; those of the configuration
	// have none and are changed in config.yaml.
	ID         int64  `json:"id,omitempty"`
	Name       string `json:"name"`
	Repo       string `json:"repo"`
	PrimaryDir string `json:"primary_dir"`
	BaseBranch string `json:"base_branch,omitempty"`
	Configured bool   `json:"configured"`
	// CloneProblem says why Legato cannot clone Repo, "" when it can (or
	// there is no Repo).
	CloneProblem string `json:"clone_problem,omitempty"`
}

// CloneChecker says why a repository cannot be cloned (RepositoryService).
type CloneChecker interface {
	CloneProblem(repo string) string
}

// RepositoryPresetStore keeps the repositories added in the web UI.
type RepositoryPresetStore interface {
	SaveRepositoryPreset(ctx context.Context, p store.RepositoryPreset) (store.RepositoryPreset, error)
	ListRepositoryPresets(ctx context.Context) ([]store.RepositoryPreset, error)
	DeleteRepositoryPreset(ctx context.Context, id int64) error
}

// RepositoryPresetService is every repository Legato offers: those of the
// configuration, then those added in the web UI. Each read sees the latest,
// so a repository added is offered at once.
type RepositoryPresetService struct {
	saved      RepositoryPresetStore
	configured []RepositoryPreset
	home       string
	checker    CloneChecker
}

// NewRepositoryPresetService builds the catalog from the configuration's
// repositories and the store of added ones.
func NewRepositoryPresetService(saved RepositoryPresetStore, configured []RepositoryPreset, home string) *RepositoryPresetService {
	s := &RepositoryPresetService{saved: saved, home: home}
	for _, p := range configured {
		p.ID, p.Configured = 0, true
		s.configured = append(s.configured, p)
	}
	return s
}

// SetCloneChecker lets List say which repositories cannot be cloned.
func (s *RepositoryPresetService) SetCloneChecker(checker CloneChecker) { s.checker = checker }

// List returns the configuration's repositories, then the added ones.
func (s *RepositoryPresetService) List(ctx context.Context) ([]RepositoryPreset, error) {
	out := append([]RepositoryPreset(nil), s.configured...)
	if s.saved != nil {
		rows, err := s.saved.ListRepositoryPresets(ctx)
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			out = append(out, RepositoryPreset{ID: row.ID, Name: row.Name, Repo: row.Repo, PrimaryDir: row.PrimaryDir, BaseBranch: row.BaseBranch})
		}
	}
	if s.checker != nil {
		for i := range out {
			if out[i].Repo != "" {
				out[i].CloneProblem = s.checker.CloneProblem(out[i].Repo)
			}
		}
	}
	return out, nil
}

func invalidRepository(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRepositoryInvalid, fmt.Sprintf(format, args...))
}

// normalizeRepo takes owner/name, or a GitHub address, and returns owner/name.
func normalizeRepo(value string) string {
	repo := strings.TrimSpace(value)
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "git@github.com:", "ssh://git@github.com/", "github.com/"} {
		repo = strings.TrimPrefix(repo, prefix)
	}
	return strings.TrimSuffix(strings.TrimSuffix(repo, "/"), ".git")
}

func (s *RepositoryPresetService) expand(path string) string {
	path = strings.TrimSpace(path)
	if s.home != "" && (path == "~" || strings.HasPrefix(path, "~/")) {
		path = filepath.Join(s.home, strings.TrimPrefix(path, "~"))
	}
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// Save adds a repository: a name no other has, and a GitHub repository
// (owner/name), a folder on this machine, or both.
func (s *RepositoryPresetService) Save(ctx context.Context, p RepositoryPreset) (RepositoryPreset, error) {
	p.Name = strings.TrimSpace(p.Name)
	p.Repo = normalizeRepo(p.Repo)
	p.PrimaryDir = s.expand(p.PrimaryDir)
	p.BaseBranch = strings.TrimSpace(p.BaseBranch)
	if p.Name == "" {
		return RepositoryPreset{}, invalidRepository("a name is required")
	}
	if p.Repo == "" && p.PrimaryDir == "" {
		return RepositoryPreset{}, invalidRepository("give a GitHub repository (owner/name), a folder, or both")
	}
	if p.Repo != "" {
		if err := validRepo(p.Repo); err != nil {
			return RepositoryPreset{}, invalidRepository("%q is not a GitHub repository: write owner/name", p.Repo)
		}
	}
	if p.PrimaryDir != "" {
		if !filepath.IsAbs(p.PrimaryDir) {
			return RepositoryPreset{}, invalidRepository("%s is not an absolute path", p.PrimaryDir)
		}
		if !isDirectory(p.PrimaryDir) {
			return RepositoryPreset{}, invalidRepository("%s is not a folder", p.PrimaryDir)
		}
	}
	if strings.ContainsAny(p.BaseBranch, " \t\n") {
		return RepositoryPreset{}, invalidRepository("%q is not a branch name", p.BaseBranch)
	}
	for _, c := range s.configured {
		if strings.EqualFold(c.Name, p.Name) {
			return RepositoryPreset{}, invalidRepository("%s is already a repository of config.yaml", p.Name)
		}
	}
	if s.saved == nil {
		return RepositoryPreset{}, errors.New("repositories cannot be added here")
	}
	row, err := s.saved.SaveRepositoryPreset(ctx, store.RepositoryPreset{Name: p.Name, Repo: p.Repo, PrimaryDir: p.PrimaryDir, BaseBranch: p.BaseBranch})
	if errors.Is(err, store.ErrConflict) {
		return RepositoryPreset{}, invalidRepository("%s is already a repository", p.Name)
	}
	if err != nil {
		return RepositoryPreset{}, err
	}
	p.ID, p.Configured = row.ID, false
	if s.checker != nil && p.Repo != "" {
		p.CloneProblem = s.checker.CloneProblem(p.Repo)
	}
	return p, nil
}

// Delete removes a repository added in the web UI.
func (s *RepositoryPresetService) Delete(ctx context.Context, id int64) error {
	if s.saved == nil {
		return store.ErrNotFound
	}
	return s.saved.DeleteRepositoryPreset(ctx, id)
}

// WorkflowPresets is every repository as a workflow repository preset.
func (s *RepositoryPresetService) WorkflowPresets(ctx context.Context) ([]workflowengine.RepositoryPreset, error) {
	all, err := s.List(ctx)
	out := make([]workflowengine.RepositoryPreset, 0, len(all))
	for _, p := range all {
		out = append(out, workflowengine.RepositoryPreset{Name: p.Name, Repo: p.Repo, PrimaryDir: p.PrimaryDir, BaseBranch: p.BaseBranch})
	}
	return out, err
}

// DirectoryPresets is every repository with a folder, as a place to start
// an agent.
func (s *RepositoryPresetService) DirectoryPresets(ctx context.Context) ([]DirectoryPreset, error) {
	all, err := s.List(ctx)
	var out []DirectoryPreset
	for _, p := range all {
		if p.PrimaryDir != "" {
			out = append(out, DirectoryPreset{Name: p.Name, Path: s.expand(p.PrimaryDir)})
		}
	}
	return out, err
}
