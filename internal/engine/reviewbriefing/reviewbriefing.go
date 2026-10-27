// Package reviewbriefing finds the review briefing a branch carries: the
// HTML pages under .review/ that an agent writes for the pull request, as
// the PR Review Briefings extension reads them on GitHub. Legato reads them
// from the checkout, so a briefing can be read before the PR exists.
//
// Only files the branch itself adds or changes count, committed or not, so
// a folder another PR left behind on the base branch is never shown.
package reviewbriefing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Dir is the folder briefings live in, at the root of the repository.
const Dir = ".review/"

// ErrNoCheckout is returned for a directory that is not inside a Git
// repository.
var ErrNoCheckout = errors.New("not a Git checkout")

// Page is one HTML page of a briefing.
type Page struct {
	// Path is the page's path from the repository root.
	Path string `json:"path"`
	// Folder is the briefing folder below .review/ ("feat-login"), empty for
	// a page directly in .review/.
	Folder string `json:"folder"`
	// Label is what the page is called in a tab: "Overview" for index.html.
	Label string `json:"label"`
	// Version changes whenever the page's content does, so a rebuilt page
	// is read again.
	Version string `json:"version"`
}

// Index is what a checkout's branch carries in .review/.
type Index struct {
	Root   string `json:"root"`
	Branch string `json:"branch"`
	// Base is the branch the briefing is measured against.
	Base string `json:"base"`
	// Files are every .review/ path the branch adds or changes.
	Files []string `json:"files"`
	Pages []Page   `json:"pages"`
}

// Find reads the briefing of the branch checked out in dir.
func Find(ctx context.Context, dir string) (*Index, error) {
	root, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNoCheckout
	}
	root = strings.TrimSpace(root)
	branch, err := run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, err
	}
	base, err := baseBranch(ctx, root)
	if err != nil {
		return nil, err
	}
	mergeBase, err := run(ctx, root, "merge-base", base, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("finding where the branch left %s: %w", base, err)
	}
	changed, err := run(ctx, root, "diff", "--no-ext-diff", "--no-renames", "--name-only", "-z", "--diff-filter=d", strings.TrimSpace(mergeBase), "--", Dir)
	if err != nil {
		return nil, err
	}
	untracked, err := run(ctx, root, "ls-files", "--others", "--exclude-standard", "-z", "--", Dir)
	if err != nil {
		return nil, err
	}
	files := append(splitNul(changed), splitNul(untracked)...)
	sort.Strings(files)
	index := &Index{Root: root, Branch: strings.TrimSpace(branch), Base: base, Files: files}
	index.Pages = pages(files, index.Branch)
	for i := range index.Pages {
		if content, err := readFile(root, index.Pages[i].Path); err == nil {
			sum := sha256.Sum256(content)
			index.Pages[i].Version = hex.EncodeToString(sum[:8])
		}
	}
	return index, nil
}

// pages are the HTML files among files, grouped by briefing folder: the
// branch's own folder first, then the others by name. In a folder its
// index.html comes first, then the rest by path.
func pages(files []string, branch string) []Page {
	var found []Page
	for _, path := range files {
		if !isPage(path) {
			continue
		}
		folder := folderOf(path)
		relative := strings.TrimPrefix(path, Dir)
		if folder != "" {
			relative = strings.TrimPrefix(relative, folder+"/")
		}
		found = append(found, Page{Path: path, Folder: folder, Label: label(relative)})
	}
	own := BranchSlug(branch)
	rank := func(page Page) (int, int) {
		folderRank := 1
		if page.Folder == own {
			folderRank = 0
		}
		pageRank := 1
		if page.Path == folderPath(page.Folder)+"index.html" {
			pageRank = 0
		}
		return folderRank, pageRank
	}
	sort.SliceStable(found, func(i, j int) bool {
		fi, pi := rank(found[i])
		fj, pj := rank(found[j])
		if fi != fj {
			return fi < fj
		}
		if found[i].Folder != found[j].Folder {
			return found[i].Folder < found[j].Folder
		}
		if pi != pj {
			return pi < pj
		}
		return found[i].Path < found[j].Path
	})
	return found
}

// BranchSlug is the folder a branch's briefing goes in: "feat/login" is
// "feat-login". The extension and the briefing skill use the same rule.
func BranchSlug(branch string) string {
	return strings.Trim(slugUnsafe.ReplaceAllString(branch, "-"), "-")
}

var slugUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func isPage(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm")
}

// folderOf is the briefing folder a file belongs to, empty for a file
// directly in .review/ (the older layout, without folders per branch).
func folderOf(path string) string {
	rest := strings.TrimPrefix(path, Dir)
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		return rest[:slash]
	}
	return ""
}

func folderPath(folder string) string {
	if folder == "" {
		return Dir
	}
	return Dir + folder + "/"
}

// label names a page by its path inside its folder: "index.html" is
// "Overview", "deep/data-flow.html" is "Deep/Data flow".
func label(relative string) string {
	name := relative[:strings.LastIndexByte(relative, '.')]
	if name == "index" {
		return "Overview"
	}
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)
	parts := strings.Split(name, "/")
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "/")
}

// baseBranch is the branch a pull request from this checkout would most
// likely target: the remote's default branch, else main, else master.
func baseBranch(ctx context.Context, root string) (string, error) {
	if ref, err := run(ctx, root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimSpace(ref), nil
	}
	for _, name := range []string{"main", "master"} {
		if _, err := run(ctx, root, "rev-parse", "--verify", "--quiet", name+"^{commit}"); err == nil {
			return name, nil
		}
	}
	return "", errors.New("no base branch: the checkout has no origin/HEAD, main or master")
}

func splitNul(out string) []string {
	var parts []string
	for _, part := range strings.Split(out, "\x00") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}
