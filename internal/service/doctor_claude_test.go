package service

import (
	"context"
	"strings"
	"testing"
)

func TestTheDoctorChecksTheClaudeAgentsLaunch(t *testing.T) {
	const link = "/data/legato/tools/claude/claude"
	m := healthyMachine()
	m.paths[link] = link
	m.answer(link+" --version", "2.1.326 (Claude Code)", nil)
	m.answer(link+" auth status", `{"loggedIn":true}`, nil)
	opts := m.options()
	opts.ToolCommand = func(id string) (string, string) {
		if id == "claude-code" {
			return link, "Legato's own, last known good 2.1.320"
		}
		return "", ""
	}
	d := NewDoctor(opts)
	r := d.Check(context.Background(), DoctorConfig{DefaultAgent: "claude-code"}, false)
	c := findCheck(t, r, "claude-code")
	if c.Result != DoctorOK || !strings.Contains(c.Detail, "2.1.326") || !strings.Contains(c.Detail, "Legato's own, last known good 2.1.320") {
		t.Fatalf("claude-code = %+v", c)
	}
	if !m.ranCommand(link+" auth status") || m.ranCommand("claude --version") {
		t.Fatalf("ran %v", m.ran)
	}
	if err := d.SpawnCheck(context.Background(), "claude-code"); err != nil {
		t.Fatal(err)
	}
}
