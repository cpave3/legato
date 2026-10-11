// Package manticore reads usage from a Manticore instance: how many tokens
// the Clients it records have used. It reads the instance's dashboard API,
// which needs no key; reporting usage to an instance is the Claude Code
// usage mod's job, not this package's.
package manticore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// requestTimeout bounds one request. Usage is shown beside live work, so a
// slow instance is treated as one that did not answer.
const requestTimeout = 5 * time.Second

// Client reads usage from one Manticore instance.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client for the instance at baseURL ("http://host:3456").
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// Summary is the usage an instance recorded over a span of time. PromptTokens
// counts the whole of each prompt, the part read from a cache included.
type Summary struct {
	Requests         int
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// Bucket is the usage of one hour.
type Bucket struct {
	Start            time.Time
	Requests         int
	PromptTokens     int64
	CompletionTokens int64
}

// Tokens is every token of the hour, in and out.
func (b Bucket) Tokens() int64 { return b.PromptTokens + b.CompletionTokens }

// Summary returns the usage recorded since a time.
func (c *Client) Summary(ctx context.Context, since time.Time) (Summary, error) {
	var body struct {
		TotalRequests         int   `json:"totalRequests"`
		TotalPromptTokens     int64 `json:"totalPromptTokens"`
		TotalCompletionTokens int64 `json:"totalCompletionTokens"`
		TotalTokens           int64 `json:"totalTokens"`
	}
	if err := c.get(ctx, "/api/dashboard/summary", url.Values{"startDate": {utc(since)}}, &body); err != nil {
		return Summary{}, err
	}
	return Summary{
		Requests:         body.TotalRequests,
		PromptTokens:     body.TotalPromptTokens,
		CompletionTokens: body.TotalCompletionTokens,
		TotalTokens:      body.TotalTokens,
	}, nil
}

// Hourly returns the usage of each hour since a time, oldest first. An hour
// with no requests has no bucket.
func (c *Client) Hourly(ctx context.Context, since time.Time) ([]Bucket, error) {
	var body []struct {
		BucketStart      time.Time `json:"bucketStart"`
		Requests         int       `json:"requests"`
		PromptTokens     int64     `json:"promptTokens"`
		CompletionTokens int64     `json:"completionTokens"`
	}
	query := url.Values{"bucket": {"hour"}, "startDate": {utc(since)}}
	if err := c.get(ctx, "/api/dashboard/time-series", query, &body); err != nil {
		return nil, err
	}
	buckets := make([]Bucket, len(body))
	for i, point := range body {
		buckets[i] = Bucket{
			Start:            point.BucketStart.UTC(),
			Requests:         point.Requests,
			PromptTokens:     point.PromptTokens,
			CompletionTokens: point.CompletionTokens,
		}
	}
	return buckets, nil
}

// utc writes a time the way Manticore accepts one: in UTC, to the second.
func utc(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

func (c *Client) get(ctx context.Context, path string, query url.Values, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return fmt.Errorf("manticore: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("manticore: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("manticore: %s answered %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("manticore: read %s: %w", path, err)
	}
	return nil
}
