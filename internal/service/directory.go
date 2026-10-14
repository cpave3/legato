package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// DirectoryPreset is a named directory an agent can be started in: a
// repository from the configuration, or a directory the person saved.
type DirectoryPreset struct {
	Name string `json:"name"`
	// Path is absolute.
	Path string `json:"path"`
	// Display is Path with the home directory written as a tilde.
	Display string `json:"display"`
	// Saved is true for a directory the person saved, and false for one that
	// comes from the configuration.
	Saved bool `json:"saved"`
}

// DirectorySuggestion is one directory that completes a typed path.
type DirectorySuggestion struct {
	Name string `json:"name"`
	// Path is absolute, without a trailing separator.
	Path string `json:"path"`
	// Display is Path with the home directory written as a tilde, and a
	// trailing separator, so that it can replace what was typed.
	Display string `json:"display"`
	// Git is true when the directory is a repository or a worktree of one.
	Git bool `json:"git"`
	// Preset is the directory's place among the presets, counted from 1, or
	// 0 when it is not one.
	Preset int `json:"preset"`
}

// DirectorySuggestions is what completes one typed path.
type DirectorySuggestions struct {
	// Query is the path as it was typed.
	Query string `json:"query"`
	// Resolved is the absolute path Query names, and Exists says whether it
	// is a directory.
	Resolved string `json:"resolved"`
	Exists   bool   `json:"exists"`
	// Base is the directory that was searched, as it is displayed, and Prefix
	// the start of a name that was typed after it.
	Base   string `json:"base"`
	Prefix string `json:"prefix"`
	// Items are the matches that are kept, and Total how many matched.
	Items []DirectorySuggestion `json:"items"`
	Total int                   `json:"total"`
}

// DirectoryPresetStore keeps the directories the person saved.
type DirectoryPresetStore interface {
	SaveDirectoryPreset(ctx context.Context, preset store.DirectoryPreset) error
	ListDirectoryPresets(ctx context.Context) ([]store.DirectoryPreset, error)
}

// DirectoryService answers where an agent can be started: the presets, and
// the directories that complete a path as it is typed.
type DirectoryService struct {
	presets    DirectoryPresetStore
	configured []DirectoryPreset
	home       string
}

// NewDirectoryService makes a directory service. presets may be nil, in which
// case nothing can be saved. configured are the presets of the configuration,
// by name and path. home is the directory a leading tilde stands for.
func NewDirectoryService(presets DirectoryPresetStore, configured []DirectoryPreset, home string) *DirectoryService {
	s := &DirectoryService{presets: presets, home: filepath.Clean(home)}
	if home == "" {
		s.home = ""
	}
	for _, preset := range configured {
		path := s.Expand(preset.Path)
		if path == "" {
			continue
		}
		name := strings.TrimSpace(preset.Name)
		if name == "" {
			name = filepath.Base(path)
		}
		s.configured = append(s.configured, DirectoryPreset{Name: name, Path: path, Display: s.Display(path)})
	}
	return s
}

// Expand turns a typed path into an absolute one: a leading tilde becomes the
// home directory, and a trailing separator is dropped. A path that is neither
// absolute nor under the home directory is returned cleaned, as typed.
func (s *DirectoryService) Expand(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if s.home != "" {
		if path == "~" {
			return s.home
		}
		if rest, ok := strings.CutPrefix(path, "~/"); ok {
			return filepath.Join(s.home, rest)
		}
	}
	return filepath.Clean(path)
}

// Display writes an absolute path with the home directory as a tilde.
func (s *DirectoryService) Display(path string) string {
	if s.home == "" || path == "" {
		return path
	}
	if path == s.home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, s.home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}

// Presets returns the presets of the configuration, then the directories the
// person saved that are not among them.
func (s *DirectoryService) Presets(ctx context.Context) ([]DirectoryPreset, error) {
	presets := append([]DirectoryPreset(nil), s.configured...)
	if s.presets == nil {
		return presets, nil
	}
	saved, err := s.presets.ListDirectoryPresets(ctx)
	if err != nil {
		return presets, fmt.Errorf("list directory presets: %w", err)
	}
	known := make(map[string]bool, len(presets))
	for _, preset := range presets {
		known[preset.Path] = true
	}
	for _, preset := range saved {
		if known[preset.Path] {
			continue
		}
		known[preset.Path] = true
		presets = append(presets, DirectoryPreset{Name: preset.Name, Path: preset.Path, Display: s.Display(preset.Path), Saved: true})
	}
	return presets, nil
}

