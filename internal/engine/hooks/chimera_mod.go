package hooks

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed all:chimeramod/legato
var chimeraModFS embed.FS

// InstallChimeraMod writes the embedded mod without MCP tools under root.
// Unchanged files keep their mtimes so a watcher does not reload live sessions.
func InstallChimeraMod(root string) (string, error) {
	dir := filepath.Join(root, "legato")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("installing the Chimera mod: %w", err)
	}
	for _, name := range []string{"index.mjs", "chimera-mod.json"} {
		content, err := chimeraModFS.ReadFile("chimeramod/legato/" + name)
		if err != nil {
			return "", fmt.Errorf("reading the Chimera mod: %w", err)
		}

		path := filepath.Join(dir, name)
		if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
			continue
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return "", fmt.Errorf("installing the Chimera mod: %w", err)
		}
	}
	return dir, nil
}
