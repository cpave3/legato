package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Myself is the user whose token the client sends.
func (c *Client) Myself(ctx context.Context) (UserField, error) {
	body, err := c.doGet(ctx, "/rest/api/3/myself")
	if err != nil {
		return UserField{}, fmt.Errorf("myself: %w", err)
	}
	var me UserField
	if err := json.Unmarshal(body, &me); err != nil || me.AccountID == "" {
		return UserField{}, fmt.Errorf("myself: reading Jira's answer: %q", body)
	}
	return me, nil
}

// FindUsers lists the users Jira's user search matches with a query, such
// as an email address. Jira hides an email the user keeps private, so a
// user may match without one.
func (c *Client) FindUsers(ctx context.Context, query string) ([]UserField, error) {
	body, err := c.doGet(ctx, "/rest/api/3/user/search?query="+url.QueryEscape(query))
	if err != nil {
		return nil, fmt.Errorf("find users: %w", err)
	}
	var users []UserField
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("find users: reading Jira's answer: %w", err)
	}
	return users, nil
}

// Assign assigns an issue to an account, or unassigns it when accountID is
// empty. It never sends "-1", which is the project's default assignee.
func (c *Client) Assign(ctx context.Context, key, accountID string) error {
	var id any
	if accountID != "" {
		id = accountID
	}
	payload, err := json.Marshal(map[string]any{"accountId": id})
	if err != nil {
		return fmt.Errorf("assign: %w", err)
	}
	if _, err := c.doPut(ctx, "/rest/api/3/issue/"+url.PathEscape(key)+"/assignee", string(payload)); err != nil {
		return fmt.Errorf("assign: %w", err)
	}
	return nil
}

func (c *Client) doPut(ctx context.Context, path, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req)
}
