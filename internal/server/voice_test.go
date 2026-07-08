package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubVoiceService is a test double for the server's VoiceService interface.
type stubVoiceService struct {
	transcribedPCM  []byte
	transcribedSR   int
	transcribeText  string
	transcribeErr   error
	delivered       bool
	deliverSession  string
	deliverKind     string
	deliverText     string
	deliverAutoSend bool
}

func (s *stubVoiceService) TranscribePCM(ctx context.Context, pcm []byte, sampleRate int) (string, error) {
	s.transcribedPCM = pcm
	s.transcribedSR = sampleRate
	if s.transcribeErr != nil {
		return "", s.transcribeErr
	}
	return s.transcribeText, nil
}

func (s *stubVoiceService) Deliver(ctx context.Context, tmuxSession, agentKind, text string, autoSend bool) error {
	s.delivered = true
	s.deliverSession = tmuxSession
	s.deliverKind = agentKind
	s.deliverText = text
	s.deliverAutoSend = autoSend
	return nil
}

func (s *stubVoiceService) AutoSend() bool { return s.deliverAutoSend }

func TestVoiceTranscribe_Success(t *testing.T) {
	vs := &stubVoiceService{transcribeText: "hello from web"}
	srv := newTestServerWithVoice(vs)

	body := voiceTranscribeRequest{
		AgentID:   "REX-1",
		AgentKind: "claude-code",
		PCM:       []byte{0, 1, 2, 3},
	}
	bodyJSON, _ := json.Marshal(body)

	resp := srv.postJSON("/api/voice/transcribe", bodyJSON)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result voiceTranscribeResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Text != "hello from web" {
		t.Errorf("text = %q, want %q", result.Text, "hello from web")
	}
	if result.Error != "" {
		t.Errorf("error = %q, want empty", result.Error)
	}
	if !vs.delivered {
		t.Error("Deliver was not called")
	}
	if vs.deliverSession != "legato-REX-1" {
		t.Errorf("deliverSession = %q, want %q", vs.deliverSession, "legato-REX-1")
	}
	if vs.deliverKind != "claude-code" {
		t.Errorf("deliverKind = %q, want %q", vs.deliverKind, "claude-code")
	}
	if vs.deliverText != "hello from web" {
		t.Errorf("deliverText = %q, want %q", vs.deliverText, "hello from web")
	}
	if len(vs.transcribedPCM) != 4 {
		t.Errorf("transcribedPCM len = %d, want 4", len(vs.transcribedPCM))
	}
	if vs.transcribedSR != 16000 {
		t.Errorf("transcribedSR = %d, want 16000", vs.transcribedSR)
	}
}

func TestVoiceTranscribe_TranscriptionError(t *testing.T) {
	vs := &stubVoiceService{transcribeErr: errVoiceTest}
	srv := newTestServerWithVoice(vs)

	body := voiceTranscribeRequest{
		AgentID:   "REX-1",
		AgentKind: "claude-code",
		PCM:       []byte{0, 1},
	}
	bodyJSON, _ := json.Marshal(body)

	resp := srv.postJSON("/api/voice/transcribe", bodyJSON)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result voiceTranscribeResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error == "" {
		t.Error("expected error in response")
	}
	if vs.delivered {
		t.Error("Deliver should not be called on transcription error")
	}
}

func TestVoiceTranscribe_Disabled(t *testing.T) {
	srv := newTestServerWithVoice(nil)

	body := voiceTranscribeRequest{
		AgentID:   "REX-1",
		AgentKind: "claude-code",
		PCM:       []byte{0, 1},
	}
	bodyJSON, _ := json.Marshal(body)

	resp := srv.postJSON("/api/voice/transcribe", bodyJSON)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (voice disabled)", resp.StatusCode, http.StatusNotFound)
	}
}

func TestVoiceTranscribe_MissingAgentID(t *testing.T) {
	vs := &stubVoiceService{transcribeText: "hello"}
	srv := newTestServerWithVoice(vs)

	body := voiceTranscribeRequest{
		AgentKind: "claude-code",
		PCM:       []byte{0, 1},
	}
	bodyJSON, _ := json.Marshal(body)

	resp := srv.postJSON("/api/voice/transcribe", bodyJSON)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestSettings_VoiceEnabled(t *testing.T) {
	vs := &stubVoiceService{}
	srv := newTestServerWithVoice(vs)

	resp := srv.get("/api/settings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var result settingsResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if !result.VoiceEnabled {
		t.Error("voice_enabled should be true when voice service is set")
	}
}

func TestSettings_VoiceDisabled(t *testing.T) {
	srv := newTestServerWithVoice(nil)

	resp := srv.get("/api/settings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var result settingsResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.VoiceEnabled {
		t.Error("voice_enabled should be false when voice service is nil")
	}
}

type settingsResponse struct {
	CaCertAvailable bool   `json:"ca_cert_available"`
	WorkingDir      string `json:"working_dir"`
	NtfyConfigured  bool   `json:"ntfy_configured"`
	VoiceEnabled    bool   `json:"voice_enabled"`
}

var errVoiceTest = &voiceTestError{"whisper connection failed"}

type voiceTestError struct{ msg string }

func (e *voiceTestError) Error() string { return e.msg }

// newTestServerWithVoice creates a test server with an optional voice service.
func newTestServerWithVoice(vs interface {
	TranscribePCM(context.Context, []byte, int) (string, error)
	Deliver(context.Context, string, string, string, bool) error
	AutoSend() bool
}) *testServer {
	s := NewWithSwarm(nil, nil, nil, "127.0.0.1:0", nil, nil, "")
	if vs != nil {
		s.SetVoiceService(vs, true)
	}
	return &testServer{s: s}
}

type testServer struct {
	s *Server
}

func (ts *testServer) postJSON(path string, body []byte) *http.Response {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ts.s.server.Handler.ServeHTTP(rec, req)
	return rec.Result()
}

func (ts *testServer) get(path string) *http.Response {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	ts.s.server.Handler.ServeHTTP(rec, req)
	return rec.Result()
}
