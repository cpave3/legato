package service

import (
	"sort"
	"strings"
)

// formatTierCatalog renders the per-adapter tier catalog as a markdown
// section the conductor can read in its brief. Format:
//
//	## Available tiers
//
//	### claude-code
//	- small — fast/cheap; trivial edits and rote refactors
//	- large — big reasoning; multi-file changes, novel logic
//
//	### chimera
//	- sandbox — sandboxed runs for risky migrations
//
// Adapters with no configured tiers are skipped. An empty catalog returns "" so
// the caller can append it unconditionally without producing a stray heading.
// Adapter and tier names are sorted alphabetically for stable output (the
// conductor sees the same catalog across runs regardless of map iteration
// order).
func formatTierCatalog(catalog map[string]map[string]string) string {
	if len(catalog) == 0 {
		return ""
	}

	adapters := make([]string, 0, len(catalog))
	for name, tiers := range catalog {
		if len(tiers) == 0 {
			continue
		}
		adapters = append(adapters, name)
	}
	if len(adapters) == 0 {
		return ""
	}
	sort.Strings(adapters)

	var b strings.Builder
	b.WriteString("## Available tiers\n\n")
	b.WriteString("Each entry below names a launch profile (typically a model selector) configured on the user's machine. " +
		"When you draft a plan, set `tier:` on each sub-task to one of these names — pick the tier whose description best " +
		"matches the sub-task's complexity. Omit `tier:` to use the adapter's base launch_args.\n")
	for _, adapter := range adapters {
		b.WriteString("\n### ")
		b.WriteString(adapter)
		b.WriteString("\n")
		tierNames := make([]string, 0, len(catalog[adapter]))
		for name := range catalog[adapter] {
			tierNames = append(tierNames, name)
		}
		sort.Strings(tierNames)
		for _, name := range tierNames {
			desc := strings.TrimSpace(catalog[adapter][name])
			if desc == "" {
				b.WriteString("- ")
				b.WriteString(name)
				b.WriteString("\n")
			} else {
				b.WriteString("- ")
				b.WriteString(name)
				b.WriteString(" — ")
				b.WriteString(desc)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}
