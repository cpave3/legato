package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeReeveVoice struct{ status service.VoiceReadiness }

func (f *fakeReeveVoice) Status(context.Context) (service.VoiceReadiness, error) {
	return f.status, nil
}

func TestTheVoiceStatusIsServedWithTheWebToken(t *testing.T) {
	srv, _ := reeveServer(t)
	srv.SetReeveVoice(&fakeReeveVoice{status: service.VoiceReadiness{
		Instance: service.VoiceInstance{ID: "srv-1", Name: "tessitura", Role: "main"},
		Reason:   "The Reeve's voice is off on tessitura", Setting: "reeve.voice.enabled"}})

	if rec := callAs(srv, "wrong", http.MethodGet, "/api/reeve/voice", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the token: %d", rec.Code)
	}
	rec := callAs(srv, "secret", http.MethodGet, "/api/reeve/voice", "")
	var got service.VoiceReadiness
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	if got.Ready || got.Instance.Role != "main" || got.Setting != "reeve.voice.enabled" {
		t.Fatalf("got %+v", got)
	}
}

func TestTheVoiceStatusSaysWhenThisServerHasNoVoice(t *testing.T) {
	srv, _ := reeveServer(t)
	rec := callAs(srv, "secret", http.MethodGet, "/api/reeve/voice", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}
