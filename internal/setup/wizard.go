package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cpave3/legato/config"
)

// ColumnSeeder is the subset of store operations the wizard needs.
type ColumnSeeder interface {
	ListColumnMappings(ctx context.Context) ([]ColumnMappingRow, error)
	CreateColumnMapping(ctx context.Context, m ColumnMappingRow) error
	DeleteColumnMapping(ctx context.Context, id int) error
}

// ColumnMappingRow represents a column mapping for the wizard.
// Mirrors store.ColumnMapping but avoids importing engine/store from setup.
type ColumnMappingRow struct {
	ID               int
	ColumnName       string
	RemoteStatuses   string
	RemoteTransition string
	SortOrder        int
}

// JiraSetup handles Jira API interactions for the wizard.
type JiraSetup interface {
	ValidateCredentials(ctx context.Context, baseURL, email, token string) error
	FetchProjects(ctx context.Context, baseURL, email, token string) ([]Project, error)
	FetchStatuses(ctx context.Context, baseURL, email, token, projectKey string) ([]string, error)
}

// WizardOption configures optional wizard behavior.
type WizardOption func(*wizardOpts)

type wizardOpts struct {
	configPath string
}

// WithConfigPath sets the config file path for Jira setup.
func WithConfigPath(path string) WizardOption {
	return func(o *wizardOpts) {
		o.configPath = path
	}
}

// RunWizard runs the first-run setup wizard.
// It seeds default columns and optionally configures Jira.
func RunWizard(ctx context.Context, out io.Writer, in io.Reader, store ColumnSeeder, columns []config.ColumnConfig, jira JiraSetup, opts ...WizardOption) error {
	o := &wizardOpts{
		configPath: config.ResolveConfigPath(),
	}
	for _, opt := range opts {
		opt(o)
	}

	scanner := bufio.NewScanner(in)
	readLine := func() string {
		scanner.Scan()
		return strings.TrimSpace(scanner.Text())
	}

	fmt.Fprintln(out, "Welcome to Legato!")
	fmt.Fprintln(out)

	// Seed default columns
	fmt.Fprintln(out, "Setting up default board columns...")
	if err := seedColumns(ctx, store, columns); err != nil {
		return err
	}

	names := make([]string, len(columns))
	for i, col := range columns {
		names[i] = col.Name
	}
	fmt.Fprintf(out, "  ✓ %s\n", strings.Join(names, ", "))
	fmt.Fprintln(out)

	// Ask about Jira
	fmt.Fprint(out, "Configure Jira integration? (y/N): ")
	answer := strings.ToLower(readLine())

	if answer != "y" && answer != "yes" {
		fmt.Fprintln(out, "Running in local-only mode. You can configure Jira later in config.yaml.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Launching Legato...")
		return nil
	}

	if jira == nil {
		return fmt.Errorf("Jira setup requested but no JiraSetup provided")
	}

	// Collect credentials
	fmt.Fprint(out, "  Jira base URL (e.g. https://yourteam.atlassian.net): ")
	baseURL := readLine()

	fmt.Fprint(out, "  Email: ")
	email := readLine()

	fmt.Fprint(out, "  API token: ")
	token := readLine()

	// Validate
	fmt.Fprint(out, "  Validating... ")
	if err := jira.ValidateCredentials(ctx, baseURL, email, token); err != nil {
		fmt.Fprintln(out, "✗")
		return fmt.Errorf("credential validation failed: %w", err)
	}
	fmt.Fprintln(out, "✓")
	fmt.Fprintln(out)

	// Fetch and display projects
	projects, err := jira.FetchProjects(ctx, baseURL, email, token)
	if err != nil {
		return fmt.Errorf("fetching projects: %w", err)
	}

	fmt.Fprintln(out, "  Available projects:")
	for i, p := range projects {
		fmt.Fprintf(out, "    %d. %s - %s\n", i+1, p.Key, p.Name)
	}
	fmt.Fprint(out, "  Select project (number): ")
	var projectIdx int
	if _, err := fmt.Sscanf(readLine(), "%d", &projectIdx); err != nil || projectIdx < 1 || projectIdx > len(projects) {
		return fmt.Errorf("invalid project selection")
	}
	selectedProject := projects[projectIdx-1]
	fmt.Fprintln(out)

	// Fetch statuses and auto-map
	fmt.Fprint(out, "  Discovering statuses... ")
	statuses, err := jira.FetchStatuses(ctx, baseURL, email, token, selectedProject.Key)
	if err != nil {
		fmt.Fprintln(out, "✗")
		return fmt.Errorf("fetching statuses: %w", err)
	}
	fmt.Fprintln(out, "✓")

	mapped := AutoMapColumns(statuses)
	fmt.Fprintln(out, "  Auto-mapped columns from Jira statuses.")
	fmt.Fprintln(out)

	// Replace default columns with Jira-mapped ones
	existing, err := store.ListColumnMappings(ctx)
	if err != nil {
		return fmt.Errorf("listing columns: %w", err)
	}
	for _, m := range existing {
		if err := store.DeleteColumnMapping(ctx, m.ID); err != nil {
			return fmt.Errorf("deleting column %q: %w", m.ColumnName, err)
		}
	}
	mappedConfigs := make([]config.ColumnConfig, len(mapped))
	for i, col := range mapped {
		mappedConfigs[i] = config.ColumnConfig{
			Name:           col.Name,
			RemoteStatuses: col.Statuses,
		}
	}
	if err := seedColumns(ctx, store, mappedConfigs); err != nil {
		return err
	}

	// Write config file
	wizCfg := WizardConfig{
		BaseURL:     baseURL,
		Email:       email,
		ProjectKeys: []string{selectedProject.Key},
		Columns:     mapped,
	}
	if err := WriteConfig(o.configPath, wizCfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Fprintf(out, "  Config written to %s\n", o.configPath)
	fmt.Fprintln(out, "  Set LEGATO_JIRA_TOKEN env var with your API token.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Launching Legato...")
	return nil
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
