package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpave3/legato/config"
)

// ColumnSeeder is the subset of store operations setup needs.
type ColumnSeeder interface {
	ListColumnMappings(ctx context.Context) ([]ColumnMappingRow, error)
	CreateColumnMapping(ctx context.Context, m ColumnMappingRow) error
	DeleteColumnMapping(ctx context.Context, id int) error
}

// ColumnMappingRow represents a column mapping for setup.
// Mirrors store.ColumnMapping but avoids importing engine/store from setup.
type ColumnMappingRow struct {
	ID               int
	ColumnName       string
	RemoteStatuses   string
	RemoteTransition string
	SortOrder        int
}

// JiraSetup handles Jira API interactions for setup.
type JiraSetup interface {
	ValidateCredentials(ctx context.Context, baseURL, email, token string) error
	FetchProjects(ctx context.Context, baseURL, email, token string) ([]Project, error)
	FetchStatuses(ctx context.Context, baseURL, email, token, projectKey string) ([]string, error)
}

// SettingsSink keeps what setup is told in Legato's database: settings
// by key (JSON values), and the Jira token as a secret, by name.
type SettingsSink interface {
	SetSetting(ctx context.Context, key string, raw []byte) error
	StoreSecret(ctx context.Context, name, value string) (ref string, err error)
}

func seedColumns(ctx context.Context, store ColumnSeeder, columns []config.ColumnConfig) error {
	for i, col := range columns {
		statuses := strings.Join(col.RemoteStatuses, ",")
		err := store.CreateColumnMapping(ctx, ColumnMappingRow{
			ColumnName:     col.Name,
			RemoteStatuses: statuses,
			SortOrder:      i,
		})
		if err != nil {
			return fmt.Errorf("creating column %q: %w", col.Name, err)
		}
	}
	return nil
}

// jiraTokenSecret is the name setup keeps the Jira token under.
const jiraTokenSecret = "jira-api-token"

// saveJira keeps Jira's settings in the database: the token in the secret
// store, the setting naming it.
func saveJira(ctx context.Context, sink SettingsSink, baseURL, email, token, project string) error {
	ref, err := sink.StoreSecret(ctx, jiraTokenSecret, token)
	if err != nil {
		return err
	}
	for key, value := range map[string]any{
		"jira.base_url": baseURL, "jira.email": email, "jira.project_keys": []string{project}, "jira.api_token": ref,
	} {
		raw, _ := json.Marshal(value)
		if err := sink.SetSetting(ctx, key, raw); err != nil {
			return err
		}
	}
	return nil
}

// SeedColumnsIfEmpty gives a board with no columns the configured ones, as
// the first run's setup does, without asking anything: for `legato serve`,
// which has nobody at a terminal to ask (a headless worker refuses every
// handoff while its board has no columns). It says whether it seeded.
func SeedColumnsIfEmpty(ctx context.Context, store ColumnSeeder, columns []config.ColumnConfig) (bool, error) {
	existing, err := store.ListColumnMappings(ctx)
	if err != nil {
		return false, fmt.Errorf("listing columns: %w", err)
	}
	if len(existing) > 0 {
		return false, nil
	}
	return true, seedColumns(ctx, store, columns)
}
