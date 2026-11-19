package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

// ErrSetupStopped is a setup the operator stopped at a failed required
// check. What was set before it stays.
var ErrSetupStopped = errors.New("setup stopped at a failed check")

// Settings reads and keeps what setup is told: settings by key (JSON
// values), whether config.yaml pins one, and secrets by name.
type Settings interface {
	SettingsSink
	// Setting is a setting's value as JSON ("" when unset) and whether
	// config.yaml pins it.
	Setting(ctx context.Context, key string) (raw string, pinned bool, err error)
}

// Flow is `legato setup`, and the first run of the TUI: plain line prompts,
// so it works over SSH. Every step can be skipped with s; on a rerun each
// shows the current value, which Enter keeps. Pinned settings are shown and
// skipped, and nothing is removed.
type Flow struct {
	Out io.Writer
	In  io.Reader
	// ReadSecret reads a secret without showing it; nil reads a line.
	ReadSecret func() (string, error)
	// Checks runs the doctor.
	Checks   func(ctx context.Context) service.DoctorReport
	Settings Settings
	// Columns seeds the board with DefaultColumns when it has none.
	Columns        ColumnSeeder
	DefaultColumns []config.ColumnConfig
	Jira           JiraSetup
	// InstallHooks installs an agent tool's hooks, saying where.
	InstallHooks func(ctx context.Context, tool string) (string, error)
	// SettingsLink is where the web UI's integrations are, for Burnlite.
	SettingsLink string
	WorkDir      string
	// Repositories keeps repository presets; nil leaves the step out.
	Repositories RepositoryPresets
	// RepositoryRoots are the folders repositories are looked for in
	// (DefaultRepositoryRoots).
	RepositoryRoots []string
	// Now is the clock that says which repositories were used lately.
	Now func() time.Time
	// Web turns on the web UI and pairs a phone; nil leaves the step out.
	Web WebSetup
	// PairOnStart is set when the operator asked to pair a phone and the
	// web server is not up yet: the caller pairs once it has started it.
	PairOnStart bool

	lines  *bufio.Scanner
	report service.DoctorReport
	// seeded says this run gave the board its columns, which Jira's
	// statuses may then replace.
	seeded bool
}

// agentTools are the agent tools setup offers, by check ID.
var agentTools = []string{"claude-code", "codex", "chimera"}

// setupStep is one titled step of the flow.
type setupStep struct {
	title string
	run   func(context.Context) error
}

// Run runs every step in order.
func (f *Flow) Run(ctx context.Context) error {
	f.lines = bufio.NewScanner(f.In)
	fmt.Fprintln(f.Out, "Welcome to Legato! A few questions get it ready. Type s to skip a step; Enter keeps what is shown in [brackets].")
	if err := f.seedColumns(ctx); err != nil {
		return err
	}
	steps := []setupStep{{"Checks", f.stepChecks}, {"Default agent tool", f.stepDefaultTool}}
	if f.Repositories != nil {
		steps = append(steps, setupStep{"Repositories", f.stepRepositories})
	}
	if f.Web != nil {
		steps = append(steps, setupStep{"Web UI and phone", f.stepWeb})
	}
	steps = append(steps, setupStep{"Ticket tracker", f.stepTracker})
	for i, step := range steps {
		fmt.Fprintf(f.Out, "\n%d/%d  %s\n", i+1, len(steps), step.title)
		if err := step.run(ctx); err != nil {
			return err
		}
	}
	fmt.Fprintln(f.Out, "\nSetup is done. Run legato setup again at any time.")
	return nil
}

func (f *Flow) seedColumns(ctx context.Context) error {
	if f.Columns == nil {
		return nil
	}
	seeded, err := SeedColumnsIfEmpty(ctx, f.Columns, f.DefaultColumns)
	if err != nil {
		return err
	}
	if seeded {
		f.seeded = true
		names := make([]string, len(f.DefaultColumns))
		for i, col := range f.DefaultColumns {
			names[i] = col.Name
		}
		fmt.Fprintf(f.Out, "Made the board's columns: %s.\n", strings.Join(names, ", "))
	}
	return nil
}

