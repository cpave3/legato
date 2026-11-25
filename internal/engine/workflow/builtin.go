package workflow

import (
	"sort"
	"strings"
)

// WithBuiltIns adds Legato's own definitions to a discovery result. A
// definition the person wrote with the same name replaces the built-in one,
// unless the name is reserved (review-loop).
func WithBuiltIns(result DiscoveryResult, builtIns ...*Definition) DiscoveryResult {
	// A reserved built-in's name is its own: a definition that takes it
	// gives way.
	kept := result.Definitions[:0:0]
	for _, definition := range result.Definitions {
		if definition != nil && reserved(definition.Name) && !strings.HasPrefix(definition.Source, builtInSourcePrefix) {
			continue
		}
		kept = append(kept, definition)
	}
	result.Definitions = kept
	present := make(map[string]bool, len(result.Definitions))
	for _, definition := range result.Definitions {
		present[definition.Name] = true
	}
	for _, definition := range builtIns {
		if definition != nil && !present[definition.Name] {
			result.Definitions = append(result.Definitions, definition)
			present[definition.Name] = true
		}
	}
	sort.SliceStable(result.Definitions, func(i, j int) bool { return result.Definitions[i].Name < result.Definitions[j].Name })
	return result
}
