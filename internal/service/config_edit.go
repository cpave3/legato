package service

import (
	"fmt"
	"os"
	"strings"

	"github.com/cpave3/legato/config"
	"gopkg.in/yaml.v3"
)

// ConfigEdit is one edit to config.yaml: a key given a value, or removed.
type ConfigEdit struct {
	Key    string `json:"key"`
	Value  any    `json:"value,omitempty"`
	Remove bool   `json:"remove,omitempty"`
}

// applyConfigEdits applies edits to a parsed config.yaml.
func applyConfigEdits(edits []ConfigEdit) func(doc *yaml.Node) error {
	return func(doc *yaml.Node) error {
		for _, e := range edits {
			key := strings.TrimSpace(e.Key)
			if key == "" {
				return fmt.Errorf("an edit needs a key")
			}
			if e.Remove {
				config.RemoveKey(doc, key)
				continue
			}
			if err := config.SetKey(doc, key, e.Value); err != nil {
				return err
			}
		}
		return nil
	}
}

// CheckConfigEdit applies edits to the config.yaml at path in memory and
// loads the result as Legato would: its diff, and the error that stops it
// loading ("" when it loads).
func CheckConfigEdit(path string, edits []ConfigEdit) (diff string, loadErr string, err error) {
	old, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", "", readErr
	}
	edited, err := config.EditBytes(old, applyConfigEdits(edits))
	if err != nil {
		return "", "", err
	}
	if _, err := config.LoadBytes(edited); err != nil {
		loadErr = err.Error()
	}
	return lineDiff(string(old), string(edited)), loadErr, nil
}

// WriteConfigEdit writes edits to the config.yaml at path, keeping its
// comments and order, with a timestamped backup beside it.
func WriteConfigEdit(path string, edits []ConfigEdit) (backup string, err error) {
	return config.EditFile(path, applyConfigEdits(edits))
}

// lineDiff is a line diff of a and b: "- " for a line removed, "+ " for
// one added, "  " for one kept.
func lineDiff(a, b string) string {
	x, y := splitLines(a), splitLines(b)
	if len(x)*len(y) > maxDiffWork {
		// Too much to match line by line: the old lines out, the new in.
		var out strings.Builder
		for _, line := range x {
			out.WriteString("- " + line + "\n")
		}
		for _, line := range y {
			out.WriteString("+ " + line + "\n")
		}
		return out.String()
	}
	// Longest common subsequence, from the end.
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out strings.Builder
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			out.WriteString("  " + x[i] + "\n")
			i++
			j++
		case j < len(y) && (i == len(x) || lcs[i][j+1] >= lcs[i+1][j]):
			out.WriteString("+ " + y[j] + "\n")
			j++
		default:
			out.WriteString("- " + x[i] + "\n")
			i++
		}
	}
	return out.String()
}

// maxDiffWork bounds lineDiff's line-by-line match, which takes memory for
// every pair of old and new lines.
const maxDiffWork = 4_000_000

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
