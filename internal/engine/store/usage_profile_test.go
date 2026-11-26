package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestUsageProfileMigrationPreservesPartialStateAndHistory(t *testing.T) {
	for _, tc := range []struct {
		name            string
		active, history bool
	}{
		{"upgrade", false, false}, {"active only", true, false}, {"history only", false, true}, {"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "usage.db")
			db, err := New(path)
			if err != nil {
				t.Fatal(err)
			}
			state := `{"profile":{"name":"conserving"},"pin":{"profile":"conserving"}}`
			change := UsageProfileSwitch{From: "usual", To: "conserving", Since: "2026-11-26T09:00:00Z", Reason: "weekly threshold"}
			if err := db.SaveUsageProfile(ctx, state, &change); err != nil {
				t.Fatal(err)
			}
			if !tc.active {
				if _, err := db.DB().Exec(`DROP TABLE active_usage_profile`); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.history {
				if _, err := db.DB().Exec(`DROP TABLE usage_profile_switches`); err != nil {
					t.Fatal(err)
				}
			}
			var version int
			if err := db.DB().Get(&version, `SELECT version FROM schema_migrations WHERE name='158_usage_profiles.sql'`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB().Exec(fmt.Sprintf(`DELETE FROM schema_migrations WHERE version >= %d; PRAGMA user_version=%d`, version, version-1)); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := New(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			got, err := reopened.ActiveUsageProfile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.active {
				want = state
			}
			if got != want {
				t.Fatalf("active state=%q, want %q", got, want)
			}
			history, err := reopened.UsageProfileSwitches(ctx, 20)
			if err != nil {
				t.Fatal(err)
			}
			if tc.history {
				if len(history) != 1 || history[0] != change {
					t.Fatalf("history=%+v, want %+v", history, change)
				}
			} else if len(history) != 0 {
				t.Fatalf("new history=%+v", history)
			}
			next := UsageProfileSwitch{From: "conserving", To: "usual", Since: "2026-11-26T10:00:00Z", Reason: "reset"}
			if err := reopened.SaveUsageProfile(ctx, `{"profile":{"name":"usual"}}`, &next); err != nil {
				t.Fatal(err)
			}
			if tc.history && next.ID <= change.ID {
				t.Fatalf("switch IDs lost continuity: %d after %d", next.ID, change.ID)
			}
		})
	}
}
