package workflow

import "sort"

// WithBuiltIns adds Legato's own definitions to a discovery result. A
// definition the person wrote with the same name replaces the built-in one.
func WithBuiltIns(result DiscoveryResult, builtIns ...*Definition) DiscoveryResult {
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
