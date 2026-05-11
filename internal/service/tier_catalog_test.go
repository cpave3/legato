package service

import (
	"strings"
	"testing"
)

func TestFormatTierCatalogEmptyReturnsEmptyString(t *testing.T) {
	if got := formatTierCatalog(nil); got != "" {
		t.Errorf("nil catalog should return empty, got %q", got)
	}
	if got := formatTierCatalog(map[string]map[string]string{}); got != "" {
		t.Errorf("empty catalog should return empty, got %q", got)
	}
}

func TestFormatTierCatalogSkipsAdaptersWithNoTiers(t *testing.T) {
	got := formatTierCatalog(map[string]map[string]string{
		"claude-code": {},
		"chimera":     nil,
	})
	if got != "" {
		t.Errorf("adapters with no tiers should produce empty catalog, got %q", got)
	}
}

func TestFormatTierCatalogIncludesAdapterAndTierNames(t *testing.T) {
	got := formatTierCatalog(map[string]map[string]string{
		"claude-code": {
			"small": "fast/cheap; trivial edits",
			"large": "big reasoning; multi-file refactors",
		},
		"chimera": {
			"quick": "sandboxed runs",
		},
	})
	for _, want := range []string{
		"## Available tiers",
		"### claude-code",
		"- small — fast/cheap; trivial edits",
		"- large — big reasoning; multi-file refactors",
		"### chimera",
		"- quick — sandboxed runs",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("catalog missing %q\n---\n%s", want, got)
		}
	}
}

func TestFormatTierCatalogStableOrder(t *testing.T) {
	// Adapters and tiers should sort alphabetically so map iteration order
	// doesn't make the brief vary across runs.
	got := formatTierCatalog(map[string]map[string]string{
		"zebra": {"z": "z desc", "a": "a desc"},
		"alpha": {"q": "q desc", "b": "b desc"},
	})
	alphaIdx := strings.Index(got, "### alpha")
	zebraIdx := strings.Index(got, "### zebra")
	if alphaIdx == -1 || zebraIdx == -1 || alphaIdx >= zebraIdx {
		t.Errorf("expected alpha before zebra, got:\n%s", got)
	}
	bIdx := strings.Index(got, "- b ")
	qIdx := strings.Index(got, "- q ")
	if bIdx == -1 || qIdx == -1 || bIdx >= qIdx {
		t.Errorf("expected tier 'b' before tier 'q' under alpha, got:\n%s", got)
	}
}

func TestFormatTierCatalogTolerantOfMissingDescription(t *testing.T) {
	got := formatTierCatalog(map[string]map[string]string{
		"claude-code": {
			"small": "",
		},
	})
	if !strings.Contains(got, "- small\n") {
		t.Errorf("missing-description tier should still render the name, got:\n%s", got)
	}
	if strings.Contains(got, "small —") {
		t.Errorf("should not emit em-dash without description, got:\n%s", got)
	}
}
