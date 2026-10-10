package hooks

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// claudeModFS holds the Legato mod for Claude Code: a plugin whose hooks
// module runs inside a Claude Code session and connects it to Legato through
// `legato agent bridge`. See claudemod/legato/hooks/register.ts.
//
//go:embed all:claudemod/legato
var claudeModFS embed.FS

const (
	claudeModSource = "claudemod/legato"
	claudeModName   = "legato"
)

// claudeUsageModFS holds the usage mod for Claude Code: a second plugin,
// loaded beside the bridge mod, that reports what each model request of a
// session cost to a Manticore instance. See claudemod/manticore/hooks/register.ts.
//
//go:embed all:claudemod/manticore
var claudeUsageModFS embed.FS

const (
	claudeUsageModSource = "claudemod/manticore"
	claudeUsageModName   = "legato-manticore"
)

// InstallClaudeMod writes the mod under root and returns its plugin
// directory, the path Claude Code loads it from.
//
// A file that already holds the embedded content is left alone: Claude Code
// watches a plugin folder and reloads the module whenever one is saved, which
// would restart the bridge of every running session each time Legato starts.
func InstallClaudeMod(root string) (string, error) {
	pluginDir := filepath.Join(root, claudeModName)
	if err := installEmbeddedMod(claudeModFS, claudeModSource, pluginDir); err != nil {
		return "", fmt.Errorf("installing the Claude Code mod: %w", err)
	}
	return pluginDir, nil
}

// InstallClaudeUsageMod writes the usage mod under root, beside the bridge
// mod, and returns its plugin directory. Like InstallClaudeMod it leaves an
// up-to-date install untouched.
func InstallClaudeUsageMod(root string) (string, error) {
	pluginDir := filepath.Join(root, claudeUsageModName)
	if err := installEmbeddedMod(claudeUsageModFS, claudeUsageModSource, pluginDir); err != nil {
		return "", fmt.Errorf("installing the Claude Code usage mod: %w", err)
	}
	return pluginDir, nil
}

func installEmbeddedMod(embedded embed.FS, source, pluginDir string) error {
	return fs.WalkDir(embedded, source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(path, source), "/")
		// The mod's own tests run under `claude plugin test` in this
		// repository; a session has no use for them.
		if relative == "tests" || strings.HasPrefix(relative, "tests/") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(pluginDir, filepath.FromSlash(relative))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := embedded.ReadFile(path)
		if err != nil {
			return err
		}
		if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, content) {
			return nil
		}
		return os.WriteFile(target, content, 0o644)
	})
}
