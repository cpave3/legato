package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

var (
	// ErrRepositoryGuidanceInvalid is guidance that names no repository or
	// has no text.
	ErrRepositoryGuidanceInvalid = errors.New("invalid repository guidance")
	// ErrRepositoryGuidanceConfigured is a change to guidance that
	// config.yaml sets, which is changed there.
	ErrRepositoryGuidanceConfigured = errors.New("this repository's guidance is set in config.yaml")
)

// RepositoryGuidance is text Legato adds to the launch prompt of agents that
// work in one repository, whichever checkout or worktree they are in.
type RepositoryGuidance struct {
	// Repo is host/owner/name, as config.NormalizeRepository writes it.
	Repo string `json:"repo"`
	Text string `json:"text"`
	// Configured is guidance from config.yaml, which is changed there.
	Configured bool `json:"configured"`
}

// RepositoryGuidanceSource gives the guidance for a repository (host/owner/name),
// "" when there is none.
type RepositoryGuidanceSource interface {
	GuidanceFor(ctx context.Context, repo string) (string, error)
}

// RepositoryGuidanceStore keeps the guidance saved in the web UI.
type RepositoryGuidanceStore interface {
	SaveRepositoryGuidance(ctx context.Context, g store.RepositoryGuidance) error
	ListRepositoryGuidance(ctx context.Context) ([]store.RepositoryGuidance, error)
	DeleteRepositoryGuidance(ctx context.Context, repo string) error
}

// RepositoryGuidanceService is the guidance of config.yaml, then the guidance
// saved in the web UI. Each read sees the latest, so an agent started after a
// save gets it.
type RepositoryGuidanceService struct {
	saved      RepositoryGuidanceStore
	configured []RepositoryGuidance
}

// NewRepositoryGuidanceService builds the guidance from the configuration's
// entries, which config.Load has normalised, and the store of saved ones
// (nil: none can be saved).
func NewRepositoryGuidanceService(saved RepositoryGuidanceStore, configured []config.AgentGuidance) *RepositoryGuidanceService {
	s := &RepositoryGuidanceService{saved: saved}
	for _, g := range configured {
		s.configured = append(s.configured, RepositoryGuidance{Repo: g.Repo, Text: g.Text, Configured: true})
	}
	return s
}

// List is the configuration's guidance, then the saved.
func (s *RepositoryGuidanceService) List(ctx context.Context) ([]RepositoryGuidance, error) {
	all := append([]RepositoryGuidance{}, s.configured...)
	if s.saved == nil {
		return all, nil
	}
	saved, err := s.saved.ListRepositoryGuidance(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range saved {
		all = append(all, RepositoryGuidance{Repo: g.Repo, Text: g.Text})
	}
	return all, nil
}

// Save sets the guidance of a repository, named as a remote names it.
func (s *RepositoryGuidanceService) Save(ctx context.Context, repo, text string) (RepositoryGuidance, error) {
	name, ok := config.NormalizeRepository(repo)
	if !ok {
		return RepositoryGuidance{}, fmt.Errorf("%w: %q is not a repository: give owner/name, host/owner/name or a Git remote address", ErrRepositoryGuidanceInvalid, repo)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return RepositoryGuidance{}, fmt.Errorf("%w: the guidance for %s is empty", ErrRepositoryGuidanceInvalid, name)
	}
	if s.isConfigured(name) {
		return RepositoryGuidance{}, fmt.Errorf("%w: %s", ErrRepositoryGuidanceConfigured, name)
	}
	if s.saved == nil {
		return RepositoryGuidance{}, errors.New("repository guidance cannot be saved here")
	}
	if err := s.saved.SaveRepositoryGuidance(ctx, store.RepositoryGuidance{Repo: name, Text: text}); err != nil {
		return RepositoryGuidance{}, err
	}
	return RepositoryGuidance{Repo: name, Text: text}, nil
}

// Remove forgets a repository's saved guidance; none is store.ErrNotFound.
func (s *RepositoryGuidanceService) Remove(ctx context.Context, repo string) error {
	name, ok := config.NormalizeRepository(repo)
	if !ok {
		return fmt.Errorf("%w: %q is not a repository", ErrRepositoryGuidanceInvalid, repo)
	}
	if s.isConfigured(name) {
		return fmt.Errorf("%w: %s", ErrRepositoryGuidanceConfigured, name)
	}
	if s.saved == nil {
		return store.ErrNotFound
	}
	return s.saved.DeleteRepositoryGuidance(ctx, name)
}

// GuidanceFor implements RepositoryGuidanceSource.
func (s *RepositoryGuidanceService) GuidanceFor(ctx context.Context, repo string) (string, error) {
	for _, g := range s.configured {
		if g.Repo == repo {
			return g.Text, nil
		}
	}
	all, err := s.List(ctx)
	for _, g := range all {
		if g.Repo == repo {
			return g.Text, nil
		}
	}
	return "", err
}

func (s *RepositoryGuidanceService) isConfigured(repo string) bool {
	for _, g := range s.configured {
		if g.Repo == repo {
			return true
		}
	}
	return false
}

// repositoryGuidancePrompt is the launch prompt section for a repository's
// guidance.
func repositoryGuidancePrompt(repo, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return "## Guidance for " + repo + "\n\n" + text
}

// reviewsOffPrompt tells an agent that the person does not want Legato's
// review tours and handoff packets.
const reviewsOffPrompt = "## Reviews\n\n" +
	"Legato reviews are off. Do not capture a review tour, run `legato review` commands, " +
	"or write a review handoff packet, and do not use the legato-review skill, " +
	"unless the person asks for a review."

// reviewsOnPrompt asks an agent whose launch prompt teaches nothing of
// reviews for a review tour at the end of its work.
const reviewsOnPrompt = "## Reviews\n\n" +
	"The person reviews your work in Legato as a guided tour. When a piece of work " +
	"is done, use the legato-review skill to capture a review tour and mark it ready. " +
	"If `LEGATO_TASK_EPHEMERAL=1`, skip it unless the person asks for a review."
