// Package subfolders lists the folders in a folder: what a folder of skills
// or of managed hooks holds, one folder each.
package subfolders

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// List names the folders in dir, sorted. A link to a folder counts, under
// the link's name; files, links to anything else, and names that start with
// a dot are left out. A dir that does not exist holds none.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		// Stat follows a link.
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.IsDir() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}
