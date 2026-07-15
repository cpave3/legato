package main

import "testing"

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
