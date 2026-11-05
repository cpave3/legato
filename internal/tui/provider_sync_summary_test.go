package tui

import (
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestProviderSyncSummaryNamesWorkers(t *testing.T) {
	msg, failed := providerSyncSummary(service.ProviderSyncResult{
		"jira":    {Status: service.ProviderSyncCompleted},
		"github":  {Status: service.ProviderSyncCompleted},
		"workers": {Status: service.ProviderSyncCompleted},
	})
	if failed || msg != "Jira + GitHub + Workers sync completed" {
		t.Fatalf("got %q failed=%v", msg, failed)
	}

	msg, failed = providerSyncSummary(service.ProviderSyncResult{
		"workers": {Status: service.ProviderSyncFailed, Error: "unreachable"},
	})
	if !failed || msg != "provider sync failed; Workers: unreachable" {
		t.Fatalf("got %q failed=%v", msg, failed)
	}
}
