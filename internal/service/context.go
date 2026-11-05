package service

import (
	"fmt"
	"strings"
)

// formatDescription produces a description-only export: heading + description body.
func formatDescription(card *CardDetail) string {
	out := fmt.Sprintf("## %s: %s\n", card.ID, card.Title)
	if card.DescriptionMD != "" {
		out += "\n" + card.DescriptionMD + "\n"
	}
	return out + formatAttachments(card.Attachments)
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
	if len(card.PullRequests) > 0 {
		prs := make([]string, 0, len(card.PullRequests))
		for _, pr := range card.PullRequests {
			prs = append(prs, formatPullRequest(pr))
		}
		out += fmt.Sprintf("**Pull requests:** %s\n", strings.Join(prs, "; "))
	}

	out += "\n---\n"

	if card.DescriptionMD != "" {
		out += "\n" + card.DescriptionMD + "\n"
	}

	out += formatAttachments(card.Attachments)
	if len(card.Notes) > 0 {
		out += "\n## Notes\n"
		for _, note := range card.Notes {
			out += "\n### " + note.CreatedAt
			if note.Source != "" {
				out += " · " + note.Source
			}
			out += "\n\n" + note.Body + "\n"
		}
	}
	return out
}

func formatAttachments(items []LocalAttachment) string {
	if len(items) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\n## Local attachments\n\n")
	for _, item := range items {
		fmt.Fprintf(&out, "- %s: `%s`\n", item.Filename, item.Path)
	}
	return out.String()
}

// formatPullRequest names one PR for a task's context: number, branch,
// state, checks and review.
func formatPullRequest(pr PRMetaView) string {
	out := fmt.Sprintf("#%d", pr.PRNumber)
	var parts []string
	if pr.Branch != "" {
		parts = append(parts, pr.Branch)
	}
	if pr.IsDraft {
		parts = append(parts, "draft")
	} else if pr.State != "" {
		parts = append(parts, strings.ToLower(pr.State))
	}
	if pr.CheckStatus != "" {
		parts = append(parts, "checks "+pr.CheckStatus)
	}
	if pr.ReviewDecision != "" {
		parts = append(parts, strings.ToLower(strings.ReplaceAll(pr.ReviewDecision, "_", " ")))
	}
	if pr.PRURL != "" {
		parts = append(parts, pr.PRURL)
	}
	if len(parts) > 0 {
		out += " (" + strings.Join(parts, ", ") + ")"
	}
	return out
}