// ask prints a prompt and reads a line; at the end of input it is "".
func (f *Flow) ask(prompt string) string {
	fmt.Fprint(f.Out, prompt)
	if !f.lines.Scan() {
		fmt.Fprintln(f.Out)
		return ""
	}
	return strings.TrimSpace(f.lines.Text())
}

func (f *Flow) secret(prompt string) (string, error) {
	fmt.Fprint(f.Out, prompt)
	if f.ReadSecret == nil {
		return f.ask(""), nil
	}
	value, err := f.ReadSecret()
	fmt.Fprintln(f.Out)
	return strings.TrimSpace(value), err
}

func skip(answer string) bool {
	return strings.EqualFold(answer, "s") || strings.EqualFold(answer, "skip")
}

// setting reads a string setting.
func (f *Flow) setting(ctx context.Context, key string) (string, bool) {
	raw, pinned, err := f.Settings.Setting(ctx, key)
	if err != nil || raw == "" {
		return "", pinned
	}
	var s string
	if json.Unmarshal([]byte(raw), &s) != nil {
		return raw, pinned
	}
	return s, pinned
}

func (f *Flow) set(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return f.Settings.SetSetting(ctx, key, raw)
}

// isAgentCheck says a check is an agent tool's, which the default tool
// step deals with.
func isAgentCheck(id string) bool {
	base, _, _ := strings.Cut(id, ".")
	return slices.Contains(agentTools, base)
}

// stepChecks runs the doctor and stops at a failed required check until
// it is fixed (Enter checks again) or the operator stops (q).
func (f *Flow) stepChecks(ctx context.Context) error {
	for {
		f.report = f.Checks(ctx)
		var failed []service.DoctorCheck
		for _, c := range f.report.RequiredFailed() {
			if !isAgentCheck(c.ID) {
				failed = append(failed, c)
			}
		}
		if len(failed) == 0 {
			fmt.Fprintln(f.Out, "  ✓ This machine has what Legato needs.")
			for _, c := range f.report.Checks {
				if !c.Required && !isAgentCheck(c.ID) && c.Failed() {
					fmt.Fprintf(f.Out, "  · %s (optional): %s\n", c.Title, c.Fix)
				}
			}
			return nil
		}
		for _, c := range failed {
			fmt.Fprintf(f.Out, "  ✗ %s: %s\n    fix: %s\n", c.Title, c.Detail, c.Fix)
		}
		if answer := f.ask("  Fix it, then press Enter to check again, or type q to stop: "); strings.EqualFold(answer, "q") {
			return ErrSetupStopped
		}
	}
}

func (f *Flow) check(id string) (service.DoctorCheck, bool) {
	for _, c := range f.report.Checks {
		if c.ID == id {
			return c, true
		}
	}
	return service.DoctorCheck{}, false
}

