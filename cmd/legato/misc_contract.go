package main

import (
	"fmt"
	"os"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/auth"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/service"
	qrterminal "github.com/mdp/qrterminal/v3"
)

func runHooksContract(args []string) int {
	if len(args) == 0 {
		return renderCommandError("hooks", usageError("missing_argument", "hooks requires install or uninstall"), false)
	}
	command := "hooks." + args[0]
	parsed, parseErr := parseCommandArgs(args[1:], map[string]flagSpec{"tool": {}, "json": {Boolean: true}})
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 || (args[0] != "install" && args[0] != "uninstall") {
		return renderCommandError(command, usageError("unknown_command", "hooks requires install or uninstall"), jsonMode)
	}
	tool := parsed.Values["tool"]
	if tool == "" {
		tool = "claude-code"
	}
	legatoBin, err := os.Executable()
	if err != nil {
		legatoBin = "legato"
	}
	registry := service.NewAdapterRegistry()
	registry.Register(hooks.NewClaudeCodeAdapter(legatoBin))
	registry.Register(hooks.NewStaccatoAdapter(legatoBin))
	registry.Register(hooks.NewChimeraAdapter(legatoBin))
	registry.Register(hooks.NewCodexAdapter(legatoBin))
	registry.Register(hooks.NewYggdrasilAdapter(legatoBin))
	adapter, err := registry.Get(tool)
	if err != nil {
		return renderCommandError(command, &commandError{
			Code: "invalid_tool", Message: err.Error(), Details: map[string]any{"available_tools": registry.List()}, Exit: exitUsage,
		}, jsonMode)
	}
	wd, err := os.Getwd()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "environment_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	if args[0] == "install" {
		err = adapter.InstallHooks(wd)
	} else {
		err = adapter.UninstallHooks(wd)
	}
	if err != nil {
		return renderCommandError(command, &commandError{Code: "hook_operation_failed", Message: err.Error(), Exit: exitDependency}, jsonMode)
	}
	text := fmt.Sprintf("%sed %s hooks in %s", map[bool]string{true: "Install", false: "Uninstall"}[args[0] == "install"], tool, wd)
	return writeCommandSuccess(command, map[string]any{"tool": tool, "directory": wd}, text, jsonMode)
}

func runAuthContract(args []string) int {
	if len(args) == 0 {
		return renderCommandError("auth", usageError("missing_argument", "auth requires token or regenerate"), false)
	}
	command := "auth." + args[0]
	parsed, parseErr := parseCommandArgs(args[1:], map[string]flagSpec{"json": {Boolean: true}})
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 || (args[0] != "token" && args[0] != "regenerate") {
		return renderCommandError(command, usageError("unknown_command", "auth requires token or regenerate"), jsonMode)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	dataDir := resolveDataDir(cfg)
	var token string
	if args[0] == "token" {
		token, err = auth.ReadToken(dataDir)
	} else {
		token, err = auth.RegenerateToken(dataDir)
	}
	if err != nil {
		return renderCommandError(command, &commandError{Code: "auth_token_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"token": token}, token, jsonMode)
}

func runPairContract(args []string) int {
	const command = "pair"
	parsed, parseErr := parseCommandArgs(args, map[string]flagSpec{"port": {}, "json": {Boolean: true}})
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "pair accepts no positional arguments"), jsonMode)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	token, err := auth.ReadToken(resolveDataDir(cfg))
	if err != nil {
		return renderCommandError(command, &commandError{Code: "auth_token_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	port := parsed.Values["port"]
	if port == "" {
		port = cfg.Web.Port
	}
	if port == "" {
		port = "3080"
	}
	host := cfg.Web.TLS.Hostname
	if host == "" {
		host, _ = os.Hostname()
	}
	if host == "" {
		host = "localhost"
	}
	serverURL := fmt.Sprintf("https://%s:%s", host, port)
	pairURI := fmt.Sprintf("legato://pair?url=%s&token=%s", serverURL, token)
	if jsonMode {
		return writeCommandSuccess(command, map[string]any{"url": serverURL, "token": token, "pair_uri": pairURI}, "", true)
	}
	qrterminal.GenerateWithConfig(pairURI, qrterminal.Config{Level: qrterminal.L, Writer: os.Stdout, QuietZone: 1})
	fmt.Fprintf(os.Stdout, "\nServer: %s\nToken:  %s\n", serverURL, token)
	return exitOK
}
