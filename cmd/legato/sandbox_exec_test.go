package main

import "testing"

func TestSandboxExecIsTheLaunchersOwnEntryPoint(t *testing.T) {
	if !isSandboxExec([]string{"sandbox", "exec", "/run/x/seatbelt.json", "--", "claude"}) {
		t.Error("the launch line is not the launcher")
	}
	for _, args := range [][]string{{"sandbox"}, {"task", "show"}, {}} {
		if isSandboxExec(args) {
			t.Errorf("%q taken for the launcher", args)
		}
	}
	// A line without its command never starts anything.
	if code := sandboxExec([]string{"/run/x/seatbelt.json"}); code != exitUsage {
		t.Errorf("exit %d", code)
	}
	if code := sandboxExec([]string{"/nonexistent/seatbelt.json", "--", "true"}); code != 1 {
		t.Errorf("exit %d for a spec that does not exist", code)
	}
}
