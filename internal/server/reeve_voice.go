package server

import (
	"context"
	"net/http"
	"os"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

// ReeveVoice is hands-free voice with the Reeve (service.ReeveVoice).
type ReeveVoice interface {
	Status(ctx context.Context) (service.VoiceReadiness, error)
}

// SetReeveVoice serves hands-free voice with the Reeve.
func (s *Server) SetReeveVoice(voice ReeveVoice) { s.reeveVoice = voice }

// reeveVoiceStatusHandler serves GET /api/reeve/voice: whether a spoken
// conversation will work here, and if not why, in one line.
func (s *Server) reeveVoiceStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.reeveVoice == nil {
			s.writeError(w, http.StatusServiceUnavailable, "this server has no voice for the Reeve")
			return
		}
		status, err := s.reeveVoice.Status(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, status)
	}
}

// VoiceClips gives out the audio of the Reeve's lines (service.ReeveVoice).
type VoiceClips interface {
	Clip(id string) (path, contentType string, ok bool)
}

// reeveVoiceLineHandler serves GET /api/reeve/voice/lines/{id}: a line's
// audio for the listening device, while it is kept.
func (s *Server) reeveVoiceLineHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clips, ok := s.reeveVoice.(VoiceClips)
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable, "this server has no voice for the Reeve")
			return
		}
		path, contentType, ok := clips.Clip(r.PathValue("id"))
		if !ok {
			s.writeError(w, http.StatusNotFound, "no such line: it has played, or is too old")
			return
		}
		f, err := os.Open(path)
		if err != nil {
			s.writeError(w, http.StatusNotFound, "no such line")
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			s.writeError(w, http.StatusNotFound, "no such line")
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "", info.ModTime(), f)
	}
}

// StartReeveSpokeEvents tells web clients what became of each line the
// Reeve said, for the said bubbles.
func (s *Server) StartReeveSpokeEvents() {
	if s.bus == nil {
		return
	}
	ch := s.bus.Subscribe(events.EventReeveSpoke)
	go func() {
		for ev := range ch {
			if p, ok := ev.Payload.(events.ReeveSpokePayload); ok {
				s.hub.Broadcast(WSMessage{Type: MsgReeveSpoke, TaskID: p.TaskID, Content: p.Text, Status: p.State})
			}
		}
	}()
}
