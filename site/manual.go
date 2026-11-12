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
// case and small words (a, the, to, …) when others are given, best first:
// pages with the query as a phrase on a line, its words in the title or
// description, and lines that hold every word. Each hit has up to five
// lines, those with every word first; at most limit pages.
func Search(query string, limit int) []Hit {
	phrase := strings.Join(strings.Fields(strings.ToLower(query)), " ")
	words := searchWords(phrase)
	if len(words) == 0 {
		return nil
	}
	type scored struct {
		hit   Hit
		score int
	}
	var found []scored
	for _, page := range Pages() {
		text, _ := Read(page.Path)
		lower := strings.ToLower(text)
		if !containsAll(lower, words) {
			continue
		}
		var phraseLines, allLines, someLines []string
		for _, line := range strings.Split(text, "\n") {
			l := strings.ToLower(line)
			switch {
			case strings.Contains(l, phrase):
				phraseLines = append(phraseLines, strings.TrimSpace(line))
			case containsAll(l, words):
				allLines = append(allLines, strings.TrimSpace(line))
			case containsAny(l, words):
				someLines = append(someLines, strings.TrimSpace(line))
			}
		}
		meta := 0
		head := strings.ToLower(page.Title + " " + page.Description)
		for _, w := range words {
			if strings.Contains(head, w) {
				meta++
			}
		}
		score := 4*min(len(phraseLines), 5) + 2*min(len(phraseLines)+len(allLines), 5) + 3*meta
		lines := append(append(phraseLines, allLines...), someLines...)
		if len(lines) > 5 {
			lines = lines[:5]
		}
		found = append(found, scored{Hit{Page: page, Lines: lines}, score})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	var hits []Hit
	for _, f := range found {
		hits = append(hits, f.hit)
		if limit > 0 && len(hits) == limit {
			break
		}
	}
	return hits
}

// smallWords are left out of a search that has other words: nearly every
// page holds them.
var smallWords = map[string]bool{"a": true, "an": true, "the": true, "to": true, "of": true, "how": true, "do": true, "i": true, "is": true, "in": true, "on": true, "for": true, "and": true, "or": true, "my": true, "can": true, "with": true}

func searchWords(phrase string) []string {
	all := strings.Fields(phrase)
	var words []string
	for _, w := range all {
		if !smallWords[w] {
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return all
	}
	return words
}

func containsAll(s string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
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
