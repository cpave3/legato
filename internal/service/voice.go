package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cpave3/legato/internal/engine/audio"
	"github.com/cpave3/legato/internal/engine/whisper"
)

// WhisperClient is the interface for transcribing audio to text. The concrete
// implementation is the engine/whisper package's Client, which speaks the
// Wyoming protocol to wyoming-faster-whisper.
type WhisperClient interface {
	Transcribe(ctx context.Context, pcmData []byte, sampleRate int) (string, error)
}

// Recorder is the interface for audio capture. The concrete implementation is
// the engine/audio package's Recorder.
type Recorder interface {
	IsRecording() bool
	Start(device string) error
	Stop() (string, error)
	Levels() []float64
	Cleanup()
}

// VoiceService orchestrates the voice-to-tmux flow: record audio, transcribe
// via a whisper-compatible endpoint, and deliver the text to an agent's tmux
// pane with adapter-appropriate trailing Enters.
type VoiceService struct {
	whisper  WhisperClient
	recorder Recorder
	tmux     TmuxManager
	agents   AgentService
	autoSend bool
	micDevice string
}

// VoiceServiceOptions configures the voice service.
type VoiceServiceOptions struct {
	AutoSend  bool
	MicDevice string
}

// NewVoiceService creates a voice service with a real whisper client and audio
// recorder. The whisperURL should be the full transcription endpoint.
func NewVoiceService(whisperURL string, tmux TmuxManager, agents AgentService, opts VoiceServiceOptions) *VoiceService {
	return &VoiceService{
		whisper:   whisper.NewClient(whisperURL),
		recorder:  audio.New(),
		tmux:      tmux,
		agents:    agents,
		autoSend:  opts.AutoSend,
		micDevice: opts.MicDevice,
	}
}

// StartRecording begins capturing audio from the configured mic device.
func (v *VoiceService) StartRecording(device string) error {
	if device == "" {
		device = v.micDevice
	}
	return v.recorder.Start(device)
}

// Stop stops the current recording and returns the audio file path. This is
// called by Transcribe internally; exposing it lets the TUI cancel a recording
// without transcribing.
func (v *VoiceService) Stop() (string, error) {
	return v.recorder.Stop()
}

// IsRecording reports whether audio capture is in progress.
func (v *VoiceService) IsRecording() bool {
	return v.recorder.IsRecording()
}

// Levels returns the current amplitude history for the TUI waveform display.
func (v *VoiceService) Levels() []float64 {
	return v.recorder.Levels()
}

// Transcribe stops recording (if active), reads the captured raw PCM audio,
// and sends it to the whisper server for transcription.
func (v *VoiceService) Transcribe(ctx context.Context) (string, error) {
	audioPath, err := v.recorder.Stop()
	if err != nil {
		return "", fmt.Errorf("stopping recording: %w", err)
	}
	defer v.recorder.Cleanup()

	pcmData, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("reading audio file: %w", err)
	}

	text, err := v.whisper.Transcribe(ctx, pcmData, 16000)
	if err != nil {
		return "", fmt.Errorf("transcribing: %w", err)
	}

	return text, nil
}

// Deliver sends transcribed text to the agent's tmux pane. When autoSend is
// true, it resolves the adapter for the given agentKind and sends the
// adapter-specified number of trailing Enter keys (default 1, codex uses 2).
// When autoSend is false, only the text is sent without any Enter keys.
func (v *VoiceService) Deliver(ctx context.Context, tmuxSession, agentKind, text string, autoSend bool) error {
	if text == "" {
		return nil
	}

	if err := v.tmux.SendKeys(tmuxSession, text); err != nil {
		return fmt.Errorf("sending text: %w", err)
	}

	if !autoSend {
		return nil
	}

	enters := 1 // default
	if v.agents != nil {
		if adapter := v.agents.AdapterFor(agentKind); adapter != nil {
			if va, ok := adapter.(VoiceDeliveryAdapter); ok {
				enters = va.VoiceTrailingEnters()
			}
		}
	}

	for i := 0; i < enters; i++ {
		if err := v.tmux.SendKey(tmuxSession, "Enter"); err != nil {
			return fmt.Errorf("sending Enter: %w", err)
		}
		time.Sleep(sendKeysInterCallGap)
	}

	return nil
}

// AutoSend returns the configured autoSend setting.
func (v *VoiceService) AutoSend() bool { return v.autoSend }

// MicDevice returns the configured mic device.
func (v *VoiceService) MicDevice() string { return v.micDevice }

// Cleanup releases audio recorder resources (temp files, etc.).
func (v *VoiceService) Cleanup() {
	if v.recorder != nil {
		v.recorder.Cleanup()
	}
}
