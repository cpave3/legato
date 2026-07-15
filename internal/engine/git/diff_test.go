package git

import (
	"context"
	"testing"
)

const simpleDiff = `diff --git a/main.go b/main.go
index 1234567..89abcde 100644
--- a/main.go
+++ b/main.go
@@ -1,4 +1,5 @@
 package main
 
-func old() {}
+func new() {}
+func extra() {}
`

const addedFileDiff = `diff --git a/new.go b/new.go
new file mode 100644
index 0000000..e69de29
--- /dev/null
+++ b/new.go
@@ -0,0 +1,2 @@
+package new
+// fresh file
`

func TestParseUnifiedDiffAddedFile(t *testing.T) {
	files := ParseUnifiedDiff(addedFileDiff)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	f := files[0]
	if f.Status != FileAdded {
		t.Fatalf("Status = %q, want %q", f.Status, FileAdded)
	}
	if f.NewPath != "new.go" {
		t.Fatalf("NewPath = %q", f.NewPath)
	}
	if len(f.Hunks) != 1 || len(f.Hunks[0].Lines) != 2 {
		t.Fatalf("hunks = %+v", f.Hunks)
	}
	if l := f.Hunks[0].Lines[0]; l.Kind != LineAdded || l.NewNo != 1 || l.Text != "package new" {
		t.Fatalf("line 0 = %+v", l)
	}
}

func TestParseUnifiedDiffDeletedFile(t *testing.T) {
	files := ParseUnifiedDiff(`diff --git a/gone.go b/gone.go
deleted file mode 100644
index e69de29..0000000
--- a/gone.go
+++ /dev/null
@@ -1,1 +0,0 @@
-package gone
`)
	if len(files) != 1 || files[0].Status != FileDeleted {
		t.Fatalf("files = %+v, want single deleted file", files)
	}
	if l := files[0].Hunks[0].Lines[0]; l.Kind != LineDeleted || l.OldNo != 1 {
		t.Fatalf("line = %+v", l)
	}
}

func TestParseUnifiedDiffRenamedFile(t *testing.T) {
	files := ParseUnifiedDiff(`diff --git a/old_name.go b/new_name.go
similarity index 95%
rename from old_name.go
rename to new_name.go
index 1234567..89abcde 100644
--- a/old_name.go
+++ b/new_name.go
@@ -1,2 +1,2 @@
 package pkg
-// old comment
+// new comment
`)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d", len(files))
	}
	f := files[0]
	if f.Status != FileRenamed {
		t.Fatalf("Status = %q, want %q", f.Status, FileRenamed)
	}
	if f.OldPath != "old_name.go" || f.NewPath != "new_name.go" {
		t.Fatalf("paths = %q → %q", f.OldPath, f.NewPath)
	}
}

func TestParseUnifiedDiffBinaryFile(t *testing.T) {
	files := ParseUnifiedDiff(`diff --git a/logo.png b/logo.png
new file mode 100644
index 0000000..a1b2c3d
Binary files /dev/null and b/logo.png differ
`)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d", len(files))
	}
	if files[0].Status != FileBinary {
		t.Fatalf("Status = %q, want %q", files[0].Status, FileBinary)
	}
	if len(files[0].Hunks) != 0 {
		t.Fatalf("binary file has hunks: %+v", files[0].Hunks)
	}
}

func TestParseUnifiedDiffSkipsNoNewlineMarker(t *testing.T) {
	files := ParseUnifiedDiff(`diff --git a/x.txt b/x.txt
index 1234567..89abcde 100644
--- a/x.txt
+++ b/x.txt
@@ -1,1 +1,1 @@
-old
\ No newline at end of file
+new
\ No newline at end of file
`)
	lines := files[0].Hunks[0].Lines
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2 (marker lines must be skipped): %+v", len(lines), lines)
	}
	if lines[0].Text != "old" || lines[1].Text != "new" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestParseUnifiedDiffMultipleFilesAndHunks(t *testing.T) {
	files := ParseUnifiedDiff(`diff --git a/first.go b/first.go
index 1111111..2222222 100644
--- a/first.go
+++ b/first.go
@@ -1,2 +1,2 @@
 package first
-// a
+// b
@@ -10,2 +10,2 @@
 func f() {
-	old()
+	new()
diff --git a/second.go b/second.go
index 3333333..4444444 100644
--- a/second.go
+++ b/second.go
@@ -1,1 +1,1 @@
-package old
+package second
`)
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	if len(files[0].Hunks) != 2 {
		t.Fatalf("first file hunks = %d, want 2", len(files[0].Hunks))
	}
	if files[0].Hunks[1].Lines[0].OldNo != 10 {
		t.Fatalf("second hunk line numbering = %+v", files[0].Hunks[1].Lines[0])
	}
	if files[1].NewPath != "second.go" || len(files[1].Hunks) != 1 {
		t.Fatalf("second file = %+v", files[1])
	}
}

