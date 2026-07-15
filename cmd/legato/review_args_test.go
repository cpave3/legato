package main

import (
	"strings"
	"testing"
)

func TestParseReviewChapterArgs(t *testing.T) {
	positional, flags, listFlags := parseReviewArgs([]string{
		"Auth flow", "Read validation before persistence",
		"--include", "internal/auth.go:1", "--include", "fixtures/C:drive/input.go:2",
		"--risk", "high", "--order", "3",
	})
	got, err := parseReviewChapterArgs(positional, flags, listFlags)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Auth flow" || got.Narration != "Read validation before persistence" || got.Risk != "high" {
		t.Fatalf("args = %+v", got)
	}
	if got.OrderHint == nil || *got.OrderHint != 3 {
		t.Fatalf("OrderHint = %v, want 3", got.OrderHint)
	}
	if len(got.Includes) != 2 || got.Includes[1].FilePath != "fixtures/C:drive/input.go" || got.Includes[1].Hunk != 2 {
		t.Fatalf("Includes = %+v", got.Includes)
	}
}

func TestParseReviewChapterArgsValidatesIncludes(t *testing.T) {
	for _, tc := range []struct {
		include string
		want    string
	}{
		{"", "requires at least one --include"},
		{"main.go", "<path>:<1-based-hunk>"},
		{"main.go:nope", "1-based hunk number"},
		{"main.go:0", "1-based hunk number"},
		{":1", "path cannot be empty"},
	} {
		args := []string{"Title"}
		if tc.include != "" {
			args = append(args, "--include", tc.include)
		}
		positional, flags, listFlags := parseReviewArgs(args)
		_, err := parseReviewChapterArgs(positional, flags, listFlags)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("include %q error = %v, want containing %q", tc.include, err, tc.want)
		}
	}
}

func TestParseReviewAnnotateArgsHunk(t *testing.T) {
	positional, flags, listFlags := parseReviewArgs([]string{"abc123", "check this", "--file", "main.go", "--hunk", "2"})
	got, err := parseReviewAnnotateArgs(positional, flags, listFlags)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA != "abc123" || got.Text != "check this" || len(got.Files) != 1 || got.Files[0] != "main.go" {
		t.Fatalf("args = %+v", got)
	}
	if got.Hunk == nil || *got.Hunk != 2 {
		t.Fatalf("Hunk = %v, want 2", got.Hunk)
	}
}

func TestParseReviewAnnotateArgsRejectsInvalidHunk(t *testing.T) {
	for _, value := range []string{"zero", "0", "-1"} {
		positional, flags, listFlags := parseReviewArgs([]string{"text", "--file", "main.go", "--hunk", value})
		if _, err := parseReviewAnnotateArgs(positional, flags, listFlags); err == nil {
			t.Fatalf("--hunk %q should fail", value)
		}
	}
}

func TestParseReviewAnnotateArgsHunkRequiresExactlyOneFile(t *testing.T) {
	for _, args := range [][]string{
		{"text", "--hunk", "1"},
		{"text", "--file", "a.go", "--file", "b.go", "--hunk", "1"},
	} {
		positional, flags, listFlags := parseReviewArgs(args)
		if _, err := parseReviewAnnotateArgs(positional, flags, listFlags); err == nil {
			t.Fatalf("args %v should fail", args)
		}
	}
}
