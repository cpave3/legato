package service

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

func renderHandoffMarkdown(packet HandoffPacket, concepts []HandoffConcept, input HandoffExportInput) string {
	var b strings.Builder
	title := "GitHub Review Brief"
	if input.Format == "maintenance" {
		title = "Maintenance Handoff Brief"
	}
	fmt.Fprintf(&b, "# %s\n\n> AI-authored snapshot; independently verify claims. PR linkage/current head: **unverified**.\n\n", title)
	fmt.Fprintf(&b, "Repository: %s · revision %d\n\nBase: %s · head: %s\n\n", safeMarkdown(packet.Repository), packet.Revision, safeMarkdown(packet.Input.BaseSHA), safeMarkdown(packet.Input.HeadSHA))
	if packet.PlanID != "" || packet.PlanRevision != "" {
		fmt.Fprintf(&b, "Plan: %s · revision: %s\n\n", safeMarkdown(packet.PlanID), safeMarkdown(packet.PlanRevision))
	}
	if packet.Outdated {
		fmt.Fprintf(&b, "> **Outdated:** %s\n\n", handoffBrief(packet.OutdatedReason, 20))
	}
	o := packet.Input.Overview
	for _, field := range [][2]string{{"Before", o.Before}, {"After", o.After}, {"Why", o.Why}, {"Non-goals", o.NonGoals}, {"Review question", o.ReviewQuestion}, {"Highest risk", o.Risk}, {"Verification gaps", o.VerificationGaps}} {
		fmt.Fprintf(&b, "**%s:** %s\n\n", field[0], handoffBrief(field[1], 24))
	}
	fmt.Fprintf(&b, "%d selected concepts. Expand the ordered route and supporting detail below.\n\n", len(concepts))
	open := func(title string) { fmt.Fprintf(&b, "<details>\n<summary>%s</summary>\n\n", title) }
	close := func() { b.WriteString("\n</details>\n\n") }
	open("Behavior, decisions, and limits")
	for _, field := range [][2]string{{"Before", o.Before}, {"After", o.After}, {"Why", o.Why}, {"Review question", o.ReviewQuestion}, {"Non-goals", o.NonGoals}, {"Decision", o.Decision}, {"Rejected alternative", o.Alternative}, {"Highest risk", o.Risk}, {"Verification gaps", o.VerificationGaps}, {"Rollback", o.Rollback}} {
		fmt.Fprintf(&b, "**%s:** %s\n\n", field[0], safeMarkdown(field[1]))
	}
	close()
	selectedSource, selectedEvidence, selectedConcept := map[string]bool{}, map[string]bool{}, map[string]bool{}
	linkedEvidence := map[string]bool{}
	for _, concept := range packet.Input.Concepts {
		for _, id := range concept.EvidenceIDs {
			linkedEvidence[id] = true
		}
	}
	for _, evidence := range packet.Input.Evidence {
		if !linkedEvidence[evidence.ID] {
			selectedEvidence[evidence.ID] = true
			for _, id := range evidence.SourceIDs {
				selectedSource[id] = true
			}
		}
	}
	open("Ordered reading route")
	for i, concept := range concepts {
		selectedConcept[concept.ID] = true
		refs := map[string]bool{}
		for _, id := range concept.SourceIDs {
			refs[id] = true
		}
		for _, id := range concept.EvidenceIDs {
			selectedEvidence[id] = true
			for _, e := range packet.Input.Evidence {
				if e.ID == id {
					for _, source := range e.SourceIDs {
						refs[source] = true
					}
				}
			}
		}
		for _, s := range packet.Input.Scenarios {
			if s.ConceptID == concept.ID {
				for _, id := range s.SourceIDs {
					refs[id] = true
				}
			}
		}
		fmt.Fprintf(&b, "### %d. %s\n\n%s\n\n", i+1, safeMarkdown(concept.Title), safeMarkdown(concept.Claim))
		for _, step := range concept.Flow {
			fmt.Fprintf(&b, "- %s\n", safeMarkdown(step))
		}
		fmt.Fprintf(&b, "\n**Invariant:** %s\n\n**Failure boundary:** %s\n\n**Start a change at:** %s\n\n", safeMarkdown(concept.Invariant), safeMarkdown(concept.FailureCase), safeMarkdown(concept.ChangeGuide))
		if len(concept.EvidenceIDs) == 0 {
			b.WriteString("**No evidence attached to this concept.**\n\n")
		} else {
			fmt.Fprintf(&b, "Evidence IDs: %s\n\n", safeMarkdown(strings.Join(concept.EvidenceIDs, ", ")))
		}
		if len(refs) == 0 {
			b.WriteString("No pinned sources attached to this concept.\n\n")
		}
		for _, source := range packet.Sources {
			if refs[source.ID] {
				selectedSource[source.ID] = true
				fmt.Fprintf(&b, "- %s\n", handoffSourceLink(packet.Repository, source))
			}
		}
		b.WriteString("\n")
	}
	close()
	open("Evidence (imported agent reports, not independent verification)")
	b.WriteString("Unlinked checks are packet-level evidence and remain included for every selection.\n\n")
	if len(selectedEvidence) == 0 {
		b.WriteString("No evidence attached to the selected concepts.\n")
	}
	for _, e := range packet.Input.Evidence {
		if !selectedEvidence[e.ID] {
			continue
		}
		label := ""
		if e.Outcome == "failed" {
			label += " **FAILED agent report.**"
		}
		if e.HeadSHA != packet.Input.HeadSHA {
			label += " **STALE agent report: outdated for this head.**"
		}
		fmt.Fprintf(&b, "- **%s** (%s): %s; head: %s; recorded_at: %s; outcome: **%s**; reporter: %s.%s\n\n  Limitations: %s\n", safeMarkdown(e.Description), safeMarkdown(e.ID), safeMarkdown(e.Command), safeMarkdown(e.HeadSHA), safeMarkdown(e.RecordedAt), safeMarkdown(e.Outcome), safeMarkdown(e.Reporter), label, safeMarkdown(e.Limitations))
		for _, source := range packet.Sources {
			for _, id := range e.SourceIDs {
				if source.ID == id {
					fmt.Fprintf(&b, "  - %s\n", handoffSourceLink(packet.Repository, source))
				}
			}
		}
	}
	close()
	open("Full diff coverage")
	for _, c := range packet.Input.Coverage {
		fmt.Fprintf(&b, "- %s — %s (classification is not a safety claim)\n", safeMarkdown(c.Path), safeMarkdown(c.Category))
	}
	close()
	open("Author change summary and revision comparison")
	fmt.Fprintf(&b, "**Author summary:** %s\n\n", safeMarkdown(packet.Input.Changes))
	if packet.Comparison.PreviousRevision > 0 {
		fmt.Fprintf(&b, "Since revision %d.\n\n", packet.Comparison.PreviousRevision)
	}
	if !packet.Comparison.Safe {
		fmt.Fprintf(&b, "Full reread required: %s\n\n", safeMarkdown(packet.Comparison.Reason))
	}
	fmt.Fprintf(&b, "Changed concepts: %s\n\nChanged files: %s\n\nEvidence to recheck: %s\n", safeMarkdown(strings.Join(packet.Comparison.ChangedConcepts, ", ")), safeMarkdown(strings.Join(packet.Comparison.ChangedFiles, ", ")), safeMarkdown(strings.Join(packet.Comparison.EvidenceToRecheck, ", ")))
	close()
	if input.Format == "maintenance" {
		open("Maintenance scenarios")
		for _, s := range packet.Input.Scenarios {
			if selectedConcept[s.ConceptID] {
				fmt.Fprintf(&b, "- **%s** (%s)\n\n  Authored explanation: %s\n\n", safeMarkdown(s.Prompt), safeMarkdown(s.ConceptID), safeMarkdown(s.Explanation))
			}
		}
		close()
	}
	if input.IncludeSources {
		open("Pinned source excerpts")
		for _, s := range packet.Sources {
			if selectedSource[s.ID] {
				fmt.Fprintf(&b, "### %s\n\nDigest: %s\n\n", handoffSourceLink(packet.Repository, s), safeMarkdown(s.Digest))
				// A longer fence preserves captured bytes, including embedded fences and controls.
				fence := "```"
				for strings.Contains(s.Text, fence) {
					fence += "`"
				}
				fmt.Fprintf(&b, "%stext\n%s\n%s\n\n", fence, s.Text, fence)
			}
		}
		close()
	}
	if input.IncludeNotes {
		open("Self-reported local note")
		fmt.Fprintf(&b, "Readiness: **%s** (not approval)\n\n%s\n\n", safeMarkdown(packet.Note.Readiness), safeMarkdown(packet.Note.Body))
		keys := make([]string, 0, len(packet.Note.Answers))
		for id := range packet.Note.Answers {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			fmt.Fprintf(&b, "- %s: %s\n", safeMarkdown(id), safeMarkdown(packet.Note.Answers[id]))
		}
		close()
	}
	return b.String()
}

