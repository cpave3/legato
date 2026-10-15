package config

import (
	"strings"
	"testing"
)

func TestResourcesAreParsedWithTheirPaths(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	writeTestConfig(t, `
resources:
  - name: Obsidian vault
    paths:
      - path: ~/Documents/My Vault
        access: write
      - path: /srv/templates
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Resources) != 1 || cfg.Resources[0].Name != "Obsidian vault" {
		t.Fatalf("resources = %+v", cfg.Resources)
	}
	paths := cfg.Resources[0].Paths
	if len(paths) != 2 || paths[0] != (ResourcePathConfig{Path: "/home/me/Documents/My Vault", Access: "write"}) || paths[1] != (ResourcePathConfig{Path: "/srv/templates", Access: "read"}) {
		t.Fatalf("paths = %+v", paths)
	}
}

func TestResourcesThatCannotBeUsedFailToLoad(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"blank name", "resources:\n  - name: ' '\n    paths: [{path: /a}]\n", "name"},
		{"duplicate name", "resources:\n  - name: Vault\n    paths: [{path: /a}]\n  - name: vault\n    paths: [{path: /b}]\n", "duplicate"},
		{"no paths", "resources:\n  - name: Vault\n", "path"},
		{"relative path", "resources:\n  - name: Vault\n    paths: [{path: notes}]\n", "absolute"},
		{"unknown access", "resources:\n  - name: Vault\n    paths: [{path: /a, access: admin}]\n", "read or write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeTestConfig(t, tc.yaml)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
