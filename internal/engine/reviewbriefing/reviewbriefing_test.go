package reviewbriefing

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindListsTheBriefingABranchCommits(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "switch", "-c", "feat/login")
	writeFile(t, dir, ".review/feat-login/index.html", "<p>briefing</p>")
	commitAll(t, dir, "add briefing")

	index, err := Find(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if index.Branch != "feat/login" || index.Base != "main" {
		t.Fatalf("branch %q base %q", index.Branch, index.Base)
	}
	if got := pagePaths(index); strings.Join(got, ",") != ".review/feat-login/index.html" {
		t.Fatalf("pages = %v", got)
	}
	if index.Pages[0].Label != "Overview" {
		t.Fatalf("label = %q", index.Pages[0].Label)
	}
}

func TestFindCountsOnlyWhatTheBranchChangesCommittedOrNot(t *testing.T) {
	dir := initRepo(t)
	// Left on main by an earlier pull request.
	writeFile(t, dir, ".review/old-pr/index.html", "<p>old</p>")
	writeFile(t, dir, ".review/old-pr/gone.html", "<p>old</p>")
	commitAll(t, dir, "old briefing")
	git(t, dir, "switch", "-c", "feat/login")
	writeFile(t, dir, ".review/feat-login/index.html", "<p>committed</p>")
	commitAll(t, dir, "briefing")
	writeFile(t, dir, ".review/feat-login/index.html", "<p>rebuilt, not committed</p>")
	writeFile(t, dir, ".review/feat-login/risks.html", "<p>untracked</p>")
	if err := os.Remove(filepath.Join(dir, ".review/old-pr/gone.html")); err != nil {
		t.Fatal(err)
	}

	index, err := Find(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(pagePaths(index), ","); got != ".review/feat-login/index.html,.review/feat-login/risks.html" {
		t.Fatalf("pages = %s", got)
	}
}

func TestFindOrdersPagesAsTheExtensionDoes(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "switch", "-c", "feat/login")
	writeFile(t, dir, ".review/another/index.html", "x")
	writeFile(t, dir, ".review/feat-login/risk_notes.html", "x")
	writeFile(t, dir, ".review/feat-login/index.html", "x")
	writeFile(t, dir, ".review/feat-login/deep/data-flow.htm", "x")
	writeFile(t, dir, ".review/feat-login/style.css", "x")
	commitAll(t, dir, "briefings")

	index, err := Find(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, page := range index.Pages {
		got = append(got, page.Folder+"|"+page.Label+"|"+page.Path)
	}
	want := []string{
		// The branch's own folder first, its index.html first.
		"feat-login|Overview|.review/feat-login/index.html",
		"feat-login|Deep/Data flow|.review/feat-login/deep/data-flow.htm",
		"feat-login|Risk notes|.review/feat-login/risk_notes.html",
		"another|Overview|.review/another/index.html",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("pages:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(index.Files) != 5 {
		t.Fatalf("files = %v", index.Files)
	}
}

func TestRenderInlinesTheBriefingsOwnFiles(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, ".review/shared.css", "old {}") // left by another PR
	commitAll(t, dir, "old")
	git(t, dir, "switch", "-c", "feat/login")
	writeFile(t, dir, ".review/feat-login/style.css", "body { color: red }")
	writeFile(t, dir, ".review/feat-login/img/flow.png", "PNGDATA")
	writeFile(t, dir, ".review/feat-login/risks.html", "x")
	writeFile(t, dir, ".review/feat-login/index.html", `<link rel="stylesheet" href="style.css">`+
		`<img src='img/flow.png'><link href="../shared.css"><a href="risks.html">r</a>`+
		`<script src="https://cdn.jsdelivr.net/npm/x.js"></script><a href="#top">t</a><img src="../../README.md">`)
	commitAll(t, dir, "briefing")

	index, err := Find(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	html, err := Render(context.Background(), index, ".review/feat-login/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="data:text/css;base64,Ym9keSB7IGNvbG9yOiByZWQgfQ=="`,
		`src="data:image/png;base64,UE5HREFUQQ=="`,
		// Not this branch's file, another page, the web, an anchor, and a file
		// outside .review/ are left as they are.
		`href="../shared.css"`, `href="risks.html"`, `src="https://cdn.jsdelivr.net/npm/x.js"`, `href="#top"`, `src="../../README.md"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered page lacks %s:\n%s", want, html)
		}
	}
}

func TestRenderRefusesAPathThatIsNotAPageOfTheBriefing(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "switch", "-c", "feat/login")
	writeFile(t, dir, ".review/feat-login/index.html", "x")
	writeFile(t, dir, ".review/feat-login/style.css", "x")
	commitAll(t, dir, "briefing")
	index, err := Find(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"README.md", ".review/feat-login/style.css", ".review/feat-login/../../README.md"} {
		if _, err := Render(context.Background(), index, path); !errors.Is(err, ErrUnknownPage) {
			t.Errorf("Render(%q) error = %v, want ErrUnknownPage", path, err)
		}
	}
}

func TestRenderDoesNotFollowALinkOutOfReview(t *testing.T) {
	dir := initRepo(t)
	secret := filepath.Join(t.TempDir(), "id_rsa")
	writeFile(t, filepath.Dir(secret), "id_rsa", "SECRET")
	git(t, dir, "switch", "-c", "feat/login")
	writeFile(t, dir, ".review/feat-login/index.html", `<img src="key.png">`)
	if err := os.Symlink(secret, filepath.Join(dir, ".review/feat-login/key.png")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "briefing")
	index, err := Find(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	html, err := Render(context.Background(), index, ".review/feat-login/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if html != `<img src="key.png">` {
		t.Fatalf("rendered %q", html)
	}
}

func TestAPagesVersionChangesWhenItIsRebuilt(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "switch", "-c", "feat/login")
	writeFile(t, dir, ".review/feat-login/index.html", "first build")
	before, err := Find(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".review/feat-login/index.html", "second, longer build")
	after, err := Find(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if before.Pages[0].Version == "" || before.Pages[0].Version == after.Pages[0].Version {
		t.Fatalf("versions %q then %q", before.Pages[0].Version, after.Pages[0].Version)
	}
}

func TestFindMeasuresAgainstTheRemotesDefaultBranch(t *testing.T) {
	upstream := initRepo(t)
	git(t, upstream, "branch", "-m", "main", "trunk")
	clone := filepath.Join(t.TempDir(), "clone")
	git(t, upstream, "clone", upstream, clone)
	git(t, clone, "config", "user.email", "test@example.com")
	git(t, clone, "config", "user.name", "Test")
	git(t, clone, "switch", "-c", "feat/login")
	writeFile(t, clone, ".review/feat-login/index.html", "x")

	index, err := Find(context.Background(), clone)
	if err != nil {
		t.Fatal(err)
	}
	if index.Base != "origin/trunk" || len(index.Pages) != 1 {
		t.Fatalf("base %q pages %v", index.Base, pagePaths(index))
	}
}

func TestFindSaysWhenTheDirectoryIsNoCheckout(t *testing.T) {
	if _, err := Find(context.Background(), t.TempDir()); !errors.Is(err, ErrNoCheckout) {
		t.Fatalf("error = %v", err)
	}
}

func pagePaths(index *Index) []string {
	paths := make([]string, 0, len(index.Pages))
	for _, page := range index.Pages {
		paths = append(paths, page.Path)
	}
	return paths
}

// initRepo creates a repository on branch main with one commit.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	writeFile(t, dir, "README.md", "hello\n")
	commitAll(t, dir, "initial commit")
	return dir
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", message)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
