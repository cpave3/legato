package setup

import "context"

// RealJiraSetup implements JiraSetup using the existing setup functions.
type RealJiraSetup struct{}

func (r *RealJiraSetup) ValidateCredentials(ctx context.Context, baseURL, email, token string) error {
	return ValidateCredentials(ctx, baseURL, email, token)
}

func (r *RealJiraSetup) FetchProjects(ctx context.Context, baseURL, email, token string) ([]Project, error) {
	return FetchProjects(ctx, baseURL, email, token)
}

func (r *RealJiraSetup) FetchStatuses(ctx context.Context, baseURL, email, token, projectKey string) ([]string, error) {
	return FetchStatuses(ctx, baseURL, email, token, projectKey)
}
