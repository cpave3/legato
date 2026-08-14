package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseHookTools(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    []string
		wantErr bool
	}{
		{name: "single", value: "codex", want: []string{"codex"}},
		{name: "multiple", value: "claude-code,codex,chimera", want: []string{"claude-code", "codex", "chimera"}},
		{name: "trim and deduplicate", value: " codex, chimera,codex ", want: []string{"codex", "chimera"}},
		{name: "empty", value: "", wantErr: true},
		{name: "empty member", value: "codex,,chimera", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseHookTools(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatal("parseHookTools() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseHookTools() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseHookTools() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestChimeraPermissionResolvedActivity(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
		wantErr bool
	}{
		{name: "final permission resumes work", payload: `{"session_status":"running","remaining_pending":0}`, want: "working"},
		{name: "another permission remains", payload: `{"session_status":"waiting_for_permission","remaining_pending":1}`, want: "waiting"},
		{name: "contradictory running payload", payload: `{"session_status":"running","remaining_pending":1}`, wantErr: true},
		{name: "missing count", payload: `{"session_status":"running"}`, wantErr: true},
		{name: "malformed JSON", payload: `{`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := chimeraPermissionResolvedActivity(strings.NewReader(test.payload))
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("activity = %q, want %q", got, test.want)
			}
		})
	}
}
