package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	parsed, parseErr := parseCommandArgs(args[1:], commandFlagSpecs("hooks", args[0]))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 || (args[0] != "install" && args[0] != "uninstall") {
		return renderCommandError(command, usageError("unknown_command", "hooks requires install or uninstall"), jsonMode)
	}
	toolValue := parsed.Values["tool"]
	if toolValue == "" {
		return renderCommandError(command, usageError("missing_flag", "hooks "+args[0]+" requires --tool <name[,name...]>"), jsonMode)
	}
	tools, err := parseHookTools(toolValue)
	if err != nil {
		return renderCommandError(command, usageError("invalid_tool", err.Error()), jsonMode)
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
	adapters := make([]service.AIToolAdapter, 0, len(tools))
	for _, tool := range tools {
		adapter, getErr := registry.Get(tool)
		if getErr != nil {
			return renderCommandError(command, &commandError{
				Code: "invalid_tool", Message: getErr.Error(), Details: map[string]any{"available_tools": registry.List()}, Exit: exitUsage,
			}, jsonMode)
		}
		adapters = append(adapters, adapter)
	}
	wd, err := os.Getwd()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "environment_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	for index, adapter := range adapters {
		if args[0] == "install" {
			err = adapter.InstallHooks(wd)
		} else {
			err = adapter.UninstallHooks(wd)
		}
		if err != nil {
			return renderCommandError(command, &commandError{
				Code: "hook_operation_failed", Message: fmt.Sprintf("%s: %v", tools[index], err), Details: map[string]any{"tool": tools[index]}, Exit: exitDependency,
			}, jsonMode)
		}
	}
	text := fmt.Sprintf("%sed %s hooks in %s", map[bool]string{true: "Install", false: "Uninstall"}[args[0] == "install"], strings.Join(tools, ", "), wd)
	data := map[string]any{"tools": tools, "directory": wd}
	if len(tools) == 1 {
		data["tool"] = tools[0]
	}
	return writeCommandSuccess(command, data, text, jsonMode)
}

func parseHookTools(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	tools := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		tool := strings.TrimSpace(part)
		if tool == "" {
			return nil, fmt.Errorf("--tool contains an empty tool name")
		}
		if !seen[tool] {
			seen[tool] = true
			tools = append(tools, tool)
		}
	}
	if len(tools) == 0 {
		return nil, fmt.Errorf("--tool requires at least one tool name")
	}
	return tools, nil
}

func runAuthContract(args []string) int {
	if len(args) == 0 {
		return renderCommandError("auth", usageError("missing_argument", "auth requires token or regenerate"), false)
	}
	command := "auth." + args[0]
	parsed, parseErr := parseCommandArgs(args[1:], commandFlagSpecs("auth", args[0]))
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
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("pair"))
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
	host := parsed.Values["host"]
	if host == "" {
		host = cfg.Web.TLS.Hostname
	}
	if host == "" {
		host, _ = os.Hostname()
	}
	if host == "" {
		host = "localhost"
	}
	scheme := "http"
	if tlsEnabled(cfg) {
		scheme = "https"
	}
	serverURL := scheme + "://" + net.JoinHostPort(host, port)
	values := url.Values{"url": {serverURL}, "token": {token}}
	if scheme == "https" {
		caCertPath := filepath.Join(resolveDataDir(cfg), "certs", "ca.pem")
		if caPEM, readErr := os.ReadFile(caCertPath); readErr == nil {
			values.Set("ca", string(caPEM))
		}
	}
	pairURI := "legato://pair?" + values.Encode()
	if jsonMode {
		return writeCommandSuccess(command, map[string]any{"url": serverURL, "token": token, "pair_uri": pairURI}, "", true)
	}
	qrterminal.GenerateWithConfig(pairURI, qrterminal.Config{Level: qrterminal.L, Writer: os.Stdout, QuietZone: 1})
	fmt.Fprintf(os.Stdout, "\nServer: %s\nToken:  %s\n", serverURL, token)
	return exitOK
}
