package service

import (
	"encoding/json"
	"net/url"
	"strings"
)

// LinkNotifier sends a notification whose click opens a link.
type LinkNotifier interface {
	NotifyLink(title, message, link string) error
}

// ApprovalNotifications returns the function an ApprovalService calls for
// each approval it creates: one notification that says what is asked and by
// whom, never the command or anything else the request carries. When
// serverURL is set, the notification opens the approval in the Legato app.
func ApprovalNotifications(notifier Notifier, serverURL string) func(Approval) {
	return func(approval Approval) {
		if notifier == nil || !notifier.Configured() {
			return
		}
		var details struct {
			TaskTitle string `json:"task_title"`
			AgentKind string `json:"agent_kind"`
		}
		_ = json.Unmarshal(approval.Details, &details)
		who := details.TaskTitle
		if who == "" {
			who = approval.TaskID
		}
		parts := []string{}
		if who != "" {
			parts = append(parts, who)
		}
		if details.AgentKind != "" {
			parts = append(parts, details.AgentKind)
		}
		message := "Legato needs your decision"
		if len(parts) > 0 {
			message = strings.Join(parts, " · ") + " needs your decision"
		}
		link := ""
		if serverURL != "" {
			link = "legato://approval?url=" + url.QueryEscape(strings.TrimRight(serverURL, "/")) + "&id=" + url.QueryEscape(approval.ID)
		}
		if linker, ok := notifier.(LinkNotifier); ok {
			_ = linker.NotifyLink(approval.Title, message, link)
			return
		}
		_ = notifier.Notify(approval.Title, message)
	}
}
