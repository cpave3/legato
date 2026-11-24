package discordbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// maxRetryAfter is the longest rate-limit wait CreateMessage sits out itself.
const maxRetryAfter = 5 * time.Second

// maxResponse bounds a REST response body.
const maxResponse = 1 << 20

type messageReference struct {
	MessageID       string `json:"message_id"`
	FailIfNotExists bool   `json:"fail_if_not_exists"`
}

type createMessageBody struct {
	Content          string            `json:"content"`
	MessageReference *messageReference `json:"message_reference,omitempty"`
	AllowedMentions  struct {
		Parse []string `json:"parse"`
	} `json:"allowed_mentions"`
}

// CreateMessage posts content to a channel, as a reply to replyTo when it is
// not "". It mentions no one by @everyone, roles or users. It returns the new
// message's ID. A rate limit of up to 5 seconds is waited out and retried once.
func (c *Client) CreateMessage(ctx context.Context, channelID, content, replyTo string) (string, error) {
	if content == "" {
		return "", errors.New("discord: empty message")
	}
	if len([]rune(content)) > MaxContentLength {
		return "", ErrTooLong
	}
	if channelID == "" {
		return "", errors.New("discord: no channel")
	}
	body := createMessageBody{Content: content}
	body.AllowedMentions.Parse = []string{}
	if replyTo != "" {
		body.MessageReference = &messageReference{MessageID: replyTo}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	endpoint := c.apiBase() + "/channels/" + url.PathEscape(channelID) + "/messages"

	var out struct {
		ID string `json:"id"`
	}
	for attempt := 0; ; attempt++ {
		err = c.do(ctx, http.MethodPost, endpoint, payload, &out)
		var apiErr *APIError
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests &&
			apiErr.RetryAfter <= maxRetryAfter {
			t := time.NewTimer(apiErr.RetryAfter)
			select {
			case <-ctx.Done():
				t.Stop()
				return "", ctx.Err()
			case <-t.C:
			}
			continue
		}
		if err != nil {
			return "", err
		}
		return out.ID, nil
	}
}

// do makes one REST call. Errors never hold the token: the request is not
// part of them.
func (c *Client) do(ctx context.Context, method, endpoint string, payload []byte, out any) error {
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return fmt.Errorf("discord: bad request address")
	}
	req.Header.Set("Authorization", "Bot "+c.Token)
	req.Header.Set("User-Agent", "DiscordBot (https://github.com/cpave3/legato, 1)")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("discord: %s %s: %w", method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return fmt.Errorf("discord: reading response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAuth
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var body struct {
			Code       int      `json:"code"`
			Message    string   `json:"message"`
			RetryAfter *float64 `json:"retry_after"`
		}
		_ = json.Unmarshal(data, &body)
		apiErr := &APIError{Status: resp.StatusCode, Code: body.Code, Message: body.Message}
		if body.RetryAfter != nil {
			apiErr.RetryAfter = time.Duration(*body.RetryAfter * float64(time.Second))
		} else if s, err := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); err == nil {
			apiErr.RetryAfter = time.Duration(s * float64(time.Second))
		}
		return apiErr
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("discord: bad response: %w", err)
		}
	}
	return nil
}
