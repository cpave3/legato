package site

import (
	"strings"
	"testing"
)

func TestTheManualIsInTheBinary(t *testing.T) {
	pages := Pages()
	if len(pages) < 40 {
		t.Fatalf("%d pages", len(pages))
	}
	var reeve *Page
	for i := range pages {
		if pages[i].Path == "using/reeve" {
			reeve = &pages[i]
		}
	}
	if reeve == nil || reeve.Title != "The Reeve" {
		t.Fatalf("using/reeve = %+v", reeve)
	}
	text, ok := Read("/legato/using/reeve/")
	if !ok || !strings.Contains(text, "agent of a Legato instance itself") {
		t.Fatalf("read: %v", ok)
	}
	if _, ok := Read("../../go.mod"); ok {
		t.Fatal("read outside the manual")
	}
}

func TestSearchFindsPagesWithEveryWord(t *testing.T) {
	hits := Search("restart legato", 0)
	found := false
	for _, h := range hits {
		if h.Path == "running/updates" && len(h.Lines) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("hits = %+v", hits)
	}
	if hits := Search("zzqqxx-never-written", 0); len(hits) != 0 {
		t.Fatalf("hits for nonsense = %+v", hits)
	}
}

// The Reeve's everyday questions about the API find the API page among the
// first few hits, in the words a person would use.
func TestSearchFindsTheAPIPageForEverydayWords(t *testing.T) {
	for _, query := range []string{"move a card", "stop an agent", "message an agent", "kill agent", "move task column api"} {
		hits := Search(query, 3)
		found := false
		for _, h := range hits {
			if h.Path == "reference/api" {
				found = true
			}
		}
		if !found {
			var paths []string
			for _, h := range hits {
				paths = append(paths, h.Path)
			}
			t.Errorf("search %q: reference/api not in the first 3 hits %v", query, paths)
		}
	}
}

// The lines shown first are those that hold every word.
func TestSearchShowsTheLinesWithEveryWordFirst(t *testing.T) {
	hits := Search("stop an agent", 1)
	if len(hits) == 0 || len(hits[0].Lines) == 0 || !strings.Contains(strings.ToLower(hits[0].Lines[0]), "stop an agent") {
		t.Fatalf("hits = %+v", hits)
	}
}
