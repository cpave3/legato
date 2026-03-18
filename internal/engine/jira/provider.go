package jira

import (
	"context"
	"strings"
	"time"
)

// ProviderTicket represents a ticket normalized from Jira's API format.
// This is the engine-layer representation; the service layer has its own type.
type ProviderTicket struct {
	ID            string
	Summary       string
	DescriptionMD string
	Status        string
	Priority      string
	IssueType     string
	Assignee      string
	Labels        []string
	EpicKey       string
	EpicName      string
	URL           string
	UpdatedAt     time.Time
}

// ProviderTransition represents an available state transition.
type ProviderTransition struct {
	ID           string
	Name         string
	TargetStatus string
}

// Provider wraps a Jira Client and implements the TicketProvider interface
// from the service layer.
type Provider struct {
	client  *Client
	baseURL string
}

// NewProvider creates a Jira-backed ticket provider.
func NewProvider(baseURL, email, apiToken string, timeout time.Duration) *Provider {
	return &Provider{
		client:  NewClient(baseURL, email, apiToken, timeout),
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// Search returns tickets matching the JQL query.
func (p *Provider) Search(ctx context.Context, query string) ([]ProviderTicket, error) {
	issues, err := p.client.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	tickets := make([]ProviderTicket, len(issues))
	for i, issue := range issues {
		tickets[i] = p.issueToTicket(issue)
	}
	return tickets, nil
}

// GetTicket fetches a single ticket by key.
func (p *Provider) GetTicket(ctx context.Context, id string) (*ProviderTicket, error) {
	issue, err := p.client.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	t := p.issueToTicket(*issue)
	return &t, nil
}

// ListTransitions returns available state transitions for a ticket.
func (p *Provider) ListTransitions(ctx context.Context, id string) ([]ProviderTransition, error) {
	transitions, err := p.client.GetTransitions(ctx, id)
	if err != nil {
		return nil, err
	}

	result := make([]ProviderTransition, len(transitions))
	for i, tr := range transitions {
		result[i] = ProviderTransition{
			ID:           tr.ID,
			Name:         tr.Name,
			TargetStatus: tr.To.Name,
		}
	}
	return result, nil
}

// DoTransition executes a state transition on a ticket.
func (p *Provider) DoTransition(ctx context.Context, id string, transitionID string) error {
	return p.client.DoTransition(ctx, id, transitionID)
}

func (p *Provider) issueToTicket(issue Issue) ProviderTicket {
	assignee := ""
	if issue.Fields.Assignee != nil {
		assignee = issue.Fields.Assignee.DisplayName
	}

	updatedAt, _ := time.Parse("2006-01-02T15:04:05.000-0700", issue.Fields.Updated)
	if updatedAt.IsZero() {
		updatedAt, _ = time.Parse("2006-01-02T15:04:05.000+0000", issue.Fields.Updated)
	}

	browseURL := p.baseURL + "/browse/" + issue.Key

	return ProviderTicket{
		ID:            issue.Key,
		Summary:       issue.Fields.Summary,
		DescriptionMD: ADFToMarkdown(issue.Fields.Description),
		Status:        issue.Fields.Status.Name,
		Priority:      issue.Fields.Priority.Name,
		IssueType:     issue.Fields.IssueType.Name,
		Assignee:      assignee,
		Labels:        issue.Fields.Labels,
		EpicKey:       issue.Fields.EpicKey,
		URL:           browseURL,
		UpdatedAt:     updatedAt,
	}
}
