package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SkillsBundleName names the plugin (Claude Code) and the mod (Chimera) that
// carry the operator's Legato-managed skills into a session.
const SkillsBundleName = "legato-skills"

// SkillsBundle makes root/legato-skills a Claude Code plugin whose skills/
// folder the caller fills, and adds it to CLAUDE_CODE_PLUGIN_DIRS. Claude
// Code namespaces a plugin's skills, so each is legato-skills:<name> and
// never takes the place of a repository's or the person's own skill of the
// same name.
func (a *ClaudeCodeAdapter) SkillsBundle(root string, env map[string]string) (string, error) {
	plugin := filepath.Join(root, SkillsBundleName)
	manifest := map[string]any{
		"name":        SkillsBundleName,
		"version":     "1.0.0",
		"description": "The skills the operator keeps in Legato, given to this session by Legato.",
		"author":      map[string]string{"name": "Legato"},
	}
	if err := writeManifest(filepath.Join(plugin, ".claude-plugin", "plugin.json"), manifest); err != nil {
		return "", err
	}
	appendPathList(env, "CLAUDE_CODE_PLUGIN_DIRS", plugin)
	return makeSkillsDir(plugin)
}

// SkillsBundle makes root/legato-skills a Chimera mod that bundles the
// skills/ folder the caller fills, with no module, and adds it to
// CHIMERA_MOD_DIRS, which Chimera loads without enabling and its subagents
// inherit. Chimera calls a mod's skill by its plain name; one of the same
// name in the project's or the person's .chimera/skills wins over it.
func (a *ChimeraAdapter) SkillsBundle(root string, env map[string]string) (string, error) {
	mod := filepath.Join(root, SkillsBundleName)
	manifest := map[string]any{
		"name":       SkillsBundleName,
		"version":    "1.0.0",
		"apiVersion": 1,
		"skills":     "skills",
	}
	if err := writeManifest(filepath.Join(mod, "chimera-mod.json"), manifest); err != nil {
		return "", err
	}
	appendPathList(env, "CHIMERA_MOD_DIRS", mod)
	return makeSkillsDir(mod)
}

func writeManifest(path string, manifest map[string]any) error {
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(content, '\n'), 0o644)
}

func makeSkillsDir(bundle string) (string, error) {
	dir := filepath.Join(bundle, "skills")
	return dir, os.MkdirAll(dir, 0o755)
}

// appendPathList adds dir to a path list in env: the session's value, else
// the one Legato inherited.
func appendPathList(env map[string]string, key, dir string) {
	current, ok := env[key]
	if !ok {
		current = os.Getenv(key)
	}
	if current != "" {
		current += string(os.PathListSeparator)
	}
	env[key] = current + dir
}
