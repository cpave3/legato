package service

import "fmt"

// formatDescription produces a description-only export: heading + description body.
func formatDescription(card *CardDetail) string {
	out := fmt.Sprintf("## %s: %s\n", card.ID, card.Summary)
	if card.DescriptionMD != "" {
		out += "\n" + card.DescriptionMD + "\n"
	}
	return out
}

// formatFull produces a full structured block with metadata and description.
func formatFull(card *CardDetail) string {
	out := fmt.Sprintf("# Ticket: %s\n\n", card.ID)
	out += fmt.Sprintf("**Summary:** %s\n", card.Summary)

	if card.IssueType != "" {
		out += fmt.Sprintf("**Type:** %s\n", card.IssueType)
	}
	if card.Priority != "" {
		out += fmt.Sprintf("**Priority:** %s\n", card.Priority)
	}
	if card.EpicName != "" {
		out += fmt.Sprintf("**Epic:** %s\n", card.EpicName)
	} else if card.EpicKey != "" {
		out += fmt.Sprintf("**Epic:** %s\n", card.EpicKey)
	}
	if card.Labels != "" {
		out += fmt.Sprintf("**Labels:** %s\n", card.Labels)
	}
	if card.URL != "" {
		out += fmt.Sprintf("**URL:** %s\n", card.URL)
	}

	out += "\n---\n"

	if card.DescriptionMD != "" {
		out += "\n" + card.DescriptionMD + "\n"
	}

	return out
}
