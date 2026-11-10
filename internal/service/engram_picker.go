package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/engram"
	"github.com/cpave3/legato/internal/engine/store"
)

// The marks an engram can carry in the picker.
const (
	EngramMarkLast    = "last"
	EngramMarkDefault = "default"
)

// EngramPickerRequest is where an agent will start: a folder (Dir) or a
// repository a job clone will be made of (Repo, owner/name), and its task.
type EngramPickerRequest struct {
	Dir    string
	Repo   string
	TaskID string
}

// EngramPlace is the place bindings are kept under.
type EngramPlace struct {
	Key   string `json:"key"`
	Label string `json:"label"` // the key without its prefix
	Kind  string `json:"kind"`  // repo or folder
	// Default is the place's default engram, "" for none. It may be an
	// engram engram no longer lists.
	Default string `json:"default,omitempty"`
}

// EngramChoice is one engram in the picker.
type EngramChoice struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// Mark is last (the task's last engram) or default (the place's).
	Mark string `json:"mark,omitempty"`
	// Bound is true for an engram bound to the place.
	Bound bool `json:"bound,omitempty"`
}

// EngramPicker is what a spawn surface offers, in the order to list it
// after No engram, built once here so the TUI and the web show the same.
type EngramPicker struct {
	EngramStatus
	Place     *EngramPlace   `json:"place,omitempty"`
	Engrams   []EngramChoice `json:"engrams"`
	Preselect string         `json:"preselect"`
}

// Picker lists the engrams an agent can start as where req says, with the
// place's bindings and the task's last engram:
//
//   - pre-selected: the task's last engram, else the place's default,
//     else none;
//   - in order: the task's last engram, the default, the other bound
//     engrams by last use (newest first), then every other engram A-Z.
//
// An engram engram does not list is left out, so a default that is gone
// falls back to none.
func (s *EngramService) Picker(ctx context.Context, req EngramPickerRequest) (EngramPicker, error) {
	status := s.Status(ctx)
	picker := EngramPicker{EngramStatus: status, Engrams: []EngramChoice{}}
	if !status.Enabled {
		return picker, nil
	}
	summaries := map[string]string{}
	for _, id := range status.Engrams {
		summaries[id.Name] = id.Summary
	}
	listed := func(name string) bool { _, ok := summaries[name]; return name != "" && ok }

	place, bindings, err := s.place(ctx, req)
	if err != nil {
		return picker, err
	}
	picker.Place = place

	last := ""
	if req.TaskID != "" && s.opts.Store != nil {
		if t, err := s.opts.Store.LastTaskEngram(ctx, req.TaskID); err == nil && listed(t.Engram) {
			last = t.Engram
		}
	}
	def := ""
	if place != nil && listed(place.Default) {
		def = place.Default
	}
	bound := map[string]bool{}
	for _, b := range bindings {
		bound[b.Engram] = true
	}

	seen := map[string]bool{}
	add := func(name, mark string) {
		if seen[name] || !listed(name) {
			return
		}
		seen[name] = true
		picker.Engrams = append(picker.Engrams, EngramChoice{Name: name, Summary: summaries[name], Mark: mark, Bound: bound[name]})
	}
	add(last, EngramMarkLast)
	add(def, EngramMarkDefault)
	others := append([]store.EngramBinding{}, bindings...)
	sort.SliceStable(others, func(i, j int) bool {
		a, b := others[i].LastUsedAt, others[j].LastUsedAt
		switch {
		case a == nil && b == nil:
			return others[i].Engram < others[j].Engram
		case a == nil || b == nil:
			return b == nil
		}
		return a.After(*b)
	})
	for _, b := range others {
		add(b.Engram, "")
	}
	rest := make([]string, 0, len(summaries))
	for name := range summaries {
		rest = append(rest, name)
	}
	sort.Strings(rest)
	for _, name := range rest {
		add(name, "")
	}

	switch {
	case last != "":
		picker.Preselect = last
	case def != "":
		picker.Preselect = def
	}
	return picker, nil
}

// place is the place of req and the bindings that match it. For a folder,
// a binding matches when its key is one of the folder's scopes, and a
// repo: match beats a dir: one; with none, the place is engram's primary
// key. A job clone's place is its repository's key.
func (s *EngramService) place(ctx context.Context, req EngramPickerRequest) (*EngramPlace, []store.EngramBinding, error) {
	var scopes []string
	switch {
	case strings.TrimSpace(req.Repo) != "":
		scopes = []string{engram.RepoKey("", strings.TrimSpace(req.Repo))}
	case strings.TrimSpace(req.Dir) != "":
		cli := s.cli(s.config(ctx))
		if cli == nil {
			return nil, nil, nil
		}
		place, err := cli.Place(ctx, strings.TrimSpace(req.Dir))
		if err != nil {
			// A folder engram cannot name (one that does not exist yet)
			// has no place: no bindings, nothing to remember.
			return nil, nil, nil
		}
		scopes = place.Scopes
		if len(scopes) == 0 {
			scopes = []string{place.Primary}
		}
	default:
		return nil, nil, nil
	}
	key := scopes[0]
	var matched []store.EngramBinding
	if s.opts.Store != nil {
		all, err := s.opts.Store.EngramBindings(ctx, scopes)
		if err != nil {
			return nil, nil, err
		}
		byKey := map[string][]store.EngramBinding{}
		for _, b := range all {
			byKey[b.PlaceKey] = append(byKey[b.PlaceKey], b)
		}
		// repo: keys first, in engram's order, then dir: ones.
		ordered := append([]string{}, scopes...)
		sort.SliceStable(ordered, func(i, j int) bool {
			return engram.KindOf(ordered[i]) == engram.KindRepo && engram.KindOf(ordered[j]) != engram.KindRepo
		})
		for _, scope := range ordered {
			if len(byKey[scope]) > 0 {
				key, matched = scope, byKey[scope]
				break
			}
		}
	}
	place := &EngramPlace{Key: key, Label: engram.Label(key), Kind: engram.KindOf(key)}
	for _, b := range matched {
		if b.IsDefault {
			place.Default = b.Engram
		}
	}
	return place, matched, nil
}

// ErrNoEngramPlace says there is no place to remember an engram for.
var ErrNoEngramPlace = errors.New("there is no repository or folder to remember an engram for")

// Remember is the spawn dialog's Remember: with an engram, it binds it to
// the place (if it is not bound already) and makes it the default, keeping
// the old default bound; with none, it clears the place's default.
func (s *EngramService) Remember(ctx context.Context, req EngramPickerRequest, name string) error {
	if s.opts.Store == nil {
		return ErrNoEngramPlace
	}
	place, _, err := s.place(ctx, req)
	if err != nil {
		return err
	}
	if place == nil {
		return ErrNoEngramPlace
	}
	if name == "" {
		return s.opts.Store.ClearEngramDefault(ctx, place.Key)
	}
	return s.opts.Store.BindEngram(ctx, place.Key, name, true, time.Now())
}

// Touch records that an engram was spawned in dir: each binding of it to
// one of dir's scopes gets the time.
func (s *EngramService) Touch(ctx context.Context, dir, name string) error {
	if s.opts.Store == nil || name == "" {
		return nil
	}
	cli := s.cli(s.config(ctx))
	if cli == nil {
		return nil
	}
	place, err := cli.Place(ctx, dir)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, scope := range place.Scopes {
		if err := s.opts.Store.TouchEngramBinding(ctx, scope, name, now); err != nil {
			return err
		}
	}
	return nil
}
