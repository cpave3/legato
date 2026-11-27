package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

const namedAccountSettingsLimit = 1 << 20

// validateNamedAccountAuth checks local settings at launch, not managed policy
// delivered by the OS or Claude's server. launchArgs must include adapter base,
// selected tier and per-spawn arguments. Default authentication is unchanged.
func validateNamedAccountAuth(identity *store.AgentAccountIdentity, workDir string, launchArgs []string) error {
	if identity == nil || identity.Account == "" {
		return nil
	}
	for _, arg := range launchArgs {
		flag, _, _ := strings.Cut(arg, "=")
		switch flag {
		case "--settings", "--profile", "--settings-sources":
			return fmt.Errorf("named Claude subscription conflicts with %s; remove that launch override or use the default account", flag)
		}
	}
	for _, name := range []string{"settings.json", ".claude.json"} {
		if err := inspectNamedAccountSettings(identity.ConfigDir, name); err != nil {
			return err
		}
	}
	dir, err := filepath.Abs(workDir)
	if err != nil {
		return errors.New("named Claude subscription working directory cannot be inspected; select an accessible directory")
	}
	for {
		for _, name := range []string{".claude/settings.json", ".claude/settings.local.json"} {
			if err := inspectNamedAccountSettings(dir, name); err != nil {
				return err
			}
		}
		if err := inspectNamedAccountWorktree(dir); err != nil {
			return err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil
}

func inspectNamedAccountSettings(dir, name string) error {
	refuse := func(reason string) error {
		return fmt.Errorf("named Claude subscription: %s in %s; remove the auth override or repair this settings file before launching", reason, name)
	}
	data, err := readNamedAccountConfig(dir, name)
	if err != nil || data == nil {
		return err
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil || settings == nil {
		return refuse("invalid JSON settings cannot be inspected")
	}
	var env map[string]json.RawMessage
	if raw, ok := settings["env"]; ok {
		if err := json.Unmarshal(raw, &env); err != nil {
			return refuse("invalid env settings cannot be inspected")
		}
	}
	for _, key := range append([]string{"CLAUDE_CONFIG_DIR"}, namedAccountAuthVars...) {
		if raw, ok := env[key]; ok && namedAccountSettingPresent(raw) {
			return refuse("conflicting env." + key)
		}
	}
	for _, key := range []string{"apiKeyHelper", "apiKey", "primaryApiKey", "anthropicApiKey", "profile", "activeProfile", "provider", "awsAuthRefresh", "awsCredentialExport", "forceLoginGatewayUrl", "policyHelper"} {
		if raw, ok := settings[key]; ok && namedAccountSettingPresent(raw) {
			return refuse("conflicting " + key)
		}
	}
	if raw, ok := settings["forceLoginMethod"]; ok && namedAccountSettingPresent(raw) && string(raw) != `"claudeai"` {
		return refuse("conflicting forceLoginMethod")
	}
	return nil
}

func namedAccountSettingPresent(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "null" && value != `""`
}

// Linked worktrees read local settings from the main checkout too. Read only
// Git's location metadata, never run git or repository-controlled commands.
func inspectNamedAccountWorktree(dir string) error {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if errors.Is(err, os.ErrNotExist) || (err == nil && info.IsDir()) {
		return nil
	}
	refuse := errors.New("named Claude subscription: linked worktree settings cannot be inspected; repair its .git/commondir metadata or use the default account")
	if err != nil {
		return refuse
	}
	data, err := readNamedAccountConfig(dir, ".git")
	if err != nil {
		return refuse
	}
	location, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
	if !ok || location == "" {
		return refuse
	}
	if !filepath.IsAbs(location) {
		location = filepath.Join(dir, location)
	}
	common, err := readNamedAccountConfig(location, "commondir")
	if err != nil {
		return refuse
	}
	// A submodule's gitfile has no commondir and does not borrow local settings.
	if common == nil {
		return nil
	}
	path := strings.TrimSpace(string(common))
	if path == "" {
		return refuse
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(location, path)
	}
	path = filepath.Clean(path)
	if filepath.Base(path) != ".git" {
		return refuse
	}
	return inspectNamedAccountSettings(filepath.Dir(path), ".claude/settings.local.json")
}

func readNamedAccountConfig(dir, name string) ([]byte, error) {
	refuse := func(reason string) error {
		return fmt.Errorf("named Claude subscription: %s in %s; repair this file before launching", reason, name)
	}
	root, err := os.OpenRoot(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, refuse("settings directory cannot be inspected")
	}
	defer root.Close()
	// Refuse links before opening, and confine the actual open too, so a link
	// swapped between the two calls still cannot escape the inspected directory.
	parts := strings.Split(filepath.ToSlash(name), "/")
	for i := range parts {
		info, err := root.Lstat(filepath.Join(parts[:i+1]...))
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, refuse("settings cannot be inspected")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, refuse("symlink settings cannot be inspected safely")
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, refuse("settings must be a regular file")
		}
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, refuse("settings cannot be read")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, refuse("settings must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, namedAccountSettingsLimit+1))
	if err != nil {
		return nil, refuse("settings cannot be read")
	}
	if len(data) > namedAccountSettingsLimit {
		return nil, refuse("settings exceed the inspection limit")
	}
	return data, nil
}
