package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DiscoveryError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

type DiscoveryResult struct {
	Definitions []*Definition    `json:"definitions"`
	Errors      []DiscoveryError `json:"errors"`
}

// Discover loads global then project definitions. Project definitions replace
// global definitions with the same workflow name.
func Discover(globalDir, projectDir string) DiscoveryResult {
	result := DiscoveryResult{Definitions: make([]*Definition, 0), Errors: make([]DiscoveryError, 0)}
	byName := make(map[string]*Definition)
	for _, dir := range []string{globalDir, projectDir} {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				result.Errors = append(result.Errors, DiscoveryError{Path: dir, Error: err.Error()})
			}
			continue
		}
		seen := make(map[string]string)
		for _, entry := range entries {
			if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml")) {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				result.Errors = append(result.Errors, DiscoveryError{Path: path, Error: err.Error()})
				continue
			}
			definition, err := Parse(path, data)
			if err != nil {
				result.Errors = append(result.Errors, DiscoveryError{Path: path, Error: err.Error()})
				continue
			}
			if previous, duplicate := seen[definition.Name]; duplicate {
				result.Errors = append(result.Errors, DiscoveryError{Path: path, Error: fmt.Sprintf("duplicate workflow %q at the same precedence (also %s)", definition.Name, previous)})
				continue
			}
			seen[definition.Name] = path
			byName[definition.Name] = definition
		}
	}
	for _, definition := range byName {
		result.Definitions = append(result.Definitions, definition)
	}
	sort.Slice(result.Definitions, func(i, j int) bool { return result.Definitions[i].Name < result.Definitions[j].Name })
	sort.Slice(result.Errors, func(i, j int) bool { return result.Errors[i].Path < result.Errors[j].Path })
	return result
}
