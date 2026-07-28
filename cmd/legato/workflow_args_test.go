package main

import "testing"

func TestParseWorkflowArgsAcceptsForceFlag(t *testing.T) {
	positionals, flags, _, err := parseWorkflowArgs([]string{"wr-1", "cleanup", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if len(positionals) != 2 || !flagPresent(flags, "force") {
		t.Fatalf("positionals=%v flags=%v", positionals, flags)
	}
}