// stepDefaultTool chooses the default agent tool among those installed and
// runnable, and offers the hooks Codex and Chimera need.
func (f *Flow) stepDefaultTool(ctx context.Context) error {
	current, pinned := f.setting(ctx, "agents.default_tool")
	if pinned {
		fmt.Fprintf(f.Out, "  Default agent tool: %s (pinned in config.yaml)\n", current)
		return nil
	}
	if current == "" {
		current = "claude-code"
	}
	var found []service.DoctorCheck
	for _, id := range agentTools {
		if c, ok := f.check(id); ok && c.Result == service.DoctorOK {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		fmt.Fprintln(f.Out, "  No agent tool is installed. Install one, then run legato setup again:")
		for _, id := range agentTools {
			if c, ok := f.check(id); ok {
				fmt.Fprintf(f.Out, "    %s: %s\n", c.Title, c.Fix)
			}
		}
		return nil
	}
	for i, c := range found {
		state := "signed in"
		if auth, ok := f.check(c.ID + ".auth"); ok && auth.Result != service.DoctorOK {
			state = "not signed in"
		}
		fmt.Fprintf(f.Out, "  %d. %s (%s, %s)\n", i+1, c.ID, c.Title, state)
	}
	for {
		answer := f.ask(fmt.Sprintf("  Default agent tool [%s]: ", current))
		if answer == "" || skip(answer) {
			return nil
		}
		chosen := ""
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(found) {
			chosen = found[n-1].ID
		} else {
			for _, c := range found {
				if strings.EqualFold(answer, c.ID) {
					chosen = c.ID
				}
			}
		}
		if chosen == "" {
			fmt.Fprintf(f.Out, "  Type a number from 1 to %d, Enter to keep %s, or s to skip.\n", len(found), current)
			continue
		}
		if chosen == current {
			return nil
		}
		if err := f.set(ctx, "agents.default_tool", chosen); err != nil {
			return fmt.Errorf("saving the default agent tool: %w", err)
		}
		fmt.Fprintf(f.Out, "  ✓ Legato starts %s when you name no tool.\n", chosen)
		if auth, ok := f.check(chosen + ".auth"); ok && auth.Failed() {
			fmt.Fprintf(f.Out, "  ! %s: %s\n", auth.Title, auth.Fix)
		}
		return f.offerHooks(ctx, chosen)
	}
}

func (f *Flow) offerHooks(ctx context.Context, tool string) error {
	if f.InstallHooks == nil || (tool != "codex" && tool != "chimera") {
		return nil
	}
	where := "your home folder"
	if tool == "codex" {
		where = f.WorkDir + " (Codex reads them from each project)"
	}
	answer := f.ask(fmt.Sprintf("  Install %s's hooks in %s, so Legato sees what its agents do? [y/N]: ", tool, where))
	if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
		fmt.Fprintf(f.Out, "  Later: legato hooks install --tool %s\n", tool)
		return nil
	}
	said, err := f.InstallHooks(ctx, tool)
	if err != nil {
		fmt.Fprintf(f.Out, "  ✗ %v. Later: legato hooks install --tool %s\n", err, tool)
		return nil
	}
	fmt.Fprintf(f.Out, "  ✓ %s\n", said)
	return nil
}

// stepTracker connects a ticket tracker: none, Jira here, or Burnlite in
// the web UI.
func (f *Flow) stepTracker(ctx context.Context) error {
	jiraURL, pinned := f.setting(ctx, "jira.base_url")
	if pinned {
		fmt.Fprintf(f.Out, "  Ticket tracker: Jira at %s (pinned in config.yaml)\n", jiraURL)
		return nil
	}
	current := "none"
	if jiraURL != "" {
		current = "jira"
	}
	fmt.Fprintln(f.Out, "  1. none: a board of local tasks\n  2. jira\n  3. burnlite (connected in the web UI)")
	for {
		answer := strings.ToLower(f.ask(fmt.Sprintf("  Ticket tracker [%s]: ", current)))
		switch {
		case answer == "" || skip(answer):
			return nil
		case answer == "1" || answer == "none":
			return nil
		case answer == "2" || answer == "jira":
			return f.connectJira(ctx)
		case answer == "3" || answer == "burnlite":
			fmt.Fprintf(f.Out, "  Connect Burnlite in Settings › Integrations: %s\n  (or run legato integration connect burnlite)\n", f.SettingsLink)
			return nil
		}
		fmt.Fprintln(f.Out, "  Type 1, 2 or 3, Enter to keep it, or s to skip.")
	}
}

