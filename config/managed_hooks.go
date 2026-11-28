package config

import "path/filepath"

// ManagedHooksConfig is where Legato finds the managed hooks it gives
// agents. See docs/ai/managed-hooks.md.
type ManagedHooksConfig struct {
	// Dir is the managed-hooks folder: one folder per hook, each with a
	// HOOK.yaml. <legato home>/managed-hooks (~/.legato/managed-hooks) when
	// empty.
	Dir string `yaml:"dir"`
}

// ResolveManagedHooksDir is the managed-hooks folder: managed_hooks.dir,
// else managed-hooks in Legato's home folder (~/.legato, or $LEGATO_HOME),
// which the caller passes since config reads no other package.
func ResolveManagedHooksDir(cfg *Config, legatoHome string) string {
	if cfg.ManagedHooks.Dir != "" {
		return cfg.ManagedHooks.Dir
	}
	return filepath.Join(legatoHome, "managed-hooks")
}
