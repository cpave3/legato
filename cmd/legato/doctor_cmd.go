package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// doctorReport runs the checks for legato doctor; tests replace it.
var doctorReport = func(ctx context.Context, online bool) (service.DoctorReport, error) {
	return service.NewDoctor(service.DoctorOptions{}).Check(ctx, loadDoctorConfig(), online), nil
}

// loadDoctorConfig reads what the effective configuration requires of the
// machine. A configuration that cannot be read requires only the defaults:
// the checks still run.
func loadDoctorConfig() service.DoctorConfig {
	cfg, err := config.Load()
	if err != nil {
		return service.DoctorConfig{}
	}
	if db, err := openStore(cfg); err == nil {
		db.Close()
	}
	return doctorConfigFor(cfg)
}

// doctorConfigFor is doctorConfig with the workflows found on this machine.
func doctorConfigFor(cfg *config.Config) service.DoctorConfig {
	return doctorConfig(cfg, workflow.DiscoverFolders(workflowFolders(workflowRepositoryPresets(cfg))).Definitions)
}

// doctorConfig is what the settings require of the machine: the default
// agent tool, bwrap with the sandbox on, and the optional tools a setting
// in use depends on.
func doctorConfig(cfg *config.Config, workflows []*workflow.Definition) service.DoctorConfig {
	out := service.DoctorConfig{
		DefaultAgent:   firstNonEmpty(cfg.DefaultAgentTool(), "claude-code"),
		SandboxEnabled: cfg.Agents.Sandbox.Enabled,
		RequiredBy:     map[string]string{},
	}
	if cfg.GitHub.Auth == "gh" {
		out.RequiredBy["gh"] = "PR tracking (github.auth: gh)"
		out.RequiredBy["gh.auth"] = "PR tracking (github.auth: gh)"
	}
	for _, def := range workflows {
		for _, step := range def.Steps {
			if step.Type == "worktree.remove" {
				out.RequiredBy["yg"] = "workflow " + def.Name + " (worktree.remove)"
			}
		}
	}
	return out
}

func runDoctorCmd(args []string) int {
	const command = "doctor"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("doctor"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "doctor accepts no positional arguments"), jsonMode)
	}
	report, err := doctorReport(context.Background(), parsed.Present["online"])
	if err != nil {
		return renderCommandError(command, &commandError{Code: "doctor_failed", Message: err.Error(), Exit: exitInternal}, jsonMode)
	}
	exit := exitOK
	if !report.OK() {
		exit = exitDependency
	}
	if jsonMode {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": report.OK(), "command": command, "data": report})
		return exit
	}
	fmt.Fprint(os.Stdout, formatDoctorReport(report))
	return exit
}

// formatDoctorReport is the report for a person: each check on a line, its
// fix under it when it is not ok, then what it adds up to.
func formatDoctorReport(r service.DoctorReport) string {
	var b strings.Builder
	for _, c := range r.Checks {
		mark := "ok  "
		switch {
		case c.Result == service.DoctorSkipped:
			mark = "--  "
		case c.Failed() && c.Required:
			mark = "FAIL"
		case c.Failed():
			mark = "note"
		}
		kind := "optional"
		if c.Required {
			kind = "required"
			if c.RequiredBy != "" {
				kind += " by " + c.RequiredBy
			}
		}
		fmt.Fprintf(&b, "%s  %s (%s)", mark, c.Title, kind)
		if c.Detail != "" {
			fmt.Fprintf(&b, ": %s", c.Detail)
		}
		b.WriteString("\n")
		if c.Failed() && c.Fix != "" {
			fmt.Fprintf(&b, "      fix: %s\n", c.Fix)
		}
	}
	if failed := r.RequiredFailed(); len(failed) > 0 {
		fmt.Fprintf(&b, "\n%d required check(s) failed: part of Legato cannot work until they are fixed.\n", len(failed))
	} else {
		b.WriteString("\nEvery required check passed.\n")
	}
	b.WriteString("Signed in means credentials are present, not that they work. legato doctor --online also asks GitHub.\n")
	return b.String()
}
