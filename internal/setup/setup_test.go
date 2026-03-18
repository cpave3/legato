package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// Task 6.1: First-run detection
func TestNeedsSetup_NoConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if !NeedsSetup(path) {
		t.Error("should need setup when config file doesn't exist")
	}
}

func TestNeedsSetup_HasConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("jira:\n  base_url: https://test.atlassian.net"), 0644)
	if NeedsSetup(path) {
		t.Error("should not need setup when config file exists")
	}
}

// Task 6.2: URL validation
func TestValidateBaseURL_ValidHTTPS(t *testing.T) {
	err := ValidateBaseURL("https://company.atlassian.net")
	if err != nil {
		t.Errorf("valid HTTPS URL rejected: %v", err)
	}
}

func TestValidateBaseURL_RejectsHTTP(t *testing.T) {
	err := ValidateBaseURL("http://company.atlassian.net")
	if err == nil {
		t.Error("should reject non-HTTPS URL")
	}
}

func TestValidateBaseURL_RejectsInvalid(t *testing.T) {
	err := ValidateBaseURL("not-a-url")
	if err == nil {
		t.Error("should reject invalid URL")
	}
}

func TestValidateBaseURL_RejectsEmpty(t *testing.T) {
	err := ValidateBaseURL("")
	if err == nil {
		t.Error("should reject empty URL")
	}
}

// Task 6.3: API token validation
func TestValidateCredentials_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"self":"https://test.atlassian.net/rest/api/3/myself"}`))
	}))
	defer srv.Close()

	err := ValidateCredentials(context.Background(), srv.URL, "test@example.com", "valid-token")
	if err != nil {
		t.Errorf("valid credentials rejected: %v", err)
	}
}

func TestValidateCredentials_InvalidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := ValidateCredentials(context.Background(), srv.URL, "test@example.com", "bad-token")
	if err == nil {
		t.Error("should reject invalid credentials")
	}
}

// Task 6.4: Project discovery (tested via mock server)
func TestFetchProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"key":"PROJ","name":"Project One"},
			{"key":"DEV","name":"Development"}
		]`))
	}))
	defer srv.Close()

	projects, err := FetchProjects(context.Background(), srv.URL, "a@b.com", "tok")
	if err != nil {
		t.Fatalf("fetch projects: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("got %d projects, want 2", len(projects))
	}
	if projects[0].Key != "PROJ" {
		t.Errorf("project[0] key = %q", projects[0].Key)
	}
}

// Task 6.5: Workflow discovery
func TestFetchStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"name":"Story","statuses":[{"name":"To Do","id":"1"},{"name":"In Progress","id":"3"},{"name":"Done","id":"4"}]},
			{"name":"Bug","statuses":[{"name":"Open","id":"1"},{"name":"In Progress","id":"3"},{"name":"Closed","id":"5"}]}
		]`))
	}))
	defer srv.Close()

	statuses, err := FetchStatuses(context.Background(), srv.URL, "a@b.com", "tok", "PROJ")
	if err != nil {
		t.Fatalf("fetch statuses: %v", err)
	}

	// Should be union: To Do, In Progress, Done, Open, Closed
	if len(statuses) != 5 {
		t.Errorf("got %d statuses, want 5: %v", len(statuses), statuses)
	}
}

// Task 6.6: Column mapping heuristics
func TestAutoMapColumns(t *testing.T) {
	statuses := []string{"To Do", "In Progress", "Done", "In Review", "Open", "Closed"}
	mappings := AutoMapColumns(statuses)

	// Check that standard statuses are mapped
	backlog := findMapping(mappings, "Backlog")
	if backlog == nil {
		t.Fatal("missing Backlog column")
	}
	if !contains(backlog.Statuses, "To Do") || !contains(backlog.Statuses, "Open") {
		t.Errorf("Backlog statuses = %v", backlog.Statuses)
	}

	doing := findMapping(mappings, "Doing")
	if doing == nil {
		t.Fatal("missing Doing column")
	}
	if !contains(doing.Statuses, "In Progress") {
		t.Errorf("Doing statuses = %v", doing.Statuses)
	}

	done := findMapping(mappings, "Done")
	if done == nil {
		t.Fatal("missing Done column")
	}
	if !contains(done.Statuses, "Done") || !contains(done.Statuses, "Closed") {
		t.Errorf("Done statuses = %v", done.Statuses)
	}
}

func TestAutoMapColumns_UnmatchedFlaggedAsUnmapped(t *testing.T) {
	statuses := []string{"Custom Status", "Weird State"}
	mappings := AutoMapColumns(statuses)

	unmapped := findMapping(mappings, "Unmapped")
	if unmapped == nil {
		t.Fatal("missing Unmapped column for unrecognized statuses")
	}
	if len(unmapped.Statuses) != 2 {
		t.Errorf("unmapped statuses = %v, want 2", unmapped.Statuses)
	}
}

// Task 6.8: Config file writing
func TestWriteConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legato", "config.yaml")

	cfg := WizardConfig{
		BaseURL:     "https://company.atlassian.net",
		Email:       "dev@company.com",
		ProjectKeys: []string{"PROJ", "DEV"},
		Columns: []ColumnMappingConfig{
			{Name: "Backlog", Statuses: []string{"To Do", "Open"}},
			{Name: "Doing", Statuses: []string{"In Progress"}},
			{Name: "Done", Statuses: []string{"Done", "Closed"}},
		},
	}

	err := WriteConfig(path, cfg)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Read and parse
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	// Token should be env var reference
	jira, _ := parsed["jira"].(map[string]any)
	token, _ := jira["api_token"].(string)
	if token != "${LEGATO_JIRA_TOKEN}" {
		t.Errorf("token = %q, want ${LEGATO_JIRA_TOKEN}", token)
	}
}

// Task 6.7: Transition ID discovery
func TestDiscoverTransitions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"transitions":[
			{"id":"21","name":"Start Progress","to":{"name":"In Progress"}},
			{"id":"31","name":"Done","to":{"name":"Done"}}
		]}`))
	}))
	defer srv.Close()

	transitions, err := FetchTransitions(context.Background(), srv.URL, "a@b.com", "tok", "PROJ-1")
	if err != nil {
		t.Fatalf("fetch transitions: %v", err)
	}
	if len(transitions) != 2 {
		t.Errorf("got %d transitions, want 2", len(transitions))
	}
	if transitions[0].ID != "21" || transitions[0].TargetStatus != "In Progress" {
		t.Errorf("transition[0] = %+v", transitions[0])
	}
}

func findMapping(mappings []ColumnMappingConfig, name string) *ColumnMappingConfig {
	for _, m := range mappings {
		if m.Name == name {
			return &m
		}
	}
	return nil
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
