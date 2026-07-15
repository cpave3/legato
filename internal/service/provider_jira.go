package service

import (
	"context"
	"io"

	"github.com/cpave3/legato/internal/engine/jira"
)

// JiraProviderAdapter wraps a jira.Provider to satisfy the TicketProvider interface.
type JiraProviderAdapter struct {
	p *jira.Provider
}

// NewJiraProvider creates a TicketProvider backed by Jira.
func NewJiraProvider(p *jira.Provider) TicketProvider {
	return &JiraProviderAdapter{p: p}
}

func jiraAttachments(items []jira.Attachment) []RemoteAttachment {
	result := make([]RemoteAttachment, len(items))
	for i, item := range items {
		result[i] = RemoteAttachment{ID: item.ID, Filename: item.Filename, MimeType: item.MimeType, Size: item.Size}
	}
	return result
}

func (a *JiraProviderAdapter) Search(ctx context.Context, query string) ([]RemoteTicket, error) {
	tickets, err := a.p.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	result := make([]RemoteTicket, len(tickets))
	for i, t := range tickets {
		result[i] = RemoteTicket{
			ID:            t.ID,
			Summary:       t.Summary,
			DescriptionMD: t.DescriptionMD,
			Status:        t.Status,
			Priority:      t.Priority,
			IssueType:     t.IssueType,
			Assignee:      t.Assignee,
			Labels:        t.Labels,
			EpicKey:       t.EpicKey,
			EpicName:      t.EpicName,
			URL:           t.URL,
			UpdatedAt:     t.UpdatedAt,
			Attachments:   jiraAttachments(t.Attachments),
		}
	}
	return result, nil
}

func (a *JiraProviderAdapter) GetTicket(ctx context.Context, id string) (*RemoteTicket, error) {
	t, err := a.p.GetTicket(ctx, id)
	if err != nil {
		return nil, err
	}
	return &RemoteTicket{
		ID:            t.ID,
		Summary:       t.Summary,
		DescriptionMD: t.DescriptionMD,
		Status:        t.Status,
		Priority:      t.Priority,
		IssueType:     t.IssueType,
		Assignee:      t.Assignee,
		Labels:        t.Labels,
		EpicKey:       t.EpicKey,
		EpicName:      t.EpicName,
		URL:           t.URL,
		UpdatedAt:     t.UpdatedAt,
		Attachments:   jiraAttachments(t.Attachments),
	}, nil
}

func (a *JiraProviderAdapter) ListTransitions(ctx context.Context, id string) ([]RemoteTransition, error) {
	trans, err := a.p.ListTransitions(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]RemoteTransition, len(trans))
	for i, tr := range trans {
		result[i] = RemoteTransition{
			ID:           tr.ID,
			Name:         tr.Name,
			TargetStatus: tr.TargetStatus,
		}
	}
	return result, nil
}

func (a *JiraProviderAdapter) DoTransition(ctx context.Context, id string, transitionID string) error {
	return a.p.DoTransition(ctx, id, transitionID)
}

func (a *JiraProviderAdapter) DownloadAttachment(ctx context.Context, id string) (io.ReadCloser, error) {
	return a.p.DownloadAttachment(ctx, id)
}
