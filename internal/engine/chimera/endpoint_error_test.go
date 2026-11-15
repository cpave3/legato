package chimera

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransportFailuresKeepDiagnosticsWithoutEndpointCredentials(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	upstream.Close()
	endpoint := upstream.URL + "/nonsecret-endpoint-canary"
	for _, test := range []struct {
		name string
		call func(context.Context, *Client) error
	}{
		{"snapshot", func(ctx context.Context, client *Client) error {
			_, err := client.GetSession(ctx, "parent")
			return err
		}},
		{"context", func(ctx context.Context, client *Client) error {
			_, err := client.GetSessionContext(ctx, "parent")
			return err
		}},
		{"message", func(ctx context.Context, client *Client) error { return client.PostMessage(ctx, "parent", "fixture") }},
		{"append", func(ctx context.Context, client *Client) error {
			return client.PostMessageAppend(ctx, "parent", "fixture")
		}},
		{"interrupt", func(ctx context.Context, client *Client) error { return client.Interrupt(ctx, "parent") }},
		{"SSE reconnect", func(ctx context.Context, client *Client) error {
			return client.Subscribe(ctx, "parent", "cursor", func(EventEnvelope) {})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(endpoint)
			for attempt := 0; attempt < 3; attempt++ {
				err := test.call(context.Background(), client)
				if err == nil {
					t.Fatal("unavailable endpoint succeeded")
				}
				wrapped := fmt.Errorf("native operation: %w", err)
				if strings.Contains(wrapped.Error(), "nonsecret-endpoint-canary") {
					t.Fatal("wrapped transport error disclosed endpoint credential")
				}
				if !strings.Contains(wrapped.Error(), "connection refused") || !strings.Contains(wrapped.Error(), "/v1/sessions/parent") {
					t.Fatalf("actionable operation and transport failure lost: %v", wrapped)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := test.call(ctx, client)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation cause lost: %v", err)
			}
			if strings.Contains(err.Error(), "nonsecret-endpoint-canary") {
				t.Fatal("cancellation disclosed endpoint credential")
			}
		})
	}
}
