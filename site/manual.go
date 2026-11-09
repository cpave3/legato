// Package site embeds Legato's user manual, the pages of the documentation
// site in src/content/docs, so the binary can give them to the Reeve (its
// docs tool). The internals docs (docs/ai/) are not given.
package site

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed src/content/docs
var content embed.FS

const root = "src/content/docs"

// Page is one page of the manual. Path is its address on the site without
// the extension, as using/reeve.
type Page struct {
	Path        string `json:"path"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// Hit is a page that matched a search, with the lines that matched.
type Hit struct {
	Page
	Lines []string `json:"lines"`
}

// Pages lists every page of the manual, by path.
func Pages() []Page {
	var pages []Page
	_ = fs.WalkDir(content, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || (path.Ext(p) != ".md" && path.Ext(p) != ".mdx") {
			return nil
		}
		data, err := content.ReadFile(p)
		if err != nil {
			return nil
		}
		title, description := frontmatter(string(data))
		pages = append(pages, Page{Path: pagePath(p), Title: title, Description: description})
		return nil
	})
	sort.Slice(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })
	return pages
}

func pagePath(file string) string {
	p := strings.TrimPrefix(file, root+"/")
	return strings.TrimSuffix(strings.TrimSuffix(p, ".mdx"), ".md")
}

// Read returns a page's text by its path (using/reeve, or with .md); ok is
// false when there is none.
func Read(p string) (string, bool) {
	p = strings.Trim(strings.TrimPrefix(strings.TrimSpace(p), "/legato/"), "/")
	p = strings.TrimSuffix(strings.TrimSuffix(p, ".mdx"), ".md")
	if p == "" || strings.Contains(p, "..") {
		return "", false
	}
	for _, ext := range []string{".md", ".mdx"} {
		if data, err := content.ReadFile(root + "/" + p + ext); err == nil {
			return string(data), true
		}
	}
	return "", false
}

// Search finds the pages whose text holds every word of query, ignoring
// case, with up to five matching lines each; at most limit pages.
func Search(query string, limit int) []Hit {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	var hits []Hit
	for _, page := range Pages() {
		text, _ := Read(page.Path)
		lower := strings.ToLower(text)
		all := true
		for _, w := range words {
			if !strings.Contains(lower, w) {
				all = false
				break
			}
		}
		if !all {
			continue
		}
		hit := Hit{Page: page}
		for _, line := range strings.Split(text, "\n") {
			l := strings.ToLower(line)
			for _, w := range words {
				if strings.Contains(l, w) {
					hit.Lines = append(hit.Lines, strings.TrimSpace(line))
					break
				}
			}
			if len(hit.Lines) == 5 {
				break
			}
		}
		hits = append(hits, hit)
		if limit > 0 && len(hits) == limit {
			break
		}
	}
	return hits
}

// frontmatter reads a page's title and description.
func frontmatter(text string) (title, description string) {
	if !strings.HasPrefix(text, "---") {
		return "", ""
	}
	end := strings.Index(text[3:], "\n---")
	if end < 0 {
		return "", ""
	}
	for _, line := range strings.Split(text[3:3+end], "\n") {
		if v, ok := strings.CutPrefix(line, "title:"); ok {
			title = strings.Trim(strings.TrimSpace(v), `"'`)
		}
		if v, ok := strings.CutPrefix(line, "description:"); ok {
			description = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return title, description
}