var handoffGitHubIdentity = regexp.MustCompile(`^repo:github\.com/([A-Za-z0-9][A-Za-z0-9-]*)/([A-Za-z0-9_][A-Za-z0-9_.-]*)$`)
var handoffExactSHA = regexp.MustCompile(`^[a-fA-F0-9]{40}([a-fA-F0-9]{24})?$`)

func handoffSourceLink(repository string, s HandoffSource) string {
	label := fmt.Sprintf("%s: %s lines %d-%d at %s", s.ID, s.Path, s.LineStart, s.LineEnd, s.CommitSHA)
	repo := handoffGitHubIdentity.FindStringSubmatch(repository)
	if repo == nil || !handoffExactSHA.MatchString(s.CommitSHA) {
		return safeMarkdown(label) + " (source URL unavailable; repository identity unverified)"
	}
	parts := strings.Split(s.Path, "/")
	for i, part := range parts {
		if part == "." || part == ".." || part == "" {
			return safeMarkdown(label) + " (source URL unavailable)"
		}
		parts[i] = url.PathEscape(part)
		// PathEscape leaves parentheses, which delimit Markdown destinations.
		parts[i] = strings.NewReplacer("(", "%28", ")", "%29").Replace(parts[i])
	}
	return fmt.Sprintf("[%s](https://github.com/%s/%s/blob/%s/%s#L%d-L%d)", safeMarkdown(label), repo[1], repo[2], s.CommitSHA, strings.Join(parts, "/"), s.LineStart, s.LineEnd)
}

func handoffBrief(value string, words int) string {
	fields := strings.Fields(value)
	if len(fields) > words {
		return safeMarkdown(strings.Join(fields[:words], " ")) + "…"
	}
	return safeMarkdown(strings.Join(fields, " "))
}

func safeMarkdown(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\t' || unicode.Is(unicode.Cf, r) || r == '\u034f' || r == '\u2028' || r == '\u2029' {
			continue
		}
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		// Neutralize authored links, images, fences and block markup. Generated links
		// are emitted separately, from canonical repository identity and pinned SHAs.
		case '\\', '[', ']', '!', '`', '*', '_', '#', '|', '~':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
