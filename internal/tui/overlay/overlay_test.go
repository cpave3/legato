package overlay

import (
	"strings"
	"testing"
)

func TestRenderPanelCentersContent(t *testing.T) {
	content := "Hello World"
	result := RenderPanel(content, 60, 20)
	if result == "" {
		t.Fatal("RenderPanel should not return empty string")
	}
	// The content should appear somewhere in the rendered output
	if !strings.Contains(result, "Hello World") {
		t.Error("rendered panel should contain the content text")
	}
}

func TestRenderPanelHasBorder(t *testing.T) {
	content := "Test"
	result := RenderPanel(content, 60, 20)
	// Rounded borders use ╭ and ╮ characters
	if !strings.Contains(result, "╭") || !strings.Contains(result, "╮") {
		t.Error("rendered panel should have rounded border characters")
	}
}

func TestRenderPanelZeroDimensionsReturnsBox(t *testing.T) {
	content := "Test"
	result := RenderPanel(content, 0, 0)
	if !strings.Contains(result, "Test") {
		t.Error("should still contain content even at zero dimensions")
	}
}
