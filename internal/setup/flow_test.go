package setup

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

// fakeColumnStore implements ColumnSeeder for testing.
type fakeColumnStore struct {
	mappings []ColumnMappingRow
	nextID   int
}

func (f *fakeColumnStore) ListColumnMappings(ctx context.Context) ([]ColumnMappingRow, error) {
	cp := make([]ColumnMappingRow, len(f.mappings))
	copy(cp, f.mappings)
	return cp, nil
}

func (f *fakeColumnStore) CreateColumnMapping(ctx context.Context, m ColumnMappingRow) error {
	f.nextID++
	m.ID = f.nextID
	f.mappings = append(f.mappings, m)
	return nil
}

func (f *fakeColumnStore) DeleteColumnMapping(ctx context.Context, id int) error {
	for i, m := range f.mappings {
		if m.ID == id {
			f.mappings = append(f.mappings[:i], f.mappings[i+1:]...)
			return nil
		}
	}
	return nil
}

type fakeJiraSetup struct {
	// validateErrs are the answers of successive validations; nil after.
	validateErrs []error
	validated    []string
	projects     []Project
	statuses     []string
}

func (f *fakeJiraSetup) ValidateCredentials(ctx context.Context, baseURL, email, token string) error {
	f.validated = append(f.validated, baseURL+" "+email+" "+token)
	if len(f.validateErrs) == 0 {
		return nil
	}
	err := f.validateErrs[0]
	f.validateErrs = f.validateErrs[1:]
	return err
}

func (f *fakeJiraSetup) FetchProjects(ctx context.Context, baseURL, email, token string) ([]Project, error) {
	return f.projects, nil
}

func (f *fakeJiraSetup) FetchStatuses(ctx context.Context, baseURL, email, token, projectKey string) ([]string, error) {
	return f.statuses, nil
}

// fakeSettings keeps what the wizard saves in the database.
type fakeSettings struct {
	values  map[string]string
	secrets map[string]string
	// pinned are the keys config.yaml sets, with their values as JSON.
	pinned map[string]string
	sets   []string
}

func (f *fakeSettings) Setting(_ context.Context, key string) (string, bool, error) {
	if v, ok := f.pinned[key]; ok {
		return v, true, nil
	}
	return f.values[key], false, nil
}

func (f *fakeSettings) SetSetting(_ context.Context, key string, raw []byte) error {
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[key] = string(raw)
	f.sets = append(f.sets, key)
	return nil
}

func (f *fakeSettings) StoreSecret(_ context.Context, name, value string) (string, error) {
	if f.secrets == nil {
		f.secrets = map[string]string{}
	}
	f.secrets[name] = value
	return "legato://" + name, nil
}

func TestSeedColumnsIfEmptySeedsAnEmptyBoard(t *testing.T) {
	store := &fakeColumnStore{}
	seeded, err := SeedColumnsIfEmpty(context.Background(), store, config.DefaultColumns())
	if err != nil || !seeded {
		t.Fatalf("seeded %v, %v", seeded, err)
	}
	if len(store.mappings) != len(config.DefaultColumns()) {
		t.Fatalf("seeded %d columns", len(store.mappings))
	}
}

func TestSeedColumnsIfEmptyLeavesABoardWithColumns(t *testing.T) {
	store := &fakeColumnStore{mappings: []ColumnMappingRow{{ID: 1, ColumnName: "Inbox"}}}
	seeded, err := SeedColumnsIfEmpty(context.Background(), store, config.DefaultColumns())
	if err != nil || seeded || len(store.mappings) != 1 {
		t.Fatalf("seeded %v, %v: %+v", seeded, err, store.mappings)
	}
}

// machine is a doctor's report: every tool present and signed in, less
// what a test takes away.
func machine(change func(r *service.DoctorReport)) service.DoctorReport {
	r := service.DoctorReport{Checks: []service.DoctorCheck{
		{ID: "tmux", Title: "tmux", Required: true, Result: service.DoctorOK},
		{ID: "git", Title: "git", Required: true, Result: service.DoctorOK},
		{ID: "claude-code", Title: "Claude Code", Required: true, Result: service.DoctorOK},
		{ID: "claude-code.auth", Title: "Claude Code signed in", Required: true, Result: service.DoctorOK},
		{ID: "codex", Title: "Codex", Result: service.DoctorOK},
		{ID: "codex.auth", Title: "Codex signed in", Result: service.DoctorNotSignedIn, Fix: "Run codex login."},
		{ID: "chimera", Title: "Chimera", Result: service.DoctorMissing, Fix: "Install Chimera"},
		{ID: "chimera.auth", Title: "Chimera signed in", Result: service.DoctorSkipped},
	}}
	if change != nil {
		change(&r)
	}
	return r
}

