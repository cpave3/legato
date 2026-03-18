package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// NeedsSetup returns true if no config file exists at the given path.
func NeedsSetup(configPath string) bool {
	_, err := os.Stat(configPath)
	return os.IsNotExist(err)
}

// ValidateBaseURL checks that the URL is valid HTTPS.
func ValidateBaseURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL cannot be empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("URL must use HTTPS (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL must have a host")
	}
	return nil
}

// ValidateCredentials tests the API credentials by calling the /myself endpoint.
func ValidateCredentials(ctx context.Context, baseURL, email, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/rest/api/3/myself", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(email, token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed: invalid email or API token")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Project represents a Jira project for selection.
type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// FetchProjects retrieves available projects from the API.
func FetchProjects(ctx context.Context, baseURL, email, token string) ([]Project, error) {
	body, err := apiGet(ctx, baseURL, email, token, "/rest/api/3/project")
	if err != nil {
		return nil, err
	}
	var projects []Project
	if err := json.Unmarshal(body, &projects); err != nil {
		return nil, fmt.Errorf("parsing projects: %w", err)
	}
	return projects, nil
}

type issueTypeStatuses struct {
	Name     string       `json:"name"`
	Statuses []statusItem `json:"statuses"`
}

type statusItem struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// FetchStatuses retrieves the union of all statuses across issue types for a project.
func FetchStatuses(ctx context.Context, baseURL, email, token, projectKey string) ([]string, error) {
	body, err := apiGet(ctx, baseURL, email, token,
		"/rest/api/3/project/"+url.PathEscape(projectKey)+"/statuses")
	if err != nil {
		return nil, err
	}

	var issueTypes []issueTypeStatuses
	if err := json.Unmarshal(body, &issueTypes); err != nil {
		return nil, fmt.Errorf("parsing statuses: %w", err)
	}

	seen := map[string]bool{}
	var result []string
	for _, it := range issueTypes {
		for _, s := range it.Statuses {
			if !seen[s.Name] {
				seen[s.Name] = true
				result = append(result, s.Name)
			}
		}
	}
	return result, nil
}

// TransitionInfo represents a discovered workflow transition.
type TransitionInfo struct {
	ID           string
	Name         string
	TargetStatus string
}

// FetchTransitions retrieves available transitions for an issue.
func FetchTransitions(ctx context.Context, baseURL, email, token, issueKey string) ([]TransitionInfo, error) {
	body, err := apiGet(ctx, baseURL, email, token,
		"/rest/api/3/issue/"+url.PathEscape(issueKey)+"/transitions")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			To   struct {
				Name string `json:"name"`
			} `json:"to"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing transitions: %w", err)
	}

	result := make([]TransitionInfo, len(resp.Transitions))
	for i, tr := range resp.Transitions {
		result[i] = TransitionInfo{
			ID:           tr.ID,
			Name:         tr.Name,
			TargetStatus: tr.To.Name,
		}
	}
	return result, nil
}

// ColumnMappingConfig represents a column mapping for config file output.
type ColumnMappingConfig struct {
	Name     string
	Statuses []string
}

// AutoMapColumns maps discovered statuses to default columns using name heuristics.
func AutoMapColumns(statuses []string) []ColumnMappingConfig {
	backlog := []string{}
	doing := []string{}
	review := []string{}
	done := []string{}
	unmapped := []string{}

	for _, s := range statuses {
		lower := strings.ToLower(s)
		switch {
		case lower == "to do" || lower == "open" || lower == "backlog" ||
			lower == "new" || lower == "ready for dev" || lower == "selected for development":
			backlog = append(backlog, s)
		case lower == "in progress" || lower == "in development" || lower == "doing" ||
			lower == "active" || lower == "started":
			doing = append(doing, s)
		case lower == "in review" || lower == "review" || lower == "code review" ||
			lower == "in qa" || lower == "testing":
			review = append(review, s)
		case lower == "done" || lower == "closed" || lower == "resolved" ||
			lower == "complete" || lower == "completed":
			done = append(done, s)
		default:
			unmapped = append(unmapped, s)
		}
	}

	var result []ColumnMappingConfig
	if len(backlog) > 0 {
		result = append(result, ColumnMappingConfig{Name: "Backlog", Statuses: backlog})
	}
	if len(doing) > 0 {
		result = append(result, ColumnMappingConfig{Name: "Doing", Statuses: doing})
	}
	if len(review) > 0 {
		result = append(result, ColumnMappingConfig{Name: "Review", Statuses: review})
	}
	if len(done) > 0 {
		result = append(result, ColumnMappingConfig{Name: "Done", Statuses: done})
	}
	if len(unmapped) > 0 {
		result = append(result, ColumnMappingConfig{Name: "Unmapped", Statuses: unmapped})
	}
	return result
}

// WizardConfig holds all values gathered during the setup wizard.
type WizardConfig struct {
	BaseURL     string
	Email       string
	ProjectKeys []string
	Columns     []ColumnMappingConfig
}

// WriteConfig writes the wizard configuration to a YAML file.
// The API token is stored as an env var reference.
func WriteConfig(path string, cfg WizardConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	type columnYAML struct {
		Name           string   `yaml:"name"`
		RemoteStatuses []string `yaml:"remote_statuses"`
	}

	type configYAML struct {
		Jira struct {
			BaseURL     string   `yaml:"base_url"`
			Email       string   `yaml:"email"`
			APIToken    string   `yaml:"api_token"`
			ProjectKeys []string `yaml:"project_keys"`
		} `yaml:"jira"`
		Board struct {
			Columns []columnYAML `yaml:"columns"`
		} `yaml:"board"`
	}

	var out configYAML
	out.Jira.BaseURL = cfg.BaseURL
	out.Jira.Email = cfg.Email
	out.Jira.APIToken = "${LEGATO_JIRA_TOKEN}"
	out.Jira.ProjectKeys = cfg.ProjectKeys

	for _, col := range cfg.Columns {
		out.Board.Columns = append(out.Board.Columns, columnYAML{
			Name:           col.Name,
			RemoteStatuses: col.Statuses,
		})
	}

	data, err := yaml.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

func apiGet(ctx context.Context, baseURL, email, token, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(baseURL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(email, token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
