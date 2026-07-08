package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubRecorder is a test double for the audio recorder interface.
type stubRecorder struct {
	recording bool
	levels   []float64
	audioPath string
	audioData []byte
	startErr  error
}

func (s *stubRecorder) IsRecording() bool { return s.recording }
func (s *stubRecorder) Start(device string) error {
	if s.startErr != nil {
		return s.startErr
	}
	s.recording = true
	return nil
}
func (s *stubRecorder) Stop() (string, error) {
	s.recording = false
	if s.audioPath != "" {
		if err := os.WriteFile(s.audioPath, s.audioData, 0644); err != nil {
			return "", err
		}
	}
	return s.audioPath, nil
}
func (s *stubRecorder) Levels() []float64 { return s.levels }
func (s *stubRecorder) Cleanup()            {}

// stubWhisperClient is a test double for the whisper transcription client.
type stubWhisperClient struct {
	text string
	err  error
}

func (s *stubWhisperClient) Transcribe(ctx context.Context, pcmData []byte, sampleRate int) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.text, nil
}

func TestVoiceService_DeliverNoAutoSendSendsTextOnly(t *testing.T) {
	mt := newMockTmux()
	mt.sessions["legato-TASK-1"] = true

	vs := &VoiceService{
		tmux:     mt,
		agents:   nil,
		autoSend:  false,
	}

	err := vs.Deliver(context.Background(), "legato-TASK-1", "shell", "hello agent", false)
	if err != nil {
		t.Fatal(err)
	}

	raw := mt.sentRawFor("legato-TASK-1")
	if len(raw) != 1 || raw[0] != "hello agent" {
		t.Errorf("sent raw = %v, want [hello agent]", raw)
	}
	keys := mt.sentKeysFor("legato-TASK-1")
	if len(keys) != 0 {
		t.Errorf("sent keys = %v, want none (autoSend=false)", keys)
	}
}

func TestVoiceService_DeliverAutoSendDefaultOneEnter(t *testing.T) {
	mt := newMockTmux()
	mt.sessions["legato-TASK-1"] = true

	adpt := &fakeAdapter{name: "claude-code"}
	agents := &agentService{
		tmux:     mt,
		adapter:  adpt,
		adapters: map[string]AIToolAdapter{"claude-code": adpt},
	}

	vs := &VoiceService{
		tmux:     mt,
		agents:   agents,
		autoSend:  true,
	}

	err := vs.Deliver(context.Background(), "legato-TASK-1", "claude-code", "hello agent", true)
	if err != nil {
		t.Fatal(err)
	}

	raw := mt.sentRawFor("legato-TASK-1")
	if len(raw) != 1 || raw[0] != "hello agent" {
		t.Errorf("sent raw = %v, want [hello agent]", raw)
	}
	keys := mt.sentKeysFor("legato-TASK-1")
	if len(keys) != 1 || keys[0] != "Enter" {
		t.Errorf("sent keys = %v, want [Enter] (default 1 Enter)", keys)
	}
}

func TestVoiceService_DeliverAutoSendCodexTwoEnters(t *testing.T) {
	mt := newMockTmux()
	mt.sessions["legato-TASK-1"] = true

	adpt := &codexDoubleEnterAdapter{fakeAdapter{name: "codex"}}
	agents := &agentService{
		tmux:     mt,
		adapter:  adpt,
		adapters: map[string]AIToolAdapter{"codex": adpt},
	}

	vs := &VoiceService{
		tmux:     mt,
		agents:   agents,
		autoSend:  true,
	}

	err := vs.Deliver(context.Background(), "legato-TASK-1", "codex", "hello agent", true)
	if err != nil {
		t.Fatal(err)
	}

	raw := mt.sentRawFor("legato-TASK-1")
	if len(raw) != 1 || raw[0] != "hello agent" {
		t.Errorf("sent raw = %v, want [hello agent]", raw)
	}
	keys := mt.sentKeysFor("legato-TASK-1")
	if len(keys) != 2 {
		t.Errorf("sent keys = %v, want 2 Enters (codex)", keys)
	}
	for i, k := range keys {
		if k != "Enter" {
			t.Errorf("key[%d] = %q, want %q", i, k, "Enter")
		}
	}
}

func TestVoiceService_TranscribeFlow(t *testing.T) {
	mt := newMockTmux()
	mt.sessions["legato-TASK-1"] = true

	rec := &stubRecorder{
		audioPath: filepath.Join(t.TempDir(), "recording.wav"),
		audioData: fakeWAVData(),
	}
	wc := &stubWhisperClient{text: "hello from voice"}

	vs := &VoiceService{
		whisper:  wc,
		recorder: rec,
		tmux:     mt,
		autoSend: false,
	}

	if err := vs.StartRecording("default"); err != nil {
		t.Fatal(err)
	}
	if !vs.IsRecording() {
		t.Error("should be recording after StartRecording")
	}

	text, err := vs.Transcribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello from voice" {
		t.Errorf("got %q, want %q", text, "hello from voice")
	}
	if vs.IsRecording() {
		t.Error("should not be recording after Transcribe")
	}
}

func TestVoiceService_Levels(t *testing.T) {
	rec := &stubRecorder{levels: []float64{0.1, 0.5, 0.9}}
	vs := &VoiceService{recorder: rec}
	levels := vs.Levels()
	if len(levels) != 3 || levels[2] != 0.9 {
		t.Errorf("Levels() = %v, want [0.1 0.5 0.9]", levels)
	}
}

func TestVoiceService_DeliverEmptyTextNoOp(t *testing.T) {
	mt := newMockTmux()
	mt.sessions["legato-TASK-1"] = true

	vs := &VoiceService{tmux: mt, autoSend: false}
	err := vs.Deliver(context.Background(), "legato-TASK-1", "shell", "", false)
	if err != nil {
		t.Fatalf("empty text should not error: %v", err)
	}
	if len(mt.sentRawFor("legato-TASK-1")) != 0 {
		t.Error("should not send anything for empty text")
	}
}

func TestVoiceService_TranscribePCM(t *testing.T) {
	wc := &stubWhisperClient{text: "hello from web"}

	vs := &VoiceService{
		whisper:  wc,
		autoSend: false,
	}

	text, err := vs.TranscribePCM(context.Background(), fakeWAVData(), 16000)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello from web" {
		t.Errorf("got %q, want %q", text, "hello from web")
	}
}

func TestVoiceService_TranscribePCMError(t *testing.T) {
	wc := &stubWhisperClient{err: fmt.Errorf("connection refused")}

	vs := &VoiceService{
		whisper:  wc,
		autoSend: false,
	}

	_, err := vs.TranscribePCM(context.Background(), fakeWAVData(), 16000)
	if err == nil {
		t.Fatal("expected error from whisper client")
	}
}

// codexDoubleEnterAdapter is a fake adapter that implements VoiceDeliveryAdapter.
type codexDoubleEnterAdapter struct {
	fakeAdapter
}

func (a *codexDoubleEnterAdapter) VoiceTrailingEnters() int { return 2 }

// fakeWAVData returns a minimal valid WAV header + silence for testing.
func fakeWAVData() []byte {
	return []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x44\xac\x00\x00\x88\x58\x01\x00\x02\x00\x10\x00data\x00\x00\x00\x00")
}

// silence unused import warning
var _ = time.Second
