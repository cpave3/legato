package claudebridge

import (
	"bytes"
	"net/http"
	"testing"
	"time"
)

func TestPermissionRetryCannotReplaceTheApprovedInput(t *testing.T) {
	h := newHarness(t)
	h.mod(hello(nil), map[string]any{"type": "turn_start", "turnId": "t"})
	events := h.subscribe("")
	answer := h.askPermission("same-id", "Bash", map[string]any{"command": "printf original"})
	nextEvent(t, events)
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Post(h.publicURL()+"/mod/permissions", "application/json", bytes.NewBufferString(`{"requestId":"same-id","tool":"Bash","input":{"command":"printf substituted"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("replaced request status=%d", resp.StatusCode)
	}
	pending := h.snapshot().PendingPermissions
	if len(pending) != 1 || pending[0].Command != "printf original" {
		t.Fatalf("pending=%v", pending)
	}
	if code, body := h.post("/v1/sessions/task-1/permissions/same-id", map[string]any{"decision": "allow"}); code != http.StatusNoContent {
		t.Fatalf("answer=%d %s", code, body)
	}
	if nextAnswer(t, answer)["decision"] != "allow" {
		t.Fatal("original waiter did not receive host decision")
	}
}
