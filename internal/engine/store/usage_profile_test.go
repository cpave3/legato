package store

import (
	"context"
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
			db := newStoreAtMigration(t, path, "157_review_loop_approval_dismissed.sql")
			// Model partially created usage tables without replaying unrelated
			// later migrations over an already-upgraded schema.
			schema, err := migrationsFS.ReadFile("migrations/158_usage_profiles.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB().Exec(string(schema)); err != nil {
				t.Fatal(err)
			}
			state := `{"profile":{"name":"conserving"},"pin":{"profile":"conserving"}}`
			change := UsageProfileSwitch{From: "usual", To: "conserving", Since: "2026-11-26T09:00:00Z", Reason: "weekly threshold"}
			// Written as schema 158 had it: switches had no scope yet.
			if _, err := db.DB().Exec(`INSERT INTO active_usage_profile(id,state) VALUES(1,?)`, state); err != nil {
				t.Fatal(err)
			}
			result, err := db.DB().Exec(`INSERT INTO usage_profile_switches(from_profile,to_profile,since,reason) VALUES(?,?,?,?)`, change.From, change.To, change.Since, change.Reason)
			if err != nil {
				t.Fatal(err)
			}
			if change.ID, err = result.LastInsertId(); err != nil {
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

func TestUsageProfileSwitchesKeepTheirScope(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.db")
	db := newStoreAtMigration(t, path, "161_handoff_accounts.sql")
	if _, err := db.DB().Exec(`INSERT INTO usage_profile_switches(from_profile,to_profile,since,reason) VALUES('a','b','2026-11-26T09:00:00Z','old')`); err != nil {
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
	scoped := UsageProfileSwitch{Scope: "work", From: "Work", To: "Codex", Since: "2026-11-26T10:00:00Z", Reason: "limit"}
	if err := reopened.SaveUsageProfile(ctx, `{}`, &scoped); err != nil {
		t.Fatal(err)
	}
	history, err := reopened.UsageProfileSwitches(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0] != scoped || history[1].Scope != "" || history[1].From != "a" {
		t.Fatalf("history = %+v", history)
	}
}
