package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/overlay"
)

// spawnSuggestionLimit is how many directories the spawn overlay is given
// for a typed path. It draws as many as it has room for.
const spawnSuggestionLimit = 8

// DirectoryService answers where an agent can be started: the directory
// presets, and the directories that complete a path as it is typed.
type DirectoryService interface {
	Presets(ctx context.Context) ([]service.DirectoryPreset, error)
	SavePreset(ctx context.Context, path, name string) (service.DirectoryPreset, error)
	Suggest(ctx context.Context, query string, limit int) (service.DirectorySuggestions, error)
	Expand(path string) string
	Display(path string) string
}

// SetDirectoryService makes the spawn overlay list the directory presets,
// complete a typed path and save one as a preset. Without one, the overlay
// offers the default directory and a path typed in full.
func (a *App) SetDirectoryService(svc DirectoryService) {
	a.dirSvc = svc
}

// withSpawnLocations gives the spawn overlay the presets and the default
// directory as it is displayed.
func (a App) withSpawnLocations(model overlay.AgentSpawnOverlay, defaultDir string) overlay.AgentSpawnOverlay {
	if a.dirSvc == nil {
		return model
	}
	presets, _ := a.dirSvc.Presets(context.Background())
	return model.
		WithLocations(overlay.SpawnDir{Path: defaultDir, Display: a.dirSvc.Display(defaultDir)}, spawnDirs(presets)).
		WithSaving()
}

func spawnDirs(presets []service.DirectoryPreset) []overlay.SpawnDir {
	dirs := make([]overlay.SpawnDir, len(presets))
	for i, preset := range presets {
		dirs[i] = overlay.SpawnDir{Name: preset.Name, Path: preset.Path, Display: preset.Display}
	}
	return dirs
}

// suggestSpawnDirectories reads the directories that complete a typed path.
// A path that cannot be read has none.
func (a App) suggestSpawnDirectories(query string) tea.Cmd {
	svc := a.dirSvc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		found, _ := svc.Suggest(context.Background(), query, spawnSuggestionLimit)
		msg := overlay.AgentSpawnSuggestionsMsg{Query: query, Base: found.Base, Prefix: found.Prefix, Total: found.Total}
		for _, item := range found.Items {
			msg.Items = append(msg.Items, overlay.SpawnSuggestion{Name: item.Name, Path: item.Path, Display: item.Display, Git: item.Git})
		}
		return msg
	}
}

// saveSpawnPreset keeps a directory as a preset and hands the overlay the
// presets as they are afterwards.
func (a App) saveSpawnPreset(path string) tea.Cmd {
	svc := a.dirSvc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		saved, err := svc.SavePreset(ctx, path, "")
		if err != nil {
			return overlay.AgentSpawnPresetsMsg{Err: err.Error()}
		}
		presets, err := svc.Presets(ctx)
		if err != nil {
			return overlay.AgentSpawnPresetsMsg{Err: err.Error()}
		}
		return overlay.AgentSpawnPresetsMsg{Presets: spawnDirs(presets), Saved: saved.Path}
	}
}

// ResourceService lists the resources an agent can be given and finds them
// by name.
type ResourceService interface {
	List(ctx context.Context) ([]service.Resource, error)
	Resolve(ctx context.Context, names []string) ([]service.Resource, error)
}

// SetResourceService makes the spawn overlay offer resources to give the
// agent. Without one, it has no resources field.
func (a *App) SetResourceService(svc ResourceService) {
	a.resourceSvc = svc
}

// withSpawnResources gives the spawn overlay the resources it can offer.
func (a App) withSpawnResources(model overlay.AgentSpawnOverlay) overlay.AgentSpawnOverlay {
	if a.resourceSvc == nil {
		return model
	}
	resources, err := a.resourceSvc.List(context.Background())
	if err != nil || len(resources) == 0 {
		return model
	}
	offered := make([]overlay.SpawnResource, len(resources))
	for i, resource := range resources {
		offered[i] = overlay.SpawnResource{Name: resource.Name}
		for _, path := range resource.Paths {
			if path.Access == "write" {
				offered[i].Write = true
			}
		}
	}
	return model.WithResources(offered)
}

// spawnResources finds the resources that were picked in the overlay.
func (a App) spawnResources(names []string) ([]service.Resource, error) {
	if len(names) == 0 || a.resourceSvc == nil {
		return nil, nil
	}
	return a.resourceSvc.Resolve(context.Background(), names)
}
