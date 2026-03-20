package board

import (
	"strings"
	"testing"
)

func TestColumnHeaderContainsName(t *testing.T) {
	cards := []CardData{{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}}
	out := RenderColumn("Doing", cards, 20, true, 0)
	if !strings.Contains(strings.ToUpper(out), "DOING") {
		t.Errorf("column header should contain uppercase name, got: %q", out)
	}
}

func TestColumnHeaderContainsCount(t *testing.T) {
	cards := []CardData{
		{Key: "REX-1", Title: "A", Priority: "High", IssueType: "Bug"},
		{Key: "REX-2", Title: "B", Priority: "Low", IssueType: "Story"},
	}
	out := RenderColumn("Ready", cards, 20, false, -1)
	if !strings.Contains(out, "2") {
		t.Errorf("column header should contain card count 2, got: %q", out)
	}
}

func TestColumnEmptyRendersHeader(t *testing.T) {
	out := RenderColumn("Backlog", nil, 20, false, -1)
	if !strings.Contains(strings.ToUpper(out), "BACKLOG") {
		t.Errorf("empty column should still show header, got: %q", out)
	}
	if !strings.Contains(out, "0") {
		t.Errorf("empty column should show count 0, got: %q", out)
	}
}

func TestColumnRendersCards(t *testing.T) {
	cards := []CardData{
		{Key: "REX-1", Title: "First", Priority: "High", IssueType: "Bug"},
		{Key: "REX-2", Title: "Second", Priority: "Low", IssueType: "Story"},
	}
	out := RenderColumn("Doing", cards, 30, true, 0)
	if !strings.Contains(out, "REX-1") {
		t.Errorf("column should contain first card key, got: %q", out)
	}
	if !strings.Contains(out, "REX-2") {
		t.Errorf("column should contain second card key, got: %q", out)
	}
}

func TestColumnActiveVsInactive(t *testing.T) {
	cards := []CardData{{Key: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug"}}
	// Both should render without error
	active := RenderColumn("Doing", cards, 30, true, 0)
	inactive := RenderColumn("Doing", cards, 30, false, -1)
	if active == "" || inactive == "" {
		t.Error("both active and inactive columns should render non-empty")
	}
}
