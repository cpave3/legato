package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/libretto"
	"github.com/cpave3/legato/internal/service"
)

// fakeStudios stands in for the service that runs libretto studios.
type fakeStudios struct {
	address string
	err     error
	asked   []string
}

func (f *fakeStudios) Address(_ context.Context, itemID string) (string, error) {
	f.asked = append(f.asked, itemID)
	return f.address, f.err
}

// studioSeen is what a pretend libretto studio received.
type studioSeen struct {
	method, path, query, header, authorization, host, body string
}

// pretendStudio is an HTTP server that answers as a libretto studio would,
// and records what it was sent.
func pretendStudio(t *testing.T) (*httptest.Server, *studioSeen) {
	t.Helper()
	seen := &studioSeen{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*seen = studioSeen{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Get("X-Libretto-Studio"),
			authorization: r.Header.Get("Authorization"), host: r.Host, body: string(body)}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api":1,"scenes":[]}`))
	}))
	t.Cleanup(upstream.Close)
	return upstream, seen
}

func studioRequest(srv *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer the-persons-token")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestTheStudioRoutePassesRequestsOnToTheItemsStudio(t *testing.T) {
	srv, _, _ := newAgentUIAPITestServer(t)
	upstream, seen := pretendStudio(t)
	studios := &fakeStudios{address: upstream.URL}
	srv.SetAgentStudioService(studios)

	rec := studioRequest(srv, http.MethodGet, "/api/ui-items/ui-0123456789/studio/api/scenes/intro/still.png?v=abc", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"api":1,"scenes":[]}` {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(studios.asked) != 1 || studios.asked[0] != "ui-0123456789" {
		t.Errorf("asked for the studio of %v", studios.asked)
	}
	if seen.method != http.MethodGet || seen.path != "/api/scenes/intro/still.png" || seen.query != "v=abc" {
		t.Errorf("the studio was sent %s %s ?%s", seen.method, seen.path, seen.query)
	}
	// The studio answers only its own page on its own machine: the request
	// arrives under the studio's own address, with the header its page
	// sends. The person's Legato token is not the studio's to see.
	if seen.header == "" || seen.host != strings.TrimPrefix(upstream.URL, "http://") || seen.authorization != "" {
		t.Errorf("the studio saw header %q, host %q, authorization %q", seen.header, seen.host, seen.authorization)
	}

	// A take is a body of some size, sent on as it is.
	take := bytes.Repeat([]byte("webm"), 50_000)
	rec = studioRequest(srv, http.MethodPost, "/api/ui-items/ui-0123456789/studio/api/scenes/intro/takes", take)
	if rec.Code != http.StatusOK || seen.method != http.MethodPost || seen.path != "/api/scenes/intro/takes" || seen.body != string(take) {
		t.Errorf("a take: status=%d, the studio was sent %s %s with %d bytes", rec.Code, seen.method, seen.path, len(seen.body))
	}
}

func TestTheStudioRouteSaysWhyThereIsNoStudio(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		want   string
	}{
		{"libretto is not installed", libretto.ErrNotInstalled, http.StatusServiceUnavailable, "libretto_missing", "libretto is not installed"},
		{"libretto is too old", &libretto.TooOldError{Version: "0.3.0"}, http.StatusServiceUnavailable, "libretto_old", "too old"},
		{"the item is not a studio", service.ErrAgentUINotAStudio, http.StatusNotFound, "not_a_studio", "not a studio"},
		{"the studio would not start", errors.New("libretto studio did not start: there is no storyboard at /gone.json"), http.StatusBadGateway, "studio_failed", "there is no storyboard at /gone.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _, _ := newAgentUIAPITestServer(t)
			srv.SetAgentStudioService(&fakeStudios{err: tt.err})
			rec := studioRequest(srv, http.MethodGet, "/api/ui-items/ui-0123456789/studio/api/project", nil)
			var body struct{ Error, Code string }
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("the answer is not JSON: %s", rec.Body.String())
			}
			if rec.Code != tt.status || body.Code != tt.code || !strings.Contains(body.Error, tt.want) {
				t.Errorf("status=%d code=%q error=%q; want %d %q containing %q", rec.Code, body.Code, body.Error, tt.status, tt.code, tt.want)
			}
		})
	}

	// A studio that stopped answering is reported, not left to hang.
	srv, _, _ := newAgentUIAPITestServer(t)
	upstream, _ := pretendStudio(t)
	address := upstream.URL
	upstream.Close()
	srv.SetAgentStudioService(&fakeStudios{address: address})
	rec := studioRequest(srv, http.MethodGet, "/api/ui-items/ui-0123456789/studio/api/project", nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "studio_failed") {
		t.Errorf("a studio that is gone: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// A Legato with no studio service says so.
	srv, _, _ = newAgentUIAPITestServer(t)
	rec = studioRequest(srv, http.MethodGet, "/api/ui-items/ui-0123456789/studio/api/project", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("with no studio service: status=%d", rec.Code)
	}
}

// toldAgents records what a task's agent was told.
type toldAgents struct {
	told chan [2]string
}

func (a *toldAgents) QueueMessage(_ context.Context, taskID, message string) error {
	a.told <- [2]string{taskID, message}
	return nil
}

func TestTheAgentIsToldOfATakeWhenThePanelAsksForThat(t *testing.T) {
	srv, ui, _ := newAgentUIAPITestServer(t)
	storyboard := filepath.Join(t.TempDir(), "storyboard.json")
	if err := os.WriteFile(storyboard, []byte(`{"scenes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	item, err := ui.Show(context.Background(), service.AgentUIShowInput{TaskID: "task1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatStudio,
		Title: "Talk", Key: "talk", Content: service.AgentUIStudioContent(storyboard, "")})
	if err != nil {
		t.Fatal(err)
	}
	status := http.StatusOK
	var notify string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		notify = r.Header.Get("X-Legato-Studio-Notify")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"api":4,"scenes":[{"id":"intro","status":"recorded","take":{"src":"takes/intro-01.webm"}}]}`))
	}))
	t.Cleanup(upstream.Close)
	srv.SetAgentStudioService(&fakeStudios{address: upstream.URL})
	agents := &toldAgents{told: make(chan [2]string, 4)}
	srv.SetAgentStudioNotifier(agents)

	send := func(method, rest string, asks bool) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/ui-items/"+item.ID+"/studio/"+rest, strings.NewReader("a take"))
		if asks {
			req.Header.Set("X-Legato-Studio-Notify", "1")
		}
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	nothingTold := func(why string) {
		t.Helper()
		select {
		case got := <-agents.told:
			t.Errorf("%s, and the agent was told: %v", why, got)
		case <-time.After(150 * time.Millisecond):
		}
	}

	// The panel sends the take with the header that asks for the agent to be
	// told. The answer reaches the panel whole, and the agent of the item's
	// task hears of the take.
	rec := send(http.MethodPost, "api/scenes/intro/takes", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"takes/intro-01.webm"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case got := <-agents.told:
		if got[0] != "task1" || !strings.Contains(got[1], `scene "intro"`) || !strings.Contains(got[1], filepath.Join(filepath.Dir(storyboard), "takes", "intro-01.webm")) {
			t.Errorf("the agent of %q was told:\n%s", got[0], got[1])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent was not told of the take")
	}
	// The header is Legato's, and is not passed on.
	if notify != "" {
		t.Errorf("the studio was sent the header %q", notify)
	}

	// Not asked for, nothing is said: the person turned it off.
	send(http.MethodPost, "api/scenes/intro/takes", false)
	nothingTold("the panel did not ask")
	// A take the studio could not keep is nothing to review.
	status = http.StatusUnprocessableEntity
	send(http.MethodPost, "api/scenes/intro/takes", true)
	nothingTold("the studio refused the take")
	// Reading is not doing.
	status = http.StatusOK
	send(http.MethodGet, "api/project", true)
	nothingTold("the panel only read the project")
}
