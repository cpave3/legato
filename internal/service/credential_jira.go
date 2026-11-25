package service

import "strings"

// JiraProfile is the profile of a credential for Jira Cloud's REST API.
const JiraProfile = "jira"

// JiraDefaults are the Jira integration's site and account (jira.base_url,
// jira.email), which a jira credential uses when its settings name none.
type JiraDefaults struct {
	BaseURL string
	Email   string
}

// defaults gives a credential's upstream and username: its settings', else
// the Jira integration's for a jira credential.
func (j JiraDefaults) defaults(profile string) (upstream, username string) {
	if profile != JiraProfile {
		return "", ""
	}
	return strings.TrimRight(strings.TrimSpace(j.BaseURL), "/"), strings.TrimSpace(j.Email)
}
