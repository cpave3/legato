package service

import (
	"context"
	"errors"

	"github.com/cpave3/legato/internal/engine/skills"
)

// ErrSkillNotFound is a skill name with no folder in the skills folder.
var ErrSkillNotFound = errors.New("no such skill in the skills folder")

// SkillStateStore keeps whether each skill is on, by folder name.
type SkillStateStore interface {
	SkillStates(ctx context.Context) (map[string]bool, error)
	SetSkillEnabled(ctx context.Context, name string, enabled bool) error
}

// Skill is a skill as Settings shows it: what its folder says, and whether
// agents are given it.
type Skill struct {
	skills.Skill
	Enabled bool `json:"enabled"`
}

// SkillService is Legato's skills: the folders of the skills folder
// (skills.dir, ~/.legato/skills by default), found again at every ask, so a
// folder added, changed or removed shows without a restart; and whether
// each is on, kept in the database. A skill Legato has never been told about
// is on: putting its folder there is how the operator adds one. Legato
// never writes into the skills folder. See docs/ai/skills.md.
type SkillService struct {
	dir   string
	store SkillStateStore
}

// NewSkillService reads skills from dir and keeps their state in store.
func NewSkillService(dir string, store SkillStateStore) *SkillService {
	return &SkillService{dir: dir, store: store}
}

// Dir is the skills folder.
func (s *SkillService) Dir() string { return s.dir }

// List is every skill in the skills folder, sorted by name, with its state.
// A folder that is not a usable skill is listed with its error.
func (s *SkillService) List(ctx context.Context) ([]Skill, error) {
	found, err := skills.Scan(s.dir)
	if err != nil {
		return nil, err
	}
	states, err := s.store.SkillStates(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]Skill, 0, len(found))
	for _, skill := range found {
		enabled, set := states[skill.Name]
		list = append(list, Skill{Skill: skill, Enabled: enabled || !set})
	}
	return list, nil
}

// SetEnabled turns a skill on or off for agents started from now on. A
// running agent keeps what it was given.
func (s *SkillService) SetEnabled(ctx context.Context, name string, enabled bool) error {
	list, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, skill := range list {
		if skill.Name == name {
			return s.store.SetSkillEnabled(ctx, name, enabled)
		}
	}
	return ErrSkillNotFound
}

// Usable names the skills that are on and usable: what Capabilities gives
// every agent. The handoff advert, the review-pass advert and the reviews
// prompt ask it rather than filter List themselves.
func (s *SkillService) Usable(ctx context.Context) ([]string, error) {
	given, err := s.Capabilities(ctx, CapabilityRequest{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(given.Skills))
	for _, skill := range given.Skills {
		names = append(names, skill.Name)
	}
	return names, nil
}

// Capabilities gives every agent the skills that are on and usable. It is
// the one CapabilitySource there is until profiles choose for an agent.
func (s *SkillService) Capabilities(ctx context.Context, _ CapabilityRequest) (AgentCapabilities, error) {
	list, err := s.List(ctx)
	if err != nil {
		return AgentCapabilities{}, err
	}
	var given AgentCapabilities
	for _, skill := range list {
		if skill.Enabled && skill.OK() {
			given.Skills = append(given.Skills, skill.Skill)
		}
	}
	return given, nil
}
