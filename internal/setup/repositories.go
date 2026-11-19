package setup

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

// RepositoryPresets are the repositories Legato offers when starting agents
// and workflows (service.RepositoryPresetService).
type RepositoryPresets interface {
	List(ctx context.Context) ([]service.RepositoryPreset, error)
	Save(ctx context.Context, p service.RepositoryPreset) (service.RepositoryPreset, error)
}

// FoundRepository is a git repository setup found on this machine.
type FoundRepository struct {
	Dir string
	// Repo is its GitHub repository (owner/name), "" when origin is not
	// on GitHub or there is none.
	Repo string
	// Recent says it was used in the last RecentRepository.
	Recent bool
}

// RecentRepository is how lately a repository was used for setup to tick it.
const RecentRepository = 14 * 24 * time.Hour

// repositoryDepth is how many folders below a root a repository is looked
// for: ~/Projects/app and ~/Projects/org/app.
const repositoryDepth = 2

// DefaultRepositoryRoots are the folders setup looks in: ~/Projects, ~/src,
// ~/code and the working directory.
func DefaultRepositoryRoots(home, workDir string) []string {
	roots := []string{filepath.Join(home, "Projects"), filepath.Join(home, "src"), filepath.Join(home, "code")}
	if workDir != "" && !slices.Contains(roots, workDir) {
		roots = append(roots, workDir)
	}
	return roots
}

// FindRepositories finds the git repositories at or under roots, at most
// two folders down, sorted by folder. A repository inside another is not
// one of its own; hidden folders and node_modules are not looked in, and a
// root that does not exist holds none.
func FindRepositories(roots []string, now time.Time) []FoundRepository {
	seen := map[string]bool{}
	var found []FoundRepository
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if seen[dir] {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			seen[dir] = true
			found = append(found, inspectRepository(dir, now))
			return
		}
		if depth == 0 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
				continue
			}
			walk(filepath.Join(dir, e.Name()), depth-1)
		}
	}
	for _, root := range roots {
		walk(filepath.Clean(root), repositoryDepth)
	}
	slices.SortFunc(found, func(a, b FoundRepository) int { return strings.Compare(a.Dir, b.Dir) })
	return found
}

// inspectRepository reads a repository's origin and when it was last used
// (its HEAD, index or config changing), from its files: nothing runs in it.
func inspectRepository(dir string, now time.Time) FoundRepository {
	r := FoundRepository{Dir: dir}
	git := filepath.Join(dir, ".git")
	var last time.Time
	for _, name := range []string{"HEAD", "index", "config", "FETCH_HEAD"} {
		if info, err := os.Stat(filepath.Join(git, name)); err == nil && info.ModTime().After(last) {
			last = info.ModTime()
		}
	}
	r.Recent = !last.IsZero() && now.Sub(last) < RecentRepository
	if origin := originURL(filepath.Join(git, "config")); origin != "" {
		if normalized, ok := config.NormalizeRepository(origin); ok {
			if rest, ok := strings.CutPrefix(normalized, "github.com/"); ok {
				r.Repo = rest
			}
		}
	}
	return r
}

// originURL reads the url of [remote "origin"] from a git config file.
func originURL(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	inOrigin := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if key, value, ok := strings.Cut(line, "="); inOrigin && ok && strings.TrimSpace(key) == "url" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// stepRepositories finds repositories and saves the ticked ones as
// repository presets with their folder. Those already saved are shown and
// kept.
func (f *Flow) stepRepositories(ctx context.Context) error {
	saved, err := f.Repositories.List(ctx)
	if err != nil {
		return fmt.Errorf("listing repositories: %w", err)
	}
	have := map[string]bool{}
	names := map[string]bool{}
	for _, p := range saved {
		names[strings.ToLower(p.Name)] = true
		if p.PrimaryDir != "" {
			have[filepath.Clean(p.PrimaryDir)] = true
		}
		fmt.Fprintf(f.Out, "  · %s (saved)\n", p.Name)
	}
	now := time.Now()
	if f.Now != nil {
		now = f.Now()
	}
	var offered []FoundRepository
	for _, r := range FindRepositories(f.RepositoryRoots, now) {
		if !have[r.Dir] {
			offered = append(offered, r)
		}
	}
	if len(offered) == 0 {
		fmt.Fprintln(f.Out, "  No other git repositories in "+strings.Join(f.RepositoryRoots, ", ")+". Add them in Settings › Agents.")
		return nil
	}
	ticked := make([]bool, len(offered))
	for i, r := range offered {
		ticked[i] = r.Recent
	}
	for {
		for i, r := range offered {
			box := "[ ]"
			if ticked[i] {
				box = "[x]"
			}
			label := r.Dir
			if r.Repo != "" {
				label += "  (" + r.Repo + ")"
			}
			fmt.Fprintf(f.Out, "  %s %d. %s\n", box, i+1, label)
		}
		answer := f.ask("  Numbers to tick or untick, Enter to save the ticked ones, s to skip: ")
		if skip(answer) {
			return nil
		}
		if answer == "" {
			break
		}
		for _, word := range strings.FieldsFunc(answer, func(r rune) bool { return r == ' ' || r == ',' }) {
			n, err := strconv.Atoi(word)
			if err != nil || n < 1 || n > len(offered) {
				fmt.Fprintf(f.Out, "  %q is not a number from 1 to %d.\n", word, len(offered))
				continue
			}
			ticked[n-1] = !ticked[n-1]
		}
	}
	for i, r := range offered {
		if !ticked[i] {
			continue
		}
		name := filepath.Base(r.Dir)
		if names[strings.ToLower(name)] {
			name = filepath.Base(filepath.Dir(r.Dir)) + "-" + name
		}
		p, err := f.Repositories.Save(ctx, service.RepositoryPreset{Name: name, Repo: r.Repo, PrimaryDir: r.Dir})
		if err != nil {
			fmt.Fprintf(f.Out, "  ✗ %s: %v\n", r.Dir, err)
			continue
		}
		names[strings.ToLower(p.Name)] = true
		if f.chosenRepository == "" {
			f.chosenRepository = r.Dir
		}
		fmt.Fprintf(f.Out, "  ✓ %s\n", p.Name)
	}
	return nil
}
