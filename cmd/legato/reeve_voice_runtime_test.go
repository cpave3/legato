package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// fakeWhisper answers Wyoming describe and transcribe with fixed text.
func fakeWhisper(t *testing.T, text string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadBytes('\n')
					if err != nil {
						return
					}
					var ev struct {
						Type          string `json:"type"`
						PayloadLength int    `json:"payload_length"`
					}
					_ = json.Unmarshal(line, &ev)
					if _, err := io.CopyN(io.Discard, r, int64(ev.PayloadLength)); err != nil {
						return
					}
					switch ev.Type {
					case "describe":
						conn.Write([]byte(`{"type":"info","data":{"asr":[]}}` + "\n"))
					case "audio-stop":
						conn.Write([]byte(`{"type":"transcript","data":{"text":"` + text + `"}}` + "\n"))
						return
					}
				}
			}()
		}
	}()
	return "tcp://" + ln.Addr().String()
}

func TestServeTranscribesAndReportsVoiceWhenVoiceIsOn(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{}
	cfg.Voice.Enabled = true
	cfg.Voice.WhisperURL = fakeWhisper(t, "hello reeve")

	srv := server.New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	wireReeveVoice(srv, cfg, db, nil, nil, newVoiceService(cfg, nil, nil))

	call := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := call(http.MethodPost, "/api/voice/transcribe", `{"transcription_only": true, "pcm": "AAAAAAAAAAA="}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"text":"hello reeve"`) {
		t.Fatalf("transcribe: %d %s", rec.Code, rec.Body)
	}
	rec = call(http.MethodGet, "/api/reeve/voice", "")
	var status service.VoiceReadiness
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	if !status.Transcription.Enabled {
		t.Fatalf("status: %+v", status)
	}
}

func TestServeHasNoTranscriptionWhenVoiceIsOff(t *testing.T) {
	cfg := &config.Config{}
	cfg.Voice.WhisperURL = "tcp://127.0.0.1:1"
	if newVoiceService(cfg, nil, nil) != nil {
		t.Fatal("a voice service with voice.enabled off")
	}
}
