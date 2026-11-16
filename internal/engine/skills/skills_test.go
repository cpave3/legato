package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, name, content string) string {
	t.Helper()
	folder := filepath.Join(dir, name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

func byName(list []Skill) map[string]Skill {
	out := map[string]Skill{}
	for _, s := range list {
		out[s.Name] = s
	}
	return out
}

func TestScanReadsEachSkillsNameAndDescription(t *testing.T) {
	dir := t.TempDir()
	folder := writeSkill(t, dir, "tdd", "---\nname: tdd\ndescription: Test-driven development.\n---\n\n# TDD\n")
	writeSkill(t, dir, "code-review", "---\nname: code-review\ndescription: >-\n  Review a branch\n  in two passes.\n---\nBody\n")

	list, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "code-review" || list[1].Name != "tdd" {
		t.Fatalf("skills = %+v, want code-review then tdd", list)
	}
	tdd := list[1]
	if tdd.Description != "Test-driven development." || tdd.Path != folder || tdd.Err != "" {
		t.Fatalf("tdd = %+v", tdd)
	}
	if list[0].Description != "Review a branch in two passes." {
		t.Fatalf("folded description = %q", list[0].Description)
	}
}

func TestScanReportsABrokenSkillWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "good", "---\nname: good\ndescription: Works.\n---\n")
	writeSkill(t, dir, "no-file", "")
	writeSkill(t, dir, "no-frontmatter", "# Just a heading\n")
	writeSkill(t, dir, "bad-yaml", "---\nname: [unclosed\n---\n")
	writeSkill(t, dir, "no-description", "---\nname: no-description\n---\n")
	writeSkill(t, dir, "other-name", "---\nname: something-else\ndescription: Named apart from its folder.\n---\n")
	writeSkill(t, dir, "Bad_Name", "---\nname: Bad_Name\ndescription: Not a skill name.\n---\n")

	list, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := byName(list)
	if got["good"].Err != "" {
		t.Fatalf("good = %+v", got["good"])
	}
	for name, want := range map[string]string{
		"no-file":        "no SKILL.md",
		"no-frontmatter": "frontmatter",
		"bad-yaml":       "frontmatter",
		"no-description": "description",
		"other-name":     "folder",
		"Bad_Name":       "lowercase",
	} {
		skill, ok := got[name]
		if !ok {
			t.Errorf("%s not listed", name)
			continue
		}
		if !strings.Contains(skill.Err, want) {
			t.Errorf("%s error = %q, want it to mention %q", name, skill.Err, want)
		}
		if skill.OK() {
			t.Errorf("%s reads as usable", name)
		}
	}
}

func TestScanOfAMissingFolderIsEmpty(t *testing.T) {
	list, err := Scan(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(list) != 0 {
		t.Fatalf("Scan = %v, %v; want nothing and no error", list, err)
	}
}

func TestScanSkipsFilesAndHiddenFoldersAndFollowsALinkedSkill(t *testing.T) {
	dir := t.TempDir()
	elsewhere := writeSkill(t, t.TempDir(), "linked", "---\nname: linked\ndescription: From a registry checkout.\n---\n")
	if err := os.Symlink(elsewhere, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, dir, ".git", "---\nname: git\ndescription: x\n---\n")

	list, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "linked" || !list[0].OK() {
		t.Fatalf("skills = %+v, want the linked skill alone", list)
	}
}

func TestScanSeesAddedChangedAndRemovedSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "first", "---\nname: first\ndescription: One.\n---\n")
	if list, _ := Scan(dir); len(list) != 1 {
		t.Fatalf("first scan = %+v", list)
	}
	writeSkill(t, dir, "second", "---\nname: second\ndescription: Two.\n---\n")
	writeSkill(t, dir, "first", "---\nname: first\ndescription: Changed.\n---\n")
	list, _ := Scan(dir)
	if got := byName(list); len(list) != 2 || got["first"].Description != "Changed." {
		t.Fatalf("after adding and changing = %+v", list)
	}
	if err := os.RemoveAll(filepath.Join(dir, "first")); err != nil {
		t.Fatal(err)
	}
	if list, _ := Scan(dir); len(list) != 1 || list[0].Name != "second" {
		t.Fatalf("after removing = %+v", list)
	}
}

func TestCopyTakesTheSkillsFilesAndNothingOutsideIt(t *testing.T) {
	src := writeSkill(t, t.TempDir(), "deploy", "---\nname: deploy\ndescription: Ship it.\n---\n")
	secret := filepath.Join(t.TempDir(), "id_rsa")
	mustWrite := func(path, content string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(src, "scripts", "run.sh"), "#!/bin/sh\necho hi\n", 0o755)
	mustWrite(filepath.Join(src, "references", "api.md"), "API", 0o644)
	mustWrite(secret, "PRIVATE", 0o600)
	for link, target := range map[string]string{
		"leak.txt":      secret,
		"api-link.md":   filepath.Join("references", "api.md"),
		"references/up": "..",
		"escape-dir":    filepath.Dir(secret),
		"dangling-file": "missing.md",
	} {
		if err := os.Symlink(target, filepath.Join(src, link)); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(t.TempDir(), "skills", "deploy")

	if err := Copy(Skill{Name: "deploy", Path: src}, dst); err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		content, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			return "<" + err.Error() + ">"
		}
		return string(content)
	}
	if got := read("SKILL.md"); !strings.Contains(got, "name: deploy") {
		t.Fatalf("SKILL.md = %q", got)
	}
	if got := read("references/api.md"); got != "API" {
		t.Fatalf("references/api.md = %q", got)
	}
	if got := read("api-link.md"); got != "API" {
		t.Fatalf("a link inside the skill = %q, want its file's content", got)
	}
	if info, err := os.Stat(filepath.Join(dst, "scripts", "run.sh")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("run.sh = %v, %v; want it executable", info, err)
	}
	for _, rel := range []string{"leak.txt", "escape-dir", "references/up", "dangling-file"} {
		if _, err := os.Lstat(filepath.Join(dst, rel)); !os.IsNotExist(err) {
			t.Errorf("%s was copied (%v); a link that leaves the skill, or leads to a folder, must not be", rel, err)
		}
	}
}

func TestCopyRefusesASkillLargerThanTheLimit(t *testing.T) {
	src := writeSkill(t, t.TempDir(), "big", "---\nname: big\ndescription: Too big.\n---\n")
	if err := os.WriteFile(filepath.Join(src, "blob.bin"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "big")
	err := copyLimited(Skill{Name: "big", Path: src}, dst, 1024)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("copy = %v, want a size refusal", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("a refused copy left %s behind", dst)
	}
}
