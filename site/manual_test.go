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
