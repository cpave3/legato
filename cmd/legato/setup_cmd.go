package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/setup"
)

// runSetupCmd is `legato setup`: the guided setup the first run gives, again.
func runSetupCmd(args []string) int {
	const command = "setup"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("setup"))
	if parseErr != nil {
		return renderCommandError(command, parseErr, false)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "setup accepts no positional arguments"), false)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, false)
	}
	db, err := openStore(cfg)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "database_unavailable", Message: err.Error(), Exit: exitEnvironment}, false)
	}
	defer db.Close()
	flow := setupFlow(cfg, db, os.Stdin, os.Stdout, false)
	if err := flow.Run(context.Background()); err != nil {
		if errors.Is(err, setup.ErrSetupStopped) {
			fmt.Fprintln(os.Stdout, "Setup stopped. Run legato setup again once it is fixed.")
			return exitDependency
		}
		return renderCommandError(command, &commandError{Code: "setup_failed", Message: err.Error(), Exit: exitInternal}, false)
	}
	return exitOK
}

// setupFlow is the guided setup with this process's configuration and
// store, on in and out. firstRun is the TUI's first start, which starts
// the web server itself once setup is done.
func setupFlow(cfg *config.Config, db *store.Store, in io.Reader, out io.Writer, firstRun bool) *setup.Flow {
	wd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	return &setup.Flow{
		Out: out, In: in,
		ReadSecret: terminalSecret(in),
		Checks: func(ctx context.Context) service.DoctorReport {
			global, project := workflowDirs()
			return processDoctor().Check(ctx, doctorConfig(cfg, workflow.DiscoverPaths(legacyWorkflowDir(), global, project).Definitions), false)
		},
		Settings:       setupSettings{wizardSettings{cfg: cfg, db: db}},
		Columns:        &setup.StoreAdapter{S: db},
		DefaultColumns: cfg.Board.Columns,
		Jira:           &setup.RealJiraSetup{},
		InstallHooks: func(_ context.Context, tool string) (string, error) {
			return installToolHooks(cfg, tool, wd)
		},
		SettingsLink:    webSettingsLink(cfg, "integrations"),
		WorkDir:         wd,
		Repositories:    newRepositoryPresetService(cfg, db, nil),
		RepositoryRoots: setup.DefaultRepositoryRoots(home, wd),
		Web:             webSetup{cfg: cfg, firstRun: firstRun, out: out},
	}
}

// terminalSecret reads a secret from a terminal without echoing it, or nil
// (a plain line) when in is not one.
func terminalSecret(in io.Reader) func() (string, error) {
	file, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return nil
	}
	return func() (string, error) {
		secret, err := term.ReadPassword(int(file.Fd()))
		return string(secret), err
	}
}

// webSettingsLink is the address of a part of the web UI's Settings: the
// configured public address, else this machine's web port.
func webSettingsLink(cfg *config.Config, part string) string {
	base := strings.TrimRight(cfg.Web.PublicURL, "/")
	if base == "" {
		port := cfg.Web.Port
		if port == "" {
			port = "3080"
		}
		base = "https://localhost:" + port
	}
	return base + "/settings#" + part
}

// installToolHooks installs an agent tool's hooks, as legato hooks install
// --tool does, and says what it did.
func installToolHooks(cfg *config.Config, tool, dir string) (string, error) {
	legatoBin, err := os.Executable()
	if err != nil {
		legatoBin = "legato"
	}
	switch tool {
	case "codex":
		if err := hooks.NewCodexAdapter(legatoBin).InstallHooks(dir); err != nil {
			return "", err
		}
		return "Installed Codex's hooks in " + dir + ". Run legato hooks install --tool codex in each other repository.", nil
	case "chimera":
		chimera := hooks.NewChimeraAdapter(legatoBin)
		native := nativeBridgeEnabled(cfg, "chimera")
		if native {
			chimera.SetModDir(filepath.Join(config.ResolveDataDir(cfg), "chimera-mod", "legato"))
		}
		if err := chimera.InstallHooks(dir); err != nil {
			return "", err
		}
		if native {
			return "Chimera's state is reported by Legato's mod; old hook scripts were removed.", nil
		}
		return "Installed Chimera's hooks in ~/.chimera/hooks.", nil
	}
	return "", fmt.Errorf("%s has no hooks to install", tool)
}

// setupSettings reads settings for setup: the effective value of each and
// whether config.yaml pins it.
type setupSettings struct{ wizardSettings }

func (s setupSettings) Setting(ctx context.Context, key string) (string, bool, error) {
	value, err := settingsFor(s.db).Get(ctx, key)
	if err != nil {
		return "", false, err
	}
	if value.Value == nil {
		return "", value.Pinned, nil
	}
	raw, err := json.Marshal(value.Value)
	if err != nil {
		return "", value.Pinned, err
	}
	if string(raw) == `""` {
		raw = nil
	}
	return string(raw), value.Pinned, nil
}