func TestParseUnifiedDiffOnRealGitOutput(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "README.md", "hello\nmore\n")
	writeFile(t, dir, "fresh.go", "package fresh\n")

	raw, err := DiffWorktree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	files := ParseUnifiedDiff(raw)
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2:\n%s", len(files), raw)
	}
	byPath := map[string]FileDiff{}
	for _, f := range files {
		byPath[f.NewPath] = f
	}
	if byPath["README.md"].Status != FileModified {
		t.Fatalf("README.md status = %q", byPath["README.md"].Status)
	}
	if byPath["fresh.go"].Status != FileAdded {
		t.Fatalf("fresh.go status = %q (untracked must parse as added)", byPath["fresh.go"].Status)
	}
}

func TestHunkAnchorStableAcrossLineNumberChanges(t *testing.T) {
	original := Hunk{Header: "@@ -1,2 +1,2 @@", Lines: []Line{
		{Kind: LineContext, OldNo: 1, NewNo: 1, Text: "same"},
		{Kind: LineDeleted, OldNo: 2, Text: "old"},
		{Kind: LineAdded, NewNo: 2, Text: "new"},
	}}
	shifted := Hunk{Header: "@@ -101,2 +201,2 @@", Lines: []Line{
		{Kind: LineContext, OldNo: 101, NewNo: 201, Text: "same"},
		{Kind: LineDeleted, OldNo: 102, Text: "old"},
		{Kind: LineAdded, NewNo: 202, Text: "new"},
	}}
	changed := shifted
	changed.Lines = append([]Line(nil), shifted.Lines...)
	changed.Lines[2].Text = "different"

	if got, want := HunkAnchor(original), HunkAnchor(shifted); got != want {
		t.Fatalf("anchor changed with line ranges: %q != %q", got, want)
	}
	if HunkAnchor(original) == HunkAnchor(changed) {
		t.Fatal("anchor must change with hunk content")
	}
}

func TestParseUnifiedDiffPopulatesHunkAnchor(t *testing.T) {
	hunk := ParseUnifiedDiff(simpleDiff)[0].Hunks[0]
	if hunk.Anchor == "" {
		t.Fatal("parsed hunk anchor is empty")
	}
	if hunk.Anchor != HunkAnchor(hunk) {
		t.Fatalf("Anchor = %q, want %q", hunk.Anchor, HunkAnchor(hunk))
	}
}

func TestParseUnifiedDiffSingleFileSingleHunk(t *testing.T) {
	files := ParseUnifiedDiff(simpleDiff)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	f := files[0]
	if f.OldPath != "main.go" || f.NewPath != "main.go" {
		t.Fatalf("paths = %q → %q", f.OldPath, f.NewPath)
	}
	if f.Status != FileModified {
		t.Fatalf("Status = %q, want %q", f.Status, FileModified)
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("len(hunks) = %d, want 1", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.Header != "@@ -1,4 +1,5 @@" {
		t.Fatalf("Header = %q", h.Header)
	}
	wantLines := []Line{
		{Kind: LineContext, OldNo: 1, NewNo: 1, Text: "package main"},
		{Kind: LineContext, OldNo: 2, NewNo: 2, Text: ""},
		{Kind: LineDeleted, OldNo: 3, NewNo: 0, Text: "func old() {}"},
		{Kind: LineAdded, OldNo: 0, NewNo: 3, Text: "func new() {}"},
		{Kind: LineAdded, OldNo: 0, NewNo: 4, Text: "func extra() {}"},
	}
	if len(h.Lines) != len(wantLines) {
		t.Fatalf("len(lines) = %d, want %d: %+v", len(h.Lines), len(wantLines), h.Lines)
	}
	for i, want := range wantLines {
		if h.Lines[i] != want {
			t.Fatalf("line %d = %+v, want %+v", i, h.Lines[i], want)
		}
	}
}
