package service

import "fmt"

// formatDescription produces a description-only export: heading + description body.
func formatDescription(card *CardDetail) string {
	out := fmt.Sprintf("## %s: %s\n", card.ID, card.Title)
	if card.DescriptionMD != "" {
		out += "\n" + card.DescriptionMD + "\n"
	}
	return out
}

// formatFull produces a full structured block with metadata and description.
func formatFull(card *CardDetail) string {
	out := fmt.Sprintf("# Task: %s\n\n", card.ID)
	out += fmt.Sprintf("**Title:** %s\n", card.Title)

	// Issue type from remote meta
	if issueType := card.RemoteMeta["issue_type"]; issueType != "" {
		out += fmt.Sprintf("**Type:** %s\n", issueType)
	}
	if card.Priority != "" {
		out += fmt.Sprintf("**Priority:** %s\n", card.Priority)
	}
	// Epic from remote meta
	if epicName := card.RemoteMeta["epic_name"]; epicName != "" {
		out += fmt.Sprintf("**Epic:** %s\n", epicName)
	} else if epicKey := card.RemoteMeta["epic_key"]; epicKey != "" {
		out += fmt.Sprintf("**Epic:** %s\n", epicKey)
	}
	if labels := card.RemoteMeta["labels"]; labels != "" {
		out += fmt.Sprintf("**Labels:** %s\n", labels)
	}
	if url := card.RemoteMeta["url"]; url != "" {
		out += fmt.Sprintf("**URL:** %s\n", url)
	}

	out += "\n---\n"

	if card.DescriptionMD != "" {
		out += "\n" + card.DescriptionMD + "\n"
	}

	return out
}
