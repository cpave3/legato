// Package themes provides the stable application IDs from the canonical palette asset.
package themes

import (
	_ "embed"
	"encoding/json"
	"slices"
)

//go:embed palettes.json
var paletteJSON []byte

var ids = func() []string {
	var source struct {
		Themes []struct {
			ID string `json:"id"`
		} `json:"themes"`
	}
	if err := json.Unmarshal(paletteJSON, &source); err != nil {
		panic(err)
	}
	result := []string{"legato"}
	for _, theme := range source.Themes {
		result = append(result, theme.ID)
	}
	return result
}()

// IDs returns stable choices, including the unchanged Legato default.
func IDs() []string { return slices.Clone(ids) }

// ValidID excludes the empty/no-preference value, which is not an app theme.
func ValidID(id string) bool { return slices.Contains(ids, id) }
