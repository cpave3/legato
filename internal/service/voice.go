package service

import (
	"context"
	"fmt"
	"log"
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

// VoiceAgents is what voice delivery needs of the agent service: whether an
// agent can be reached directly, sending to it that way, and its adapter.
type VoiceAgents interface {
	AdapterFor(kind string) AIToolAdapter
	HasChimeraEndpoint(ctx context.Context, taskID string) bool
	SendMessage(ctx context.Context, taskID, message string) error
}

// VoiceService orchestrates dictation: record audio, transcribe via a
// whisper-compatible endpoint, and deliver the text to an agent, over its
// native endpoint when it has one and through its tmux pane otherwise.
type VoiceService struct {
	whisper   WhisperClient
	recorder  Recorder
	tmux      TmuxManager
	agents    VoiceAgents
	autoSend  bool
	micDevice string
}

// VoiceServiceOptions configures the voice service.
type VoiceServiceOptions struct {
	AutoSend  bool
	MicDevice string
}

// NewVoiceService creates a voice service with a real whisper client and audio
// recorder. The whisperURL should be the full transcription endpoint.
func NewVoiceService(whisperURL string, tmux TmuxManager, agents VoiceAgents, opts VoiceServiceOptions) *VoiceService {
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

	return v.TranscribePCM(ctx, pcmData, 16000)
}

// TranscribePCM transcribes raw 16-bit PCM audio directly (without the
// recorder). Used by the web UI path where the browser captures audio and
// sends it as raw PCM bytes.
func (v *VoiceService) TranscribePCM(ctx context.Context, pcmData []byte, sampleRate int) (string, error) {
	text, err := v.whisper.Transcribe(ctx, pcmData, sampleRate)
	if err != nil {
		return "", fmt.Errorf("transcribing: %w", err)
	}
	return text, nil
}

// Deliver gives transcribed text to the agent of taskID.
//
// With autoSend the text is a message. An agent with a native endpoint (a
// Chimera session, or a Claude Code or Codex session through its bridge) gets
// it over that endpoint: an idle agent starts a turn on it and a busy one
// reads it at its next step, with no keystroke timing involved. An agent
// without one, or whose endpoint does not take the message, has it typed into
// its tmux pane, followed by the Enter keys its adapter asks for (1 by
// default, 2 for Codex).
//
// Without autoSend the text is a draft the person finishes at the terminal.
// It is always typed, with no Enter: an endpoint has no prompt box to leave
// it in.
func (v *VoiceService) Deliver(ctx context.Context, taskID, tmuxSession, agentKind, text string, autoSend bool) error {
	if text == "" {
		return nil
	}

	if autoSend && taskID != "" && v.agents != nil && v.agents.HasChimeraEndpoint(ctx, taskID) {
		err := v.sendNative(ctx, taskID, text)
		if err == nil {
			return nil
		}
		log.Printf("voice: native delivery to %s failed, typing into its pane instead: %v", taskID, err)
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

// sendNative sends over the agent's endpoint. Queued when the agent service
// can, so a message spoken during a turn joins that turn instead of being
// refused as a second prompt.
func (v *VoiceService) sendNative(ctx context.Context, taskID, text string) error {
	if queuer, ok := v.agents.(AgentMessageQueuer); ok {
		return queuer.QueueMessage(ctx, taskID, text)
	}
	return v.agents.SendMessage(ctx, taskID, text)
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