type setupRun struct {
	out      bytes.Buffer
	settings *fakeSettings
	columns  *fakeColumnStore
	jira     *fakeJiraSetup
	hooks    []string
	checks   int
	err      error
}

// runSetup runs the flow on scripted lines, with secrets typed apart.
func runSetup(t *testing.T, input string, secrets []string, reports []service.DoctorReport, prepare func(*setupRun)) *setupRun {
	t.Helper()
	r := &setupRun{settings: &fakeSettings{values: map[string]string{}}, columns: &fakeColumnStore{}, jira: &fakeJiraSetup{
		projects: []Project{{Key: "REX", Name: "Rex App"}, {Key: "INF", Name: "Infrastructure"}},
		statuses: []string{"To Do", "In Progress", "In Review", "Done"},
	}}
	if prepare != nil {
		prepare(r)
	}
	flow := Flow{
		Out: &r.out, In: strings.NewReader(input),
		ReadSecret: func() (string, error) {
			if len(secrets) == 0 {
				return "", errors.New("no secret typed")
			}
			s := secrets[0]
			secrets = secrets[1:]
			return s, nil
		},
		Checks: func(context.Context) service.DoctorReport {
			report := reports[min(r.checks, len(reports)-1)]
			r.checks++
			return report
		},
		Settings: r.settings, Columns: r.columns, DefaultColumns: config.DefaultColumns(), Jira: r.jira,
		InstallHooks: func(_ context.Context, tool string) (string, error) {
			r.hooks = append(r.hooks, tool)
			return "installed " + tool + "'s hooks", nil
		},
		SettingsLink: "https://legato.local:3080/settings#integrations",
		WorkDir:      "/home/op/code/app",
	}
	r.err = flow.Run(context.Background())
	return r
}

func TestSetupStopsOnAFailedRequiredCheckWithItsFix(t *testing.T) {
	tmuxMissing := machine(func(r *service.DoctorReport) {
		r.Checks[0] = service.DoctorCheck{ID: "tmux", Title: "tmux", Required: true, Result: service.DoctorMissing, Detail: "tmux not found", Fix: "Install tmux (apt install tmux)"}
	})
	r := runSetup(t, "\nq\n", nil, []service.DoctorReport{tmuxMissing}, nil)
	if !errors.Is(r.err, ErrSetupStopped) {
		t.Fatalf("setup = %v, want it stopped", r.err)
	}
	if !strings.Contains(r.out.String(), "Install tmux (apt install tmux)") {
		t.Errorf("no fix in:\n%s", r.out.String())
	}
	if r.checks != 2 {
		t.Errorf("checked %d times; Enter should check again", r.checks)
	}
	if len(r.settings.sets) != 0 {
		t.Errorf("set %v after a stop", r.settings.sets)
	}
}

func TestSetupGoesOnOnceTheFixedCheckPasses(t *testing.T) {
	tmuxMissing := machine(func(r *service.DoctorReport) { r.Checks[0].Result = service.DoctorMissing })
	r := runSetup(t, "\ns\ns\n", nil, []service.DoctorReport{tmuxMissing, machine(nil)}, nil)
	if r.err != nil {
		t.Fatalf("setup = %v\n%s", r.err, r.out.String())
	}
}

func TestSetupSeedsTheBoardOnAFirstRun(t *testing.T) {
	r := runSetup(t, "s\ns\n", nil, []service.DoctorReport{machine(nil)}, nil)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.columns.mappings) != len(config.DefaultColumns()) {
		t.Fatalf("columns = %+v", r.columns.mappings)
	}
}

func TestSetupChoosesTheDefaultAgentToolFromThoseFound(t *testing.T) {
	r := runSetup(t, "2\ny\ns\n", nil, []service.DoctorReport{machine(nil)}, nil)
	if r.err != nil {
		t.Fatal(r.err)
	}
	out := r.out.String()
	if strings.Contains(out, "Chimera") && strings.Contains(out, "3. Chimera") {
		t.Errorf("offered a tool that is missing:\n%s", out)
	}
	if r.settings.values["agents.default_tool"] != `"codex"` {
		t.Errorf("default tool = %s", r.settings.values["agents.default_tool"])
	}
	if !strings.Contains(out, "Run codex login.") {
		t.Errorf("did not say Codex is signed out:\n%s", out)
	}
	if !slices.Equal(r.hooks, []string{"codex"}) {
		t.Errorf("hooks installed for %v", r.hooks)
	}
}

