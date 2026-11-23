package server

import (
	"context"
	"net/http"
	"testing"
)

// messagedReeve keeps what POST /api/reeve/message gave the Reeve.
type messagedReeve struct {
	ReeveSessions
	text, by string
}

func (m *messagedReeve) Message(_ context.Context, text, by string) error {
	m.text, m.by = text, by
	return nil
}

func TestAMessageFromTheGlassesReachesTheReeveMarked(t *testing.T) {
	srv, _ := reeveServer(t)
	reeve := &messagedReeve{ReeveSessions: srv.reeve}
	srv.SetReeveService(reeve)
	rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/message", `{"text": "start an agent on LEG-212", "from": "glasses"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if reeve.text != "[legato glasses] start an agent on LEG-212" || reeve.by != "glasses" {
		t.Fatalf("the Reeve was given %q by %q", reeve.text, reeve.by)
	}
}

func TestATypedMessageReachesTheReeveAsTyped(t *testing.T) {
	srv, _ := reeveServer(t)
	reeve := &messagedReeve{ReeveSessions: srv.reeve}
	srv.SetReeveService(reeve)
	if rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/message", `{"text": "hello"}`); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if reeve.text != "hello" || reeve.by != "web" {
		t.Fatalf("the Reeve was given %q by %q", reeve.text, reeve.by)
	}
}

func TestAMessageFromSomewhereUnknownIsRefused(t *testing.T) {
	srv, _ := reeveServer(t)
	reeve := &messagedReeve{ReeveSessions: srv.reeve}
	srv.SetReeveService(reeve)
	if rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/message", `{"text": "hello", "from": "voice"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if reeve.text != "" {
		t.Fatalf("the Reeve was given %q", reeve.text)
	}
}
