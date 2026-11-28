package managedhooks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeHook(t *testing.T, dir, name, manifest string) string {
	t.Helper()
	folder := filepath.Join(dir, name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(folder, FileName), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

func byName(list []Hook) map[string]Hook {
	out := map[string]Hook{}
	for _, h := range list {
		out[h.Name] = h
	}
	return out
}

const codeeval = `name: codeeval
description: CodeEval net-debt gate at the end of each turn
agent_tools: [claude-code, chimera]
read_only: false
resources: [codeeval, {name: code-eval.bin, as: CODEEVAL_BIN}]
yields_to:
  claude_code_command: 'codeeval\s+check'
  chimera_hook: '*codeeval*'
events:
  stop:
    command: ./stop-check.sh
    timeout: 120
  pre_tool:
    match: [shell, file_edit, "mcp:github"]
    command: ./guard.sh
`

func TestScanReadsAHooksManifest(t *testing.T) {
	dir := t.TempDir()
	folder := writeHook(t, dir, "codeeval", codeeval)

	list, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("hooks = %+v, want one", list)
	}
	h := list[0]
	if !h.OK() {
		t.Fatalf("codeeval has an error: %s", h.Err)
	}
	if h.Name != "codeeval" || h.Path != folder || h.Description != "CodeEval net-debt gate at the end of each turn" || h.ReadOnly {
		t.Fatalf("hook = %+v", h)
	}
	if !reflect.DeepEqual(h.AgentTools, []string{"claude-code", "chimera"}) {
		t.Fatalf("agent tools = %v", h.AgentTools)
	}
	if want := []Resource{{Name: "codeeval", As: "CODEEVAL"}, {Name: "code-eval.bin", As: "CODEEVAL_BIN"}}; !reflect.DeepEqual(h.Resources, want) {
		t.Fatalf("resources = %+v, want %+v", h.Resources, want)
	}
	if !reflect.DeepEqual(h.EventNames(), []Event{PreTool, Stop}) {
		t.Fatalf("events = %v", h.EventNames())
	}
	stop := h.Events[Stop]
	if stop.Command != "./stop-check.sh" || stop.Timeout != 120 || len(stop.Match) != 0 {
		t.Fatalf("stop = %+v", stop)
	}
	pre := h.Events[PreTool]
	if pre.Timeout != 10 || !reflect.DeepEqual(pre.Match, []string{"shell", "file_edit", "mcp:github"}) {
		t.Fatalf("pre_tool = %+v, want the default 10 s and its matchers", pre)
	}
	if h.YieldsTo.ClaudeCodeCommand == nil || !h.YieldsTo.ClaudeCodeCommand.MatchString("codeeval  check --x") {
		t.Fatalf("yields_to.claude_code_command = %v", h.YieldsTo.ClaudeCodeCommand)
	}
	if !h.YieldsTo.MatchesChimeraHook("01-codeeval.sh") || h.YieldsTo.MatchesChimeraHook("lint.sh") {
		t.Fatalf("yields_to.chimera_hook %q matches wrongly", h.YieldsTo.ChimeraHook)
	}
}

func TestEachEventHasItsDefaultTimeLimit(t *testing.T) {
	dir := t.TempDir()
	writeHook(t, dir, "all", `name: all
description: Every event.
events:
  session_start: {command: ./a}
  stop: {command: ./b}
  pre_tool: {command: ./c}
  post_tool: {command: ./d, match: [web]}
`)
	list, _ := Scan(dir)
	if len(list) != 1 || !list[0].OK() {
		t.Fatalf("hooks = %+v", list)
	}
	got := map[Event]int{}
	for event, handler := range list[0].Events {
		got[event] = handler.Timeout
	}
	want := map[Event]int{SessionStart: 10, Stop: 120, PreTool: 10, PostTool: 30}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("timeouts = %v, want %v", got, want)
	}
	if len(list[0].AgentTools) != 0 || list[0].Resources != nil {
		t.Fatalf("a manifest without agent_tools or resources = %+v", list[0])
	}
}

func TestATimeLimitAtItsMaximumIsAllowed(t *testing.T) {
	dir := t.TempDir()
	writeHook(t, dir, "max", "name: max\ndescription: x\nevents:\n  session_start: {command: ./a, timeout: 30}\n  pre_tool: {command: ./b, timeout: 30}\n  post_tool: {command: ./c, timeout: 120}\n  stop: {command: ./d, timeout: 600}\n")
	if list, _ := Scan(dir); len(list) != 1 || !list[0].OK() {
		t.Fatalf("hooks = %+v", list)
	}
}