// connectJira asks for Jira's address and credentials until they work or
// the operator skips. The token is never shown.
func (f *Flow) connectJira(ctx context.Context) error {
	if f.Jira == nil {
		fmt.Fprintln(f.Out, "  Jira cannot be set up here. Use Settings › Integrations in the web UI.")
		return nil
	}
	baseURL, _ := f.setting(ctx, "jira.base_url")
	email, _ := f.setting(ctx, "jira.email")
	var token string
	for {
		if answer := f.ask(fmt.Sprintf("  Jira address (https://yourteam.atlassian.net)%s: ", bracket(baseURL))); answer != "" {
			baseURL = answer
		}
		if answer := f.ask(fmt.Sprintf("  Email%s: ", bracket(email))); answer != "" {
			email = answer
		}
		typed, err := f.secret("  API token (not shown as you type): ")
		if err != nil {
			return fmt.Errorf("reading the token: %w", err)
		}
		if typed != "" {
			token = typed
		}
		err = ValidateBaseURL(baseURL)
		if err == nil {
			fmt.Fprint(f.Out, "  Checking... ")
			err = f.Jira.ValidateCredentials(ctx, baseURL, email, token)
		}
		if err == nil {
			fmt.Fprintln(f.Out, "✓")
			break
		}
		fmt.Fprintf(f.Out, "✗ %v\n", err)
		if skip(f.ask("  Press Enter to try again, or s to skip Jira: ")) {
			return nil
		}
	}
	projects, err := f.Jira.FetchProjects(ctx, baseURL, email, token)
	if err != nil || len(projects) == 0 {
		fmt.Fprintf(f.Out, "  ✗ Could not list Jira's projects: %v. Finish in Settings › Integrations.\n", err)
		return nil
	}
	for i, p := range projects {
		fmt.Fprintf(f.Out, "    %d. %s - %s\n", i+1, p.Key, p.Name)
	}
	var project Project
	for {
		answer := f.ask("  Project (number): ")
		if skip(answer) {
			return nil
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(projects) {
			project = projects[n-1]
			break
		}
		fmt.Fprintf(f.Out, "  Type a number from 1 to %d, or s to skip.\n", len(projects))
	}
	if err := saveJira(ctx, f.Settings, baseURL, email, token, project.Key); err != nil {
		return fmt.Errorf("saving Jira's settings: %w", err)
	}
	fmt.Fprintln(f.Out, "  ✓ Jira's settings are saved, and the token in Legato's secret store.")
	if f.seeded && f.Columns != nil {
		f.mapJiraColumns(ctx, baseURL, email, token, project.Key)
	} else {
		fmt.Fprintln(f.Out, "  Your board's columns are kept: map Jira's statuses to them in Settings.")
	}
	return nil
}

// mapJiraColumns replaces the columns this run made with ones mapped from
// the project's statuses. Columns that were there before are never touched.
func (f *Flow) mapJiraColumns(ctx context.Context, baseURL, email, token, project string) {
	statuses, err := f.Jira.FetchStatuses(ctx, baseURL, email, token, project)
	if err != nil {
		fmt.Fprintf(f.Out, "  ✗ Could not read the project's statuses (%v): the default columns stay.\n", err)
		return
	}
	existing, err := f.Columns.ListColumnMappings(ctx)
	if err != nil {
		return
	}
	for _, m := range existing {
		if err := f.Columns.DeleteColumnMapping(ctx, m.ID); err != nil {
			return
		}
	}
	mapped := AutoMapColumns(statuses)
	columns := make([]config.ColumnConfig, len(mapped))
	names := make([]string, len(mapped))
	for i, col := range mapped {
		columns[i] = config.ColumnConfig{Name: col.Name, RemoteStatuses: col.Statuses}
		names[i] = col.Name
	}
	if err := seedColumns(ctx, f.Columns, columns); err != nil {
		fmt.Fprintf(f.Out, "  ✗ %v\n", err)
		return
	}
	fmt.Fprintf(f.Out, "  ✓ Columns mapped from Jira's statuses: %s.\n", strings.Join(names, ", "))
}

func bracket(s string) string {
	if s == "" {
		return ""
	}
	return " [" + s + "]"
}
