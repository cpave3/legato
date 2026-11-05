package main

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

func TestTheLaunchPromptSpeaksOfStacksOnlyWhenSTCanImport(t *testing.T) {
	cfg := &config.Config{}
	if got := credentialPromptGuidance(cfg, true, service.SessionBranches{}); !strings.Contains(got, "st sync") {
		t.Errorf("no stack guidance in the launch prompt without credentials:\n%s", got)
	}
	if got := credentialPromptGuidance(cfg, false, service.SessionBranches{}); got != "" {
		t.Errorf("launch prompt without credentials or stacks = %q", got)
	}
}
