package service

import (
	"context"
	"errors"

	"github.com/cpave3/legato/internal/engine/managedhooks"
)

// ErrManagedHookNotFound is a name with no usable hook folder in the
// managed-hooks folder: absent, or listed with an error.
var ErrManagedHookNotFound = errors.New("no such managed hook in the managed-hooks folder")

// ManagedHookStateStore keeps whether each managed hook is on for every
// agent, by folder name.
type ManagedHookStateStore interface {
	ManagedHookStates(ctx context.Context) (map[string]bool, error)
	SetManagedHookEnabled(ctx context.Context, name string, enabled bool) error
}

// ManagedHookListing is a managed hook as Settings shows it: what its manifest
// says, and whether agents are given it. Hook is the hook as scanned, for
// what decides an agent's hooks.
type ManagedHookListing struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Events      []string `json:"events"`
	AgentTools  []string `json:"agent_tools"`
	ReadOnly    bool     `json:"read_only"`
	Path        string   `json:"path"`
	Err         string   `json:"error,omitempty"`
	Enabled     bool     `json:"enabled"`

	Hook managedhooks.Hook `json:"-"`
}

// ManagedHookService is Legato's managed hooks: the folders of the
// managed-hooks folder (managed_hooks.dir, ~/.legato/managed-hooks by
// default), found again at every ask, so a hook added, changed or removed
// shows without a restart; and whether each is on for every agent, kept in
// the database. A hook Legato has never been told about is on: putting its
// folder there is how the operator adds one. Legato never writes into the
// folder. See docs/ai/managed-hooks.md.
type ManagedHookService struct {
	dir   string
	store ManagedHookStateStore
}

// NewManagedHookService reads hooks from dir and keeps their state in store.
func NewManagedHookService(dir string, store ManagedHookStateStore) *ManagedHookService {
	return &ManagedHookService{dir: dir, store: store}
}

// Dir is the managed-hooks folder.
func (s *ManagedHookService) Dir() string { return s.dir }

// List is every hook in the managed-hooks folder, sorted by name, with its
// state. A folder that is not a usable hook is listed with its error.
func (s *ManagedHookService) List(ctx context.Context) ([]ManagedHookListing, error) {
	found, err := managedhooks.Scan(s.dir)
	if err != nil {
		return nil, err
	}
	states, err := s.store.ManagedHookStates(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]ManagedHookListing, 0, len(found))
	for _, hook := range found {
		enabled, set := states[hook.Name]
		list = append(list, managedHookView(hook, enabled || !set))
	}
	return list, nil
}

func managedHookView(hook managedhooks.Hook, enabled bool) ManagedHookListing {
	events := []string{}
	for _, e := range hook.EventNames() {
		events = append(events, string(e))
	}
	tools := append([]string{}, hook.AgentTools...)
	return ManagedHookListing{
		Name: hook.Name, Description: hook.Description, Events: events, AgentTools: tools,
		ReadOnly: hook.ReadOnly, Path: hook.Path, Err: hook.Err, Enabled: enabled, Hook: hook,
	}
}

// SetEnabled turns a hook on or off for agents started from now on. A
// running agent keeps what it was given. A hook listed with an error cannot
// be turned on or off: it is given to no agent either way.
func (s *ManagedHookService) SetEnabled(ctx context.Context, name string, enabled bool) error {
	list, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, hook := range list {
		if hook.Name == name && hook.Err == "" {
			return s.store.SetManagedHookEnabled(ctx, name, enabled)
		}
	}
	return ErrManagedHookNotFound
}
