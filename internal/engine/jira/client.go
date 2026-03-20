package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPageSize   = 50
	maxRetries        = 5
	initialBackoff    = 1 * time.Second
	maxBackoff        = 60 * time.Second
)

// Client is a Jira REST API v3 client with Basic Auth and rate limit handling.
type Client struct {
	baseURL    string
	email      string
	apiToken   string
	httpClient *http.Client
}

// NewClient creates a new Jira API client.
func NewClient(baseURL, email, apiToken string, timeout time.Duration) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		email:    email,
		apiToken: apiToken,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Search executes a JQL query and returns all matching issues, handling pagination automatically.
func (c *Client) Search(ctx context.Context, jql string) ([]Issue, error) {
	var allIssues []Issue
	startAt := 0

	for {
		params := url.Values{
			"jql":        {jql},
			"startAt":    {strconv.Itoa(startAt)},
			"maxResults": {strconv.Itoa(defaultPageSize)},
			"fields":     {"summary,status,priority,issuetype,assignee,labels,description,updated,project,customfield_10014"},
		}

		body, err := c.doGet(ctx, "/rest/api/3/search/jql?"+params.Encode())
		if err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}

		var result SearchResult
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("search unmarshal: %w", err)
		}

		allIssues = append(allIssues, result.Issues...)

		if startAt+len(result.Issues) >= result.Total {
			break
		}
		startAt += len(result.Issues)
	}

	return allIssues, nil
}

// GetIssue fetches a single issue by key.
func (c *Client) GetIssue(ctx context.Context, key string) (*Issue, error) {
	body, err := c.doGet(ctx, "/rest/api/3/issue/"+url.PathEscape(key))
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}

	var issue Issue
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("get issue unmarshal: %w", err)
	}

	return &issue, nil
}

// GetTransitions returns available transitions for an issue.
func (c *Client) GetTransitions(ctx context.Context, key string) ([]Transition, error) {
	body, err := c.doGet(ctx, "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions")
	if err != nil {
		return nil, fmt.Errorf("get transitions: %w", err)
	}

	var resp TransitionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("get transitions unmarshal: %w", err)
	}

	return resp.Transitions, nil
}

// DoTransition executes a workflow transition on an issue.
func (c *Client) DoTransition(ctx context.Context, key, transitionID string) error {
	payload := fmt.Sprintf(`{"transition":{"id":%q}}`, transitionID)
	_, err := c.doPost(ctx, "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions", payload)
	if err != nil {
		return fmt.Errorf("do transition: %w", err)
	}
	return nil
}

func (c *Client) doGet(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.doRequest(req)
}

func (c *Client) doPost(ctx context.Context, path, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req)
}

func (c *Client) doRequest(req *http.Request) ([]byte, error) {
	req.SetBasicAuth(c.email, c.apiToken)
	req.Header.Set("Accept", "application/json")

	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading response: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt == maxRetries {
				return nil, fmt.Errorf("rate limited after %d retries", maxRetries)
			}

			wait := backoff
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil {
					wait = time.Duration(secs) * time.Second
				}
			}

			select {
			case <-time.After(wait):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}

			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		if resp.StatusCode == http.StatusNoContent {
			return nil, nil
		}

		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("authentication failed: invalid credentials")
		}

		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("not found: %s", req.URL.Path)
		}

		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
		}

		return body, nil
	}

	return nil, fmt.Errorf("max retries exceeded")
}
