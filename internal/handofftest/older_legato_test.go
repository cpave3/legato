package handofftest

import (
	"context"
	"fmt"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// laterColumns are the columns today's store writes on a run's start that
// migrations after the conversion added: an older Legato never had them.
var laterColumns = []struct{ table, column, kind string }{
	{"workflow_runs", "parent_attempt", "INTEGER"}, // migration 154 (LEG-489)
}

// writeAsOlderLegato runs write, which uses today's store, against a
// database opened at the schema before the conversion. The columns later
// migrations add are there for the write and dropped after it, so the
// database is as an older Legato left it and those migrations run on it as
// they would on an upgrade.
func writeAsOlderLegato(t testing.TB, db *store.Store, write func()) {
	t.Helper()
	ctx := context.Background()
	for _, c := range laterColumns {
		if _, err := db.DB().ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %q ADD COLUMN %q %s`, c.table, c.column, c.kind)); err != nil {
			t.Fatal(err)
		}
	}
	write()
	for _, c := range laterColumns {
		if _, err := db.DB().ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %q DROP COLUMN %q`, c.table, c.column)); err != nil {
			t.Fatal(err)
		}
	}
}
