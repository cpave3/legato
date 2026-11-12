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

func TestRepositoryResourcesNameConfiguredResourcesForARepository(t *testing.T) {
	writeTestConfig(t, `
agents:
  repository_resources:
    - repo: git@github.com:Autopilot-Dev/Autopilot.git
      resources: [nuget feed token, NuGet feed token, Specs]
resources:
  - name: NuGet feed token
    paths: [{path: /creds}]
  - name: Specs
    paths: [{path: /specs}]
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Agents.RepositoryResources
	if len(got) != 1 || got[0].Repo != "github.com/autopilot-dev/autopilot" {
		t.Fatalf("repository resources = %+v", got)
	}
	// Named as the resources block names them, each once.
	if strings.Join(got[0].Resources, "|") != "NuGet feed token|Specs" {
		t.Fatalf("resources = %q", got[0].Resources)
	}
}

func TestRepositoryResourcesThatCannotBeGivenFailToLoad(t *testing.T) {
	resources := "resources:\n  - name: Vault\n    paths: [{path: /a}]\n"
	for _, tc := range []struct{ name, yaml, want string }{
		{"unknown resource", "agents:\n  repository_resources:\n    - repo: acme/web\n      resources: [Missing]\n" + resources, `no resource named "Missing"`},
		{"repository twice", "agents:\n  repository_resources:\n    - repo: acme/web\n      resources: [Vault]\n    - repo: https://github.com/Acme/web.git\n      resources: [Vault]\n" + resources, "already"},
		{"not a repository", "agents:\n  repository_resources:\n    - repo: web\n      resources: [Vault]\n" + resources, "owner/name"},
		{"no resources", "agents:\n  repository_resources:\n    - repo: acme/web\n" + resources, "at least one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeTestConfig(t, tc.yaml)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