// SavePreset keeps a directory as a preset, named after the directory when
// name is empty. A directory that is already a preset is returned as it is.
func (s *DirectoryService) SavePreset(ctx context.Context, path, name string) (DirectoryPreset, error) {
	path = s.Expand(path)
	if path == "" {
		return DirectoryPreset{}, fmt.Errorf("a directory is required")
	}
	if !filepath.IsAbs(path) {
		return DirectoryPreset{}, fmt.Errorf("%s is not an absolute path", path)
	}
	if !isDirectory(path) {
		return DirectoryPreset{}, fmt.Errorf("%s is not a directory", s.Display(path))
	}
	existing, err := s.Presets(ctx)
	if err != nil {
		return DirectoryPreset{}, err
	}
	for _, preset := range existing {
		if preset.Path == path {
			return preset, nil
		}
	}
	if s.presets == nil {
		return DirectoryPreset{}, fmt.Errorf("directory presets cannot be saved here")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(path)
	}
	if err := s.presets.SaveDirectoryPreset(ctx, store.DirectoryPreset{Path: path, Name: name}); err != nil {
		return DirectoryPreset{}, fmt.Errorf("save directory preset: %w", err)
	}
	return DirectoryPreset{Name: name, Path: path, Display: s.Display(path), Saved: true}, nil
}

// Suggest returns the directories that complete a typed path, at most limit
// of them: the directories under the last directory typed in full whose names
// start with what follows it. A directory typed in full comes first, then the
// presets in their own order, then repositories, then the rest; hidden
// directories are left out until a dot is typed. A path that cannot be read
// has no suggestions, which is not an error: most of what is typed on the way
// to a path names nothing yet.
func (s *DirectoryService) Suggest(ctx context.Context, query string, limit int) (DirectorySuggestions, error) {
	result := DirectorySuggestions{Query: query, Items: []DirectorySuggestion{}}
	typed := strings.TrimSpace(query)
	resolved := s.Expand(typed)
	if resolved == "" || !filepath.IsAbs(resolved) {
		return result, nil
	}
	result.Resolved = resolved
	result.Exists = isDirectory(resolved)

	dir, prefix := filepath.Dir(resolved), filepath.Base(resolved)
	if strings.HasSuffix(typed, string(filepath.Separator)) || typed == "~" || resolved == string(filepath.Separator) {
		dir, prefix = resolved, ""
	}
	result.Prefix = prefix
	result.Base = s.Display(dir)
	if !strings.HasSuffix(result.Base, string(filepath.Separator)) {
		result.Base += string(filepath.Separator)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return result, nil
	}
	presets, err := s.Presets(ctx)
	if err != nil {
		return result, err
	}
	place := make(map[string]int, len(presets))
	for i, preset := range presets {
		place[preset.Path] = i + 1
	}

	folded := strings.ToLower(prefix)
	var matches []DirectorySuggestion
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), folded) {
			continue
		}
		path := filepath.Join(dir, name)
		if !entry.IsDir() && (entry.Type()&os.ModeSymlink == 0 || !isDirectory(path)) {
			continue
		}
		_, gitErr := os.Lstat(filepath.Join(path, ".git"))
		matches = append(matches, DirectorySuggestion{
			Name:    name,
			Path:    path,
			Display: result.Base + name + string(filepath.Separator),
			Git:     gitErr == nil,
			Preset:  place[path],
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if exactA, exactB := a.Name == prefix, b.Name == prefix; exactA != exactB {
			return exactA
		}
		if (a.Preset > 0) != (b.Preset > 0) {
			return a.Preset > 0
		}
		if a.Preset != b.Preset {
			return a.Preset < b.Preset
		}
		if a.Git != b.Git {
			return a.Git
		}
		if la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name); la != lb {
			return la < lb
		}
		return a.Name < b.Name
	})

	result.Total = len(matches)
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	if matches != nil {
		result.Items = matches
	}
	return result, nil
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
