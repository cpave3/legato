package claudebridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func postIdentityEvents(handler http.Handler, body string) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mod/events", strings.NewReader(body)))
		done <- response
	}()
	return done
}

func awaitIdentityResponse(t *testing.T, done <-chan *httptest.ResponseRecorder, status int) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case response := <-done:
		if response.Code != status {
			t.Fatalf("event response = %d %s, want %d", response.Code, response.Body, status)
		}
		return response
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event response")
		return nil
	}
}

func awaitIdentitySignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for identity coordination")
	}
}

type identityStreamRecorder struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
	once    sync.Once
}

func (r *identityStreamRecorder) Flush() {
	r.ResponseRecorder.Flush()
	r.once.Do(func() { close(r.flushed) })
}

func TestIdentityPersistenceDoesNotBlockPanelReadsAndOrdersConcurrentBatches(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	captured := make(chan string, 4)
	bridge := New(Options{TaskID: "task-1", OnIdentity: func(id, cwd string) error {
		captured <- id + ":" + cwd
		if id == "first" {
			close(entered)
			<-release
		}
		return nil
	}})
	handler := bridge.Handler()
	first := postIdentityEvents(handler, `{"events":[{"type":"hello","sessionId":"first","cwd":"/first"},{"type":"reset","sessionId":"second","cwd":"/second"}]}`)
	awaitIdentitySignal(t, entered)
	second := postIdentityEvents(handler, `{"events":[{"type":"identity","sessionId":"third","cwd":"/third"}]}`)

	readsDone := make(chan struct{})
	go func() {
		defer close(readsDone)
		for _, path := range []string{"/v1/sessions/task-1", "/v1/instance", "/v1/sessions/task-1/context", "/v1/sessions/task-1/models", "/v1/sessions/task-1/skills"} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Errorf("GET %s = %d", path, response.Code)
			}
			if path == "/v1/sessions/task-1" && !strings.Contains(response.Body.String(), `"cwd":"/first"`) {
				t.Errorf("snapshot during persistence = %s", response.Body)
			}
		}
	}()
	awaitIdentitySignal(t, readsDone)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &identityStreamRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{})}
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		handler.ServeHTTP(stream, httptest.NewRequest(http.MethodGet, "/v1/sessions/task-1/events", nil).WithContext(ctx))
	}()
	awaitIdentitySignal(t, stream.flushed)
	cancel()
	awaitIdentitySignal(t, streamDone)

	select {
	case response := <-second:
		t.Fatalf("concurrent batch overtook persistence: %d", response.Code)
	default:
	}
	unblock()
	awaitIdentityResponse(t, first, http.StatusNoContent)
	awaitIdentityResponse(t, second, http.StatusNoContent)
	for _, want := range []string{"first:/first", "second:/second", "third:/third"} {
		if got := <-captured; got != want {
			t.Fatalf("identity = %q, want %q", got, want)
		}
	}
	bridge.Close()
}

func TestCloseWaitsForIdentityPersistenceAndRejectsLaterEvents(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	bridge := New(Options{OnIdentity: func(id, cwd string) error {
		close(entered)
		<-release
		return nil
	}})
	handler := bridge.Handler()
	pending := postIdentityEvents(handler, `{"events":[{"type":"hello","sessionId":"first","cwd":"/repo"}]}`)
	awaitIdentitySignal(t, entered)
	closeStarted, closeDone := make(chan struct{}), make(chan struct{})
	go func() {
		close(closeStarted)
		bridge.Close()
		close(closeDone)
	}()
	awaitIdentitySignal(t, closeStarted)
	select {
	case <-closeDone:
		t.Fatal("Close returned during identity persistence")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	awaitIdentityResponse(t, pending, http.StatusNoContent)
	awaitIdentitySignal(t, closeDone)
	awaitIdentityResponse(t, postIdentityEvents(handler, `{"events":[{"type":"identity","sessionId":"later"}]}`), http.StatusServiceUnavailable)
	bridge.Close()
}
