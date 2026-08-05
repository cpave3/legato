package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowTriggerConfirmationMigrationExpandsStatusConstraint(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var tableSQL string
	if err := store.DB().Get(&tableSQL, `SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'workflow_trigger_requests'`); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"waiting_confirmation", "starting"} {
		if !strings.Contains(tableSQL, "'"+status+"'") {
			t.Fatalf("workflow trigger constraint does not include %q: %s", status, tableSQL)
		}
	}
}
