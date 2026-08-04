package static

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded frontend build output as an fs.FS rooted at dist/.
// Returns nil if the dist directory is empty or doesn't exist.
func DistFS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	return sub
}

// BuildID returns the identity generated with the embedded frontend bundle.
// TUI-only builds retain a stable sentinel so clients do not reload for Go-only changes.
func BuildID() string {
	data, err := distFS.ReadFile("dist/build-id")
	return buildID(data, err)
}

func buildID(data []byte, err error) string {
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "frontend-unavailable"
	}
	return strings.TrimSpace(string(data))
}
