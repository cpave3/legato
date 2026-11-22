package workflow_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func definitionYAML(name, title string) string {
	return "# kept\nschema_version: 1\nname: " + name + "\ntitle: " + title + "\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with: {prompt: hi} # and this\n"
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFoldersInPrecedenceOrder(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	folders := workflow.Folders(filepath.Join(root, "legacy"), filepath.Join(root, "global"), []workflow.RepositoryPreset{
		{Name: "api", PrimaryDir: repo}, {Name: "none"}, {Name: "same", PrimaryDir: root},
	}, filepath.Join(root, ".legato", "workflows"))
	var ids []string
	for _, folder := range folders {
		ids = append(ids, folder.ID)
	}
	// The "same" repository's folder is the project's, read once, as the
	// project.
	want := []string{"legacy", "global", "repo:api", "project"}
	if len(ids) != len(want) {
		t.Fatalf("folders = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("folders = %v, want %v", ids, want)
		}
	}
	if folders[2].Path != filepath.Join(repo, ".legato", "workflows") || !folders[0].Legacy {
		t.Fatalf("folders = %+v", folders)
	}
}

func TestDiscoverFoldersReadsRepositoryFoldersAndNotesShadows(t *testing.T) {
	root := t.TempDir()
	global, repo, project := filepath.Join(root, "global"), filepath.Join(root, "repo"), filepath.Join(root, "project")
	writeFile(t, global, "review.yaml", definitionYAML("review", "Global"))
	repoFile := writeFile(t, filepath.Join(repo, ".legato", "workflows"), "review.yaml", definitionYAML("review", "Repository"))
	writeFile(t, filepath.Join(repo, ".legato", "workflows"), "only.yaml", definitionYAML("only", "Only here"))
	folders := workflow.Folders("", global, []workflow.RepositoryPreset{{Name: "api", PrimaryDir: repo}}, project)
	result := workflow.DiscoverFolders(folders)
	titles := map[string]string{}
	for _, definition := range result.Definitions {
		titles[definition.Name] = definition.Title
	}
	if titles["review"] != "Repository" || titles["only"] != "Only here" {
		t.Fatalf("titles = %v", titles)
	}
	if shadows := result.Shadows["review"]; len(shadows) != 1 || shadows[0] != filepath.Join(global, "review.yaml") {
		t.Fatalf("shadows = %v", result.Shadows)
	}
	info := workflow.FileFor(result.Definitions[1], folders)
	if result.Definitions[1].Name != "review" || info.Path != repoFile || info.Folder != "repo:api" || !info.Editable {
		t.Fatalf("file = %+v", info)
	}
}

func TestFileForABuiltInOrALinkIsNotEditable(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "global")
	folders := workflow.Folders("", global, nil, "")
	if info := workflow.FileFor(&workflow.Definition{Name: "legato-job", Source: "builtin:legato-job"}, folders); info.Editable || info.Reason == "" {
		t.Fatalf("built-in = %+v", info)
	}
	outside := writeFile(t, filepath.Join(root, "elsewhere"), "x.yaml", definitionYAML("x", "X"))
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(global, "x.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if info := workflow.FileFor(&workflow.Definition{Name: "x", Source: link}, folders); info.Editable || info.Reason == "" {
		t.Fatalf("a link out of its folder = %+v", info)
	}
	if info := workflow.FileFor(&workflow.Definition{Name: "x", Source: outside}, folders); info.Editable {
		t.Fatalf("a file in no folder = %+v", info)
	}
}

func TestCreateFileNeverOverwrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new")
	data := []byte(definitionYAML("review", "Review"))
	path, err := workflow.CreateFile(dir, "review", data)
	if err != nil || path != filepath.Join(dir, "review.workflow.yaml") {
		t.Fatalf("CreateFile = %q, %v", path, err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(data) {
		t.Fatalf("file = %q", got)
	}
	if _, err := workflow.CreateFile(dir, "review", data); !errors.Is(err, workflow.ErrWorkflowNameTaken) {
		t.Fatalf("again: %v", err)
	}
	if _, err := workflow.CreateFile(dir, "../escape", data); err == nil {
		t.Fatal("a name with a slash was written")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestReplaceFileChecksTheDigest(t *testing.T) {
	path := writeFile(t, t.TempDir(), "review.yaml", definitionYAML("review", "Review"))
	before, _ := os.ReadFile(path)
	edited := []byte(definitionYAML("review", "Edited"))
	if err := workflow.ReplaceFile(path, edited, "sha256:stale"); !errors.Is(err, workflow.ErrStaleFile) {
		t.Fatalf("stale: %v", err)
	}
	if err := workflow.ReplaceFile(path, edited, workflow.FileDigest(before)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(edited) {
		t.Fatalf("file = %q", got)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("left behind: %v", entries)
	}
}
