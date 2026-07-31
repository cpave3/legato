package hotkey

import "strings"

// DisplayKey joins alternate keys using the conventional slash notation.
func DisplayKey(binding Binding) string {
	keys := make([]string, 0, len(binding.Keys))
	seen := make(map[string]bool)
	for _, key := range binding.Keys {
		if !seen[key.Display] {
			keys = append(keys, key.Display)
			seen[key.Display] = true
		}
	}
	return strings.Join(keys, "/")
}

type Section struct {
	Name     string
	Bindings []Binding
}

// Sections groups an already ordered surface query by section.
func (r *Registry) Sections(ctx Context, surface Surface) []Section {
	return groupSections(r.Bindings(ctx, surface))
}

func (r *Registry) AllSections(ctx Context, surface Surface) []Section {
	return groupSections(r.AllBindings(ctx, surface))
}

func groupSections(bindings []Binding) []Section {
	var sections []Section
	for _, binding := range bindings {
		if len(sections) == 0 || sections[len(sections)-1].Name != binding.Section {
			sections = append(sections, Section{Name: binding.Section})
		}
		last := len(sections) - 1
		sections[last].Bindings = append(sections[last].Bindings, binding)
	}
	return sections
}

func PageTitle(page Page) string {
	switch page {
	case PageDetail:
		return "Detail"
	case PageAgents:
		return "Agents"
	case PageReport:
		return "Report"
	case PageReview:
		return "Review"
	case PagePlans:
		return "Plans"
	case PageAttention:
		return "Attention"
	default:
		return "Board"
	}
}
