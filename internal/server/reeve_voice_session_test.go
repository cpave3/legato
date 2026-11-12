package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"nhooyr.io/websocket"
)

type voiceFixtureMessages struct {
	Device []json.RawMessage `json:"device"`
	Main   []json.RawMessage `json:"main"`
}

// Every message in the shared fixture is one the server's types read and
// write without losing or adding a field.
func TestTheVoiceSessionMessagesMatchTheSharedFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/voice-session/messages.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture voiceFixtureMessages
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	roundTrip := func(raw json.RawMessage, into any) {
		t.Helper()
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		again, _ := json.Marshal(into)
		var want, got map[string]any
		_ = json.Unmarshal(raw, &want)
		_ = json.Unmarshal(again, &got)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s came back as %s", raw, again)
		}
	}
	for _, raw := range fixture.Device {
		roundTrip(raw, &service.VoiceIn{})
	}
	for _, raw := range fixture.Main {
		roundTrip(raw, &service.VoiceOut{})
	}
}

type voiceTurnFixture struct {
	Transcript string `json:"transcript"`
	Steps      []struct {
		Send   json.RawMessage `json:"send"`
		Expect json.RawMessage `json:"expect"`
		Audio  *struct {
			SpeechMs int `json:"speech_ms"`
			QuietMs  int `json:"quiet_ms"`
		} `json:"audio"`
	} `json:"steps"`
}

type fixedTranscription struct{ text string }

func (f fixedTranscription) Probe(context.Context) error { return nil }
func (f fixedTranscription) TranscribePCM(context.Context, []byte, int) (string, error) {
	return f.text, nil
}

type recordingReeve struct {
	mu   sync.Mutex
	sent []string
}

func (r *recordingReeve) Current(context.Context) (*store.ReeveSession, error) {
	return &store.ReeveSession{TaskID: "reeve-1", State: store.ReeveRunning}, nil
}
func (r *recordingReeve) Resume(context.Context, string, service.ReeveStart) (*store.ReeveSession, error) {
	return nil, nil
}
func (r *recordingReeve) Message(_ context.Context, text, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, text)
	return nil
}

func voiceSessionServer(t *testing.T, transcript string) (*Server, *recordingReeve, string) {
	t.Helper()
	srv, db := reeveServer(t)
	reeve := &recordingReeve{}
	off := false
	cfg := &config.Config{}
	cfg.Reeve.Voice.Partials = &off
	srv.SetReeveVoice(service.NewReeveVoice(service.ReeveVoiceOptions{
		Store: db, Reeve: reeve, Transcription: fixedTranscription{text: transcript},
		Settings: func(context.Context) *config.Config { return cfg },
		Poll:     10 * time.Millisecond, Logf: func(string, ...any) {},
	}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, reeve, "ws" + strings.TrimPrefix(ts.URL, "http") + voiceSessionPath
}

func voiceFrames(ms int, amplitude float64) [][]byte {
	var frames [][]byte
	for i := 0; i < ms/20; i++ {
		frame := make([]byte, 640)
		for n := 0; n < 320; n++ {
			v := int16(amplitude * math.Sin(2*math.Pi*220*float64(i*320+n)/16000))
			binary.LittleEndian.PutUint16(frame[2*n:], uint16(v))
		}
		frames = append(frames, frame)
	}
	return frames
}

// The shared turn fixture, played over a real WebSocket.
func TestAVoiceSessionCarriesATurnFromTheMicrophoneToTheReeve(t *testing.T) {
	data, err := os.ReadFile("testdata/voice-session/turn.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture voiceTurnFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	_, reeve, url := voiceSessionServer(t, fixture.Transcript)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url+"?token=secret", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	for i, step := range fixture.Steps {
		switch {
		case step.Send != nil:
			if err := conn.Write(ctx, websocket.MessageText, step.Send); err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
		case step.Audio != nil:
			frames := voiceFrames(step.Audio.SpeechMs, 4000)
			frames = append(frames, voiceFrames(step.Audio.QuietMs, 0)...)
			for _, f := range frames {
				if err := conn.Write(ctx, websocket.MessageBinary, f); err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
			}
		case step.Expect != nil:
			var want map[string]any
			_ = json.Unmarshal(step.Expect, &want)
			for {
				_, msg, err := conn.Read(ctx)
				if err != nil {
					t.Fatalf("step %d: waiting for %s: %v", i, step.Expect, err)
				}
				var got map[string]any
				_ = json.Unmarshal(msg, &got)
				if got["type"] == "pause" || got["type"] == "partial" {
					continue
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("step %d: got %s, want %s", i, msg, step.Expect)
				}
				break
			}
		}
	}
	reeve.mu.Lock()
	defer reeve.mu.Unlock()
	if len(reeve.sent) != 1 || !strings.HasSuffix(reeve.sent[0], fixture.Transcript) {
		t.Fatalf("the Reeve was sent %q", reeve.sent)
	}
}

func TestAVoiceSessionNeedsTheWebToken(t *testing.T) {
	_, _, url := voiceSessionServer(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, url+"?token=wrong", nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dial without the token: %v %v", resp, err)
	}
}

func TestAVoiceSessionStartsWithHello(t *testing.T) {
	_, _, url := voiceSessionServer(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url+"?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"listen"}`))
	if _, _, err := conn.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("a session without hello: %v", err)
	}
}

func TestALinesAudioIsServedToTheDeviceWithItsType(t *testing.T) {
	srv, db := reeveServer(t)
	voice := service.NewReeveVoice(service.ReeveVoiceOptions{Store: db, Logf: func(string, ...any) {},
		Transcription: fixedTranscription{text: "say hi"}, Reeve: &recordingReeve{}})
	srv.SetReeveVoice(voice)
	got := make(chan service.VoiceOut, 1024)
	session := voice.Open(service.VoiceHello{Device: "desk", Play: true}, func(o service.VoiceOut) { got <- o })
	defer session.Close()
	path := t.TempDir() + "/line.wav"
	if err := os.WriteFile(path, []byte("RIFF\x00\x00\x00\x00WAVEfmt "), 0o600); err != nil {
		t.Fatal(err)
	}
	// A line answering the device's spoken turn goes to it.
	session.Handle(service.VoiceIn{Type: "listen"})
	for _, f := range append(voiceFrames(1000, 4000), voiceFrames(1200, 0)...) {
		session.Audio(f)
	}
	deadline := time.After(5 * time.Second)
	for !voice.AnsweringVoice() {
		select {
		case <-deadline:
			t.Fatal("the turn was not delivered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if route := voice.Deliver(service.SpokenLine{Text: "hi"}, path, "audio/wav"); route != service.RouteDevice {
		t.Fatalf("route %v", route)
	}
	var line service.VoiceOut
	for line.Type != "line" {
		select {
		case line = <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("no line")
		}
	}
	rec := callAs(srv, "secret", http.MethodGet, line.URL, "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/wav" || !strings.HasPrefix(rec.Body.String(), "RIFF") {
		t.Fatalf("line: %d %s %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if rec := callAs(srv, "secret", http.MethodGet, service.VoiceLinePath+"00000000000000000000000000000000", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown line: %d", rec.Code)
	}
	if rec := callAs(srv, "wrong", http.MethodGet, line.URL, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the token: %d", rec.Code)
	}
}
