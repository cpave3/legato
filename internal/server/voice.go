package server

import (
	"context"
	"encoding/json"
	"net/http"
)

// VoiceService is the interface the server uses for voice transcription and
// delivery. The concrete implementation is service.VoiceService.
type VoiceService interface {
	TranscribePCM(ctx context.Context, pcm []byte, sampleRate int) (string, error)
	Deliver(ctx context.Context, tmuxSession, agentKind, text string, autoSend bool) error
	AutoSend() bool
}

// voiceTranscribeRequest is the JSON body for POST /api/voice/transcribe.
type voiceTranscribeRequest struct {
	AgentID           string `json:"agent_id"`
	AgentKind         string `json:"agent_kind"`
	TranscriptionOnly bool   `json:"transcription_only"`
	// PCM is the raw 16-bit little-endian PCM audio (base64-encoded in JSON).
	PCM []byte `json:"pcm"`
}

// voiceTranscribeResponse is the JSON response from POST /api/voice/transcribe.
type voiceTranscribeResponse struct {
	Text  string `json:"text"`
	Error string `json:"error,omitempty"`
}

func (s *Server) voiceTranscribeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.voiceSvc == nil {
			http.Error(w, "voice not enabled", http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req voiceTranscribeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if req.AgentID == "" && !req.TranscriptionOnly {
			http.Error(w, "agent_id is required", http.StatusBadRequest)
			return
		}

		if len(req.PCM) == 0 {
			http.Error(w, "pcm audio data is required", http.StatusBadRequest)
			return
		}

		text, err := s.voiceSvc.TranscribePCM(r.Context(), req.PCM, 16000)
		resp := voiceTranscribeResponse{}
		if err != nil {
			resp.Error = err.Error()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
			return
		}

		resp.Text = text
		if req.TranscriptionOnly {
			s.writeJSON(w, http.StatusOK, resp)
			return
		}

		session := "legato-" + req.AgentID
		if err := s.voiceSvc.Deliver(r.Context(), session, req.AgentKind, text, s.voiceAutoSend); err != nil {
			resp.Text = ""
			resp.Error = err.Error()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}
}
