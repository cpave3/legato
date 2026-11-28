package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// ClaudeBridgeHookOptions says how `legato agent bridge-hook claude-code`
// reaches the session's bridge.
type ClaudeBridgeHookOptions struct {
	// URL is the bridge's endpoint with its secret path, which the Legato
	// mod puts in LEGATO_CLAUDE_BRIDGE_URL once the bridge is ready. Empty
	// means there is no bridge, and the hook does nothing.
	URL string
	// Client makes the calls; nil uses one that never goes through a proxy.
	Client *http.Client
}

// What a text or argument reaches the bridge with at most, as the mod cut
// them: the panel only has to show enough to follow along.
const (
	bridgeHookArgumentLimit = 20_000
	bridgeHookTextLimit     = 20_000
)

// bridgeHookCallTimeout bounds one call to the bridge. The bridge answers a
// held permission with "wait" after 25 seconds, so a call never needs more.
const bridgeHookCallTimeout = 60 * time.Second

var bridgeHookRequests atomic.Int64

// providerEndpoint is any address in a failed request's explanation: the
// panel shows that the request failed, not where it went.
var providerEndpoint = regexp.MustCompile(`https?://[^\s<>"']+`)

// ClaudeBridgeHook answers one of Claude Code's command hooks for the
// session's bridge and returns what the hook prints.
//
// Claude Code's bundled security mod passes the `classic.*` events over every
// mod a person installs, the Legato mod included (LEG-684), so the mod can no
// longer answer a permission dialog. A command hook runs in the engine's
// core, which nothing passes over; this is that hook.
//
//   - PermissionRequest (an AskUserQuestion or a plan as well as a tool's
//     permission): the request is put to the bridge, which shows it in the
//     panel while someone watches, and Legato's decision is printed. The
//     terminal's dialog stays up throughout; when it is answered first,
//     Claude Code ends this process and the bridge lets the request go.
//   - StopFailure: the failed request is reported to the panel.
//
// Without a decision, and on any failure, it prints nothing, so the dialog
// alone decides and the hook never fails the tool call.
func ClaudeBridgeHook(ctx context.Context, payload io.Reader, opts ClaudeBridgeHookOptions) string {
	if opts.URL == "" {
		return ""
	}
	var hook struct {
		Event       string            `json:"hook_event_name"`
		Tool        string            `json:"tool_name"`
		Input       json.RawMessage   `json:"tool_input"`
		Suggestions []json.RawMessage `json:"permission_suggestions"`
		Error       string            `json:"error"`
		LastMessage string            `json:"last_assistant_message"`
	}
	if err := json.NewDecoder(payload).Decode(&hook); err != nil {
		return ""
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{
			Timeout: bridgeHookCallTimeout,
			// The bridge listens on loopback; an agent's proxy must not
			// stand between them.
			Transport: &http.Transport{Proxy: nil},
		}
	}
	url := strings.TrimRight(opts.URL, "/")
	switch hook.Event {
	case "PermissionRequest":
		return askBridgePermission(ctx, client, url, hook.Tool, hook.Input, hook.Suggestions)
	case "StopFailure":
		explanation := hook.LastMessage
		if explanation == "" {
			explanation = hook.Error
		}
		explanation = capText(providerEndpoint.ReplaceAllString(explanation, "[provider endpoint]"), bridgeHookTextLimit)
		body := map[string]any{"events": []any{map[string]any{"type": "request_error", "error": explanation}}}
		if response, err := postBridge(ctx, client, url+"/mod/events", body); err == nil {
			response.Body.Close()
		}
	}
	return ""
}

// bridgePermissionAnswer is the bridge's answer to /mod/permissions.
type bridgePermissionAnswer struct {
	// wait: still open; the request is asked again under the same ID.
	Decision           string            `json:"decision"`
	Feedback           string            `json:"feedback"`
	Message            string            `json:"message"`
	Answers            map[string]string `json:"answers"`
	ReturnInput        bool              `json:"returnInput"`
	UpdatedPermissions []json.RawMessage `json:"updatedPermissions"`
}

func askBridgePermission(ctx context.Context, client *http.Client, url, tool string, input json.RawMessage, suggestions []json.RawMessage) string {
	var fields map[string]any
	if err := json.Unmarshal(input, &fields); err != nil {
		return ""
	}
	requestID := fmt.Sprintf("hook-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), bridgeHookRequests.Add(1))
	body := map[string]any{
		"requestId":   requestID,
		"tool":        tool,
		"input":       capArguments(fields),
		"suggestions": suggestions,
	}
	var answer bridgePermissionAnswer
	for {
		response, err := postBridge(ctx, client, url+"/mod/permissions", body)
		if err != nil {
			return ""
		}
		answer = bridgePermissionAnswer{}
		decodeErr := json.NewDecoder(response.Body).Decode(&answer)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil {
			return ""
		}
		if answer.Decision != "wait" {
			break
		}
	}

	decision := map[string]any{}
	switch answer.Decision {
	case "allow":
		decision["behavior"] = "allow"
		if answer.Answers != nil {
			// The engine reads a question's answers from the tool's own
			// input, where its dialog would have put them.
			fields["answers"] = answer.Answers
			decision["updatedInput"] = fields
		} else if answer.ReturnInput {
			decision["updatedInput"] = json.RawMessage(input)
		}
		if answer.UpdatedPermissions != nil {
			decision["updatedPermissions"] = answer.UpdatedPermissions
		}
	case "deny":
		message := answer.Message
		if message == "" {
			message = answer.Feedback
		}
		if message == "" {
			message = "Denied in Legato."
		}
		decision["behavior"] = "deny"
		decision["message"] = message
	default:
		return ""
	}
	printed, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": decision},
	})
	if err != nil {
		return ""
	}
	return string(printed)
}

func postBridge(ctx context.Context, client *http.Client, url string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	return client.Do(request)
}

// capArguments cuts each string argument short for the panel.
func capArguments(fields map[string]any) map[string]any {
	capped := make(map[string]any, len(fields))
	for key, value := range fields {
		if text, ok := value.(string); ok {
			value = capText(text, bridgeHookArgumentLimit)
		}
		capped[key] = value
	}
	return capped
}

func capText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	// Cut at the start of a character, never inside one.
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit] + "\n… (truncated)"
}
