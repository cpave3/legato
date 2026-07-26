package main

import (
	"reflect"
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
