package hotkey

import (
	"fmt"
	"strings"
)

// keyboardReference writes the docs site's keyboard page from the
// registry: the keys of the full help, page by page, with the keys of a
// page's modes under their own headings. Overlay keys are left out: each
// overlay shows its own.
func keyboardReference(bindings []Binding) string {
	var b strings.Builder
	b.WriteString(`---
title: Keyboard shortcuts
description: Every key of the Legato TUI, page by page.
sidebar:
  order: 30
---

<!-- Written by go test ./internal/tui/hotkey -update from the hotkey registry. Do not edit. -->

In the TUI, press ` + "`?`" + ` to see these keys. The help shows only the keys that work with your configuration. This page lists all of them.

`)
	writeGroup(&b, "Everywhere", bindingsIn(bindings, func(s Scope) bool { return s.Global }))
	for _, page := range referencePages {
		inPage := bindingsIn(bindings, func(s Scope) bool { return s.Page == page.page })
		writeGroup(&b, page.title, inPage)
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

var referencePages = []struct {
	page  Page
	title string
}{
	{PageBoard, "Board"},
	{PageDetail, "Task detail"},
	{PageAgents, "Agents"},
	{PageReport, "Reports"},
	{PageReview, "Reviews"},
	{PagePlans, "Plans"},
	{PageAttention, "Attention"},
}

// The modes of a page, in the order a person meets them.
var modeTitles = []struct {
	mode  Mode
	title string
}{
	{ModeList, "In the list"},
	{ModeGrid, "In the ensemble"},
	{ModeQueue, "In the queue"},
	{ModeReader, "Reading a plan"},
	{ModeTour, "In a review tour"},
}

// What a person turns on to get a key that is not always there.
var capabilityNotes = map[Capability]string{
	CapabilitySync:          "with a ticket provider such as Jira",
	CapabilityWorktrees:     "with Yggdrasil worktrees on",
	CapabilityVoice:         "with voice input on",
	CapabilityNotifications: "with ntfy notifications set up",
}

// bindingsIn returns the full-help bindings of the scopes in, in the
// help's order, with every capability and predicate on.
func bindingsIn(bindings []Binding, in func(Scope) bool) []Binding {
	var selected []Binding
	for _, binding := range bindings {
		if binding.Surfaces&FullHelp != 0 && in(binding.Scope) {
			selected = append(selected, binding)
		}
	}
	registry := &Registry{bindings: selected}
	ctx := Context{Capabilities: Capabilities{}, Predicates: Predicates{}}
	for _, binding := range selected {
		for _, c := range binding.Requires {
			ctx.Capabilities[c] = true
		}
		for _, p := range binding.When {
			ctx.Predicates[p] = true
		}
		for _, p := range binding.DisplayWhen {
			ctx.Predicates[p] = true
		}
	}
	return registry.orderedBindings(ctx, FullHelp, false)
}

func writeGroup(b *strings.Builder, title string, bindings []Binding) {
	if len(bindings) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)

	var base []Binding
	byMode := map[Mode][]Binding{}
	for _, binding := range bindings {
		if binding.Scope.Mode == "" {
			base = append(base, binding)
		} else {
			byMode[binding.Scope.Mode] = append(byMode[binding.Scope.Mode], binding)
		}
	}

	sections := groupSections(base)
	if len(sections) > 1 {
		for _, section := range sections {
			fmt.Fprintf(b, "### %s\n\n", section.Name)
			writeTable(b, section.Bindings)
		}
	} else if len(base) > 0 {
		writeTable(b, base)
	}
	for _, mode := range modeTitles {
		if len(byMode[mode.mode]) > 0 {
			fmt.Fprintf(b, "### %s\n\n", mode.title)
			writeTable(b, byMode[mode.mode])
			delete(byMode, mode.mode)
		}
	}
	for mode := range byMode {
		panic(fmt.Sprintf("mode %q has no title in modeTitles", mode))
	}
}

func writeTable(b *strings.Builder, bindings []Binding) {
	b.WriteString("| Key | Action |\n| --- | --- |\n")
	for _, binding := range bindings {
		help := binding.Help
		var notes []string
		for _, c := range binding.Requires {
			if note := capabilityNotes[c]; note != "" {
				notes = append(notes, note)
			}
		}
		if len(notes) > 0 {
			help += " (" + strings.Join(notes, ", ") + ")"
		}
		key := strings.ReplaceAll(DisplayKey(binding), "|", `\|`)
		fmt.Fprintf(b, "| `%s` | %s |\n", key, strings.ReplaceAll(help, "|", `\|`))
	}
	b.WriteString("\n")
}
