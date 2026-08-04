package static

import (
	"errors"
	"testing"
)

func TestBuildIDParsesFrontendArtifact(t *testing.T) {
	if got := buildID([]byte("build-123\n"), nil); got != "build-123" {
		t.Fatalf("buildID() = %q", got)
	}
}

func TestBuildIDFallsBackForTUIOnlyBuild(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
		err  error
	}{
		{name: "missing", err: errors.New("missing")},
		{name: "empty", data: []byte(" \n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := buildID(test.data, test.err); got != "frontend-unavailable" {
				t.Fatalf("buildID() = %q", got)
			}
		})
	}
}
