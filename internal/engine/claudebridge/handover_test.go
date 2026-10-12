package claudebridge

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestASessionThatEndedReleasesItsBridge(t *testing.T) {
	// `/background` hands the conversation to another Claude process, which
	// starts a bridge of its own. The bridge of the session that ended must
	// go, or it keeps the task bound to a conversation nobody adds to.
	released := make(chan struct{})
	bridge := New(Options{TaskID: "task-1", OnDisconnect: func() { close(released) }})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})

	post := func(body string) {
		t.Helper()
		resp, err := http.Post(server.URL+"/mod/events", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", resp.StatusCode)
		}
	}

	post(`{"events":[{"type":"turn_start","turnId":"turn-1"}]}`)
	select {
	case <-released:
		t.Fatal("an ordinary event released the bridge")
	case <-time.After(50 * time.Millisecond):
	}

	post(`{"events":[{"type":"ended"}]}`)
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("the bridge was not released when its session ended")
	}
}
