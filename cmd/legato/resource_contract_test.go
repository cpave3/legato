package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResourceCommandsAddListAndRemove(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "Daily Notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.yaml")
	configYAML := "db:\n  path: " + filepath.Join(root, "legato.db") + "\n" +
		"resources:\n  - name: Vault\n    paths:\n      - path: /srv/vault\n        access: write\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)

	if code := runCLI([]string{"resource", "add", "Notes", notes, "--write", "--json"}); code != 0 {
		t.Fatalf("add exited %d", code)
	}

	output := captureStdout(t, func() int { return runCLI([]string{"resource", "list", "--json"}) })
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Resources []struct {
				Name       string `json:"name"`
				Configured bool   `json:"configured"`
				Paths      []struct {
					Path   string `json:"path"`
					Access string `json:"access"`
				} `json:"paths"`
			} `json:"resources"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("JSON: %v\n%s", err, output)
	}
	got := envelope.Data.Resources
	if !envelope.OK || len(got) != 2 || got[0].Name != "Vault" || !got[0].Configured || got[1].Name != "Notes" || got[1].Paths[0].Path != notes || got[1].Paths[0].Access != "write" {
		t.Fatalf("list = %s", output)
	}

	if code := runCLI([]string{"resource", "remove", "Vault"}); code == 0 {
		t.Fatal("a configured resource was removed")
	}
	if code := runCLI([]string{"resource", "remove", "notes"}); code != 0 {
		t.Fatalf("remove exited %d", code)
	}
}
