package config

import "testing"

func TestSharedRepositoryPresetsParsed(t *testing.T) {
	writeTestConfig(t, `
repositories:
  - name: legato
    repo: cpave3/legato
    primary_dir: /home/cameron/Projects/legato
    base_branch: main
    jira_projects: [LEG]
    workspaces: [Legato]
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repositories) != 1 || cfg.Repositories[0].Repo != "cpave3/legato" || cfg.Repositories[0].PrimaryDir == "" || cfg.Repositories[0].JiraProjects[0] != "LEG" {
		t.Fatalf("repositories = %+v", cfg.Repositories)
	}
}

func TestLegacyWorkflowRepositoryPresetsMigrateToSharedCatalog(t *testing.T) {
	writeTestConfig(t, `
workflows:
  repositories:
    - name: legato
      repo: cpave3/legato
      primary_dir: /projects/legato
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repositories) != 1 || cfg.Repositories[0].Repo != "cpave3/legato" {
		t.Fatalf("repositories = %+v", cfg.Repositories)
	}
}