func TestEveryManifestMistakeIsListedWithItsErrorBesideValidHooks(t *testing.T) {
	dir := t.TempDir()
	writeHook(t, dir, "good", "name: good\ndescription: Fine.\nevents:\n  stop: {command: ./stop.sh}\n")
	const head = "description: x\n"
	cases := map[string]struct{ manifest, want string }{
		"mismatch":      {"name: other\n" + head + "events:\n  stop: {command: ./a}\n", "not the folder's name"},
		"no-name":       {head + "events:\n  stop: {command: ./a}\n", "no name"},
		"no-desc":       {"name: no-desc\nevents:\n  stop: {command: ./a}\n", "no description"},
		"no-events":     {"name: no-events\n" + head, "no events"},
		"bad-event":     {"name: bad-event\n" + head + "events:\n  user_prompt: {command: ./a}\n", `unknown event "user_prompt"`},
		"stop-match":    {"name: stop-match\n" + head + "events:\n  stop: {command: ./a, match: [shell]}\n", "stop: match"},
		"bad-match":     {"name: bad-match\n" + head + "events:\n  pre_tool: {command: ./a, match: [bash]}\n", `unknown matcher "bash"`},
		"empty-mcp":     {"name: empty-mcp\n" + head + "events:\n  pre_tool: {command: ./a, match: [\"mcp:\"]}\n", `unknown matcher "mcp:"`},
		"no-command":    {"name: no-command\n" + head + "events:\n  stop: {timeout: 5}\n", "stop: no command"},
		"slow":          {"name: slow\n" + head + "events:\n  stop: {command: ./a, timeout: 601}\n", "at most 600"},
		"slow-pre":      {"name: slow-pre\n" + head + "events:\n  pre_tool: {command: ./a, timeout: 31}\n", "at most 30"},
		"zero-time":     {"name: zero-time\n" + head + "events:\n  stop: {command: ./a, timeout: -1}\n", "timeout"},
		"bad-re2":       {"name: bad-re2\n" + head + "yields_to: {claude_code_command: 'codeeval(('}\nevents:\n  stop: {command: ./a}\n", "claude_code_command"},
		"bad-glob":      {"name: bad-glob\n" + head + "yields_to: {chimera_hook: '[codeeval'}\nevents:\n  stop: {command: ./a}\n", "chimera_hook"},
		"bad-resource":  {"name: bad-resource\n" + head + "resources: [{as: X}]\nevents:\n  stop: {command: ./a}\n", "resources"},
		"bad-res-name":  {"name: bad-res-name\n" + head + "resources: [code-eval]\nevents:\n  stop: {command: ./a}\n", "give it an as"},
		"bad-res-as":    {"name: bad-res-as\n" + head + "resources: [{name: x, as: lower-case}]\nevents:\n  stop: {command: ./a}\n", "as"},
		"dup-resource":  {"name: dup-resource\n" + head + "resources: [codeeval, {name: other, as: CODEEVAL}]\nevents:\n  stop: {command: ./a}\n", "twice"},
		"bad-tool":      {"name: bad-tool\n" + head + "agent_tools: [claude-code, cursor]\nevents:\n  stop: {command: ./a}\n", `unknown agent tool "cursor"`},
		"unknown-field": {"name: unknown-field\n" + head + "events:\n  stop: {command: ./a, timout: 5}\n", "timout"},
		"not-yaml":      {"name: [\n", "HOOK.yaml"},
		"no-manifest":   {"", "no HOOK.yaml"},
		"Upper":         {"name: Upper\n" + head + "events:\n  stop: {command: ./a}\n", "lowercase"},
	}
	for name, tc := range cases {
		writeHook(t, dir, name, tc.manifest)
	}

	list, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v; a scan never fails over a hook", err)
	}
	got := byName(list)
	if good := got["good"]; !good.OK() || good.Events[Stop].Command != "./stop.sh" {
		t.Fatalf("good = %+v, want it clean beside the broken ones", good)
	}
	for name, tc := range cases {
		hook, ok := got[name]
		if !ok {
			t.Errorf("%s not listed", name)
			continue
		}
		if hook.OK() || !strings.Contains(hook.Err, tc.want) {
			t.Errorf("%s error = %q, want it to mention %q", name, hook.Err, tc.want)
		}
		if hook.Path != filepath.Join(dir, name) {
			t.Errorf("%s path = %q", name, hook.Path)
		}
	}
}

func TestScanOfAMissingOrEmptyFolderHoldsNoHooks(t *testing.T) {
	for _, dir := range []string{filepath.Join(t.TempDir(), "absent"), t.TempDir()} {
		list, err := Scan(dir)
		if err != nil || len(list) != 0 {
			t.Fatalf("Scan(%s) = %v, %v; want nothing and no error", dir, list, err)
		}
	}
}

func TestScanFollowsALinkedHookAndSkipsFilesAndHiddenFolders(t *testing.T) {
	dir := t.TempDir()
	elsewhere := writeHook(t, t.TempDir(), "legato-hook", "name: codeeval\ndescription: From CodeEval's release.\nevents:\n  stop: {command: ./stop.sh}\n")
	if err := os.Symlink(elsewhere, filepath.Join(dir, "codeeval")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeHook(t, dir, ".git", "name: git\n")

	list, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "codeeval" || !list[0].OK() || list[0].Path != filepath.Join(dir, "codeeval") {
		t.Fatalf("hooks = %+v, want the linked hook alone, named by its link", list)
	}
}

func TestScanSeesAHookChangedOnDisk(t *testing.T) {
	dir := t.TempDir()
	writeHook(t, dir, "gate", "name: gate\ndescription: One.\nevents:\n  stop: {command: ./a}\n")
	if list, _ := Scan(dir); len(list) != 1 || list[0].Description != "One." {
		t.Fatalf("first scan = %+v", list)
	}
	writeHook(t, dir, "gate", "name: gate\ndescription: Two.\nevents:\n  stop: {command: ./a}\n")
	if list, _ := Scan(dir); len(list) != 1 || list[0].Description != "Two." {
		t.Fatalf("after the change = %+v", list)
	}
}
