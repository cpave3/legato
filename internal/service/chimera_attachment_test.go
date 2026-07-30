package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChimeraAttachments(t *testing.T) {
	home := t.TempDir()
	tokens := parseChimeraAttachments("inspect @docs #src ## Heading [Image #2] [Pasted text #3, 10 lines]", "/repo", home)
	if len(tokens) != 2 {
		t.Fatalf("tokens = %+v", tokens)
	}
	if tokens[0].Kind != "read" || tokens[0].Raw != "docs" || tokens[0].Absolute != "/repo/docs" {
		t.Fatalf("read token = %+v", tokens[0])
	}
	if tokens[1].Kind != "write" || tokens[1].Absolute != "/repo/src" {
		t.Fatalf("write token = %+v", tokens[1])
	}

	homeToken := parseChimeraAttachments("@~/notes", "/repo", home)
	if len(homeToken) != 1 || homeToken[0].Absolute != filepath.Join(home, "notes") {
		t.Fatalf("home token = %+v", homeToken)
	}
}

func TestParseChimeraAttachmentsIgnoresMarkdownPunctuation(t *testing.T) {
	tokens := parseChimeraAttachments("Selected lines (`@@ -1 +1 @@`):** and @`):**", "/repo", t.TempDir())
	if len(tokens) != 0 {
		t.Fatalf("tokens = %+v", tokens)
	}
}

func TestReadChimeraAttachmentFileAndDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("first\nsecond"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}

	body, err := readChimeraAttachment(file)
	if err != nil {
		t.Fatal(err)
	}
	if body != "1\tfirst\n2\tsecond" {
		t.Fatalf("file body = %q", body)
	}

	body, err = readChimeraAttachment(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "notes.txt") || !strings.Contains(body, "subdir/") {
		t.Fatalf("directory body = %q", body)
	}
}

func TestReadChimeraAttachmentRejectsMissingPath(t *testing.T) {
	_, err := readChimeraAttachment(filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v", err)
	}
}
