package config

import "testing"

func TestWorkflowRepositoryPresetsParsed(t *testing.T) {
	writeTestConfig(t, `
workflows:
  repositories:
    - name: legato
      repo: cpave3/legato
      primary_dir: /home/cameron/Projects/legato
      base_branch: main
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workflows.Repositories) != 1 || cfg.Workflows.Repositories[0].Repo != "cpave3/legato" || cfg.Workflows.Repositories[0].PrimaryDir == "" {
		t.Fatalf("repositories = %+v", cfg.Workflows.Repositories)
	}
}