func TestSetupStepsCanBeSkipped(t *testing.T) {
	r := runSetup(t, "s\ns\n", nil, []service.DoctorReport{machine(nil)}, nil)
	if r.err != nil || len(r.settings.sets) != 0 || len(r.hooks) != 0 {
		t.Fatalf("err %v, set %v, hooks %v", r.err, r.settings.sets, r.hooks)
	}
}

func TestSetupConnectsJiraWithTheTokenHiddenAndARetry(t *testing.T) {
	input := "s\n" + // default tool
		"2\n" + // Jira
		"https://team.atlassian.net\nuser@example.com\n" + // then the token, hidden
		"\n" + // validation failed: Enter tries again
		"\n\n" + // keep the URL and email
		"1\n" // project
	r := runSetup(t, input, []string{"bad-token", "good-token"}, []service.DoctorReport{machine(nil)}, func(r *setupRun) {
		r.jira.validateErrs = []error{errors.New("authentication failed: invalid email or API token")}
	})
	if r.err != nil {
		t.Fatalf("setup = %v\n%s", r.err, r.out.String())
	}
	out := r.out.String()
	if strings.Contains(out, "good-token") || strings.Contains(out, "bad-token") {
		t.Errorf("the token was shown:\n%s", out)
	}
	if !strings.Contains(out, "authentication failed") {
		t.Errorf("the error was not shown:\n%s", out)
	}
	if len(r.jira.validated) != 2 || r.jira.validated[1] != "https://team.atlassian.net user@example.com good-token" {
		t.Errorf("validated %v", r.jira.validated)
	}
	want := map[string]string{
		"jira.base_url":     `"https://team.atlassian.net"`,
		"jira.email":        `"user@example.com"`,
		"jira.project_keys": `["REX"]`,
		"jira.api_token":    `"legato://jira-api-token"`,
	}
	for key, value := range want {
		if r.settings.values[key] != value {
			t.Errorf("%s = %s, want %s", key, r.settings.values[key], value)
		}
	}
	if r.settings.secrets["jira-api-token"] != "good-token" {
		t.Errorf("secrets = %v", r.settings.secrets)
	}
	if len(r.columns.mappings) != 4 {
		t.Errorf("a first run's columns were not mapped to Jira's statuses: %+v", r.columns.mappings)
	}
}

func TestSetupNamesWhereToConnectBurnlite(t *testing.T) {
	r := runSetup(t, "s\n3\n", nil, []service.DoctorReport{machine(nil)}, nil)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !strings.Contains(r.out.String(), "https://legato.local:3080/settings#integrations") {
		t.Errorf("no Settings link:\n%s", r.out.String())
	}
}

func TestSetupRerunKeepsValuesAndSkipsPinnedSettings(t *testing.T) {
	r := runSetup(t, "\n", nil, []service.DoctorReport{machine(nil)}, func(r *setupRun) {
		r.settings.values["agents.default_tool"] = `"codex"`
		r.settings.pinned = map[string]string{"jira.base_url": `"https://team.atlassian.net"`}
		r.columns.mappings = []ColumnMappingRow{{ID: 1, ColumnName: "Inbox"}, {ID: 2, ColumnName: "Shipped"}}
	})
	if r.err != nil {
		t.Fatalf("setup = %v\n%s", r.err, r.out.String())
	}
	out := r.out.String()
	if !strings.Contains(out, "[codex]") {
		t.Errorf("the current tool is not shown:\n%s", out)
	}
	if !strings.Contains(out, "pinned in config.yaml") {
		t.Errorf("the pinned tracker is not shown as pinned:\n%s", out)
	}
	if len(r.settings.sets) != 0 || len(r.hooks) != 0 {
		t.Errorf("a rerun of Enter changed %v, hooks %v", r.settings.sets, r.hooks)
	}
	if len(r.columns.mappings) != 2 || r.columns.mappings[0].ColumnName != "Inbox" {
		t.Errorf("columns changed: %+v", r.columns.mappings)
	}
}
