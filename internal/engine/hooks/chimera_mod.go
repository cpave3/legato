package hooks

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed all:chimeramod/legato
var chimeraModFS embed.FS

// InstallChimeraMod writes the embedded mod without MCP tools under root.
// Unchanged files keep their mtimes so a watcher does not reload live sessions.
func InstallChimeraMod(root string) (string, error) {
	return installChimeraMod(root, nil)
}

// InstallChimeraModWithTools bundles Legato's MCP server and its trusted tools.
// The caller supplies names so the engine need not import the CLI registry.
func InstallChimeraModWithTools(root, legatoBin string, trustedTools []string) (string, error) {
	server := struct {
		Transport    string            `json:"transport"`
		Command      string            `json:"command"`
		Args         []string          `json:"args"`
		Env          map[string]string `json:"env"`
		TrustedTools []string          `json:"trustedTools"`
	}{
		Transport: "stdio", Command: legatoBin, Args: []string{"mcp", "serve"},
		// An env map makes Chimera pass its environment to the stdio server.
		// Reference only the required variable: optional unset references
		// would prevent the server from starting.
		Env:          map[string]string{"LEGATO_TASK_ID": "env:LEGATO_TASK_ID"},
		TrustedTools: append([]string{}, trustedTools...),
	}
	mcp, err := json.Marshal(map[string]any{"legato": server})
	if err != nil {
		return "", fmt.Errorf("encoding Chimera MCP server: %w", err)
	}
	return installChimeraMod(root, mcp)
}

func installChimeraMod(root string, mcp json.RawMessage) (string, error) {
	dir := filepath.Join(root, "legato")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("installing the Chimera mod: %w", err)
	}
	for _, name := range []string{"index.mjs", "chimera-mod.json"} {
		content, err := chimeraModFS.ReadFile("chimeramod/legato/" + name)
		if err != nil {
			return "", fmt.Errorf("reading the Chimera mod: %w", err)
		}
		if name == "chimera-mod.json" && mcp != nil {
			var manifest map[string]json.RawMessage
			if err := json.Unmarshal(content, &manifest); err != nil {
				return "", fmt.Errorf("reading the Chimera manifest: %w", err)
			}
			manifest["mcpServers"] = mcp
			content, err = json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				return "", fmt.Errorf("encoding the Chimera manifest: %w", err)
			}
			content = append(content, '\n')
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
