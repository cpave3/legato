package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// voiceProbeTimeout is how long Whisper has to answer a describe before
// the voice is not ready.
const voiceProbeTimeout = time.Second

// VoiceProbe checks that transcription answers (VoiceService.Probe).
type VoiceProbe interface {
	Probe(ctx context.Context) error
}

// SpeechErrors is the last line the Reeve's voice failed to say
// (ReeveSpeech.LastError).
type SpeechErrors interface {
	LastError() string
}

// ReeveVoiceSessions is what the voice needs of the Reeve's sessions.
type ReeveVoiceSessions interface {
	Current(ctx context.Context) (*store.ReeveSession, error)
	Message(ctx context.Context, text, by string) error
	Resume(ctx context.Context, taskID string, in ReeveStart) (*store.ReeveSession, error)
}

// reeveVoicer is the part of the Reeve's sessions that gives a running
// Reeve its voice (ReeveService).
type reeveVoicer interface {
	StartedWithVoice(taskID string) (on, known bool)
	GiveVoice(ctx context.Context, taskID string) error
}

// ReeveVoiceOptions configures hands-free voice with the Reeve.
type ReeveVoiceOptions struct {
	Store *store.Store
	// Settings is the effective configuration, read on each call.
	Settings func(ctx context.Context) *config.Config
	// Transcription is this process's transcription; nil when it has
	// none (voice.enabled was off, or voice.whisper_url empty, at start).
	Transcription VoiceTranscription
	// Agents reads the Reeve's conversation and interrupts its turn; nil
	// leaves both out.
	Agents ReeveVoiceAgents
	// Poll is how often a session checks on the Reeve while it thinks
	// (500ms when 0).
	Poll time.Duration
	// Logf records a turn's timings and failures; log.Printf when nil.
	Logf func(format string, args ...any)
	// Reeve is this instance's Reeve; nil when it has none.
	Reeve ReeveVoiceSessions
	// Speech reports the voice's last failure; nil when unknown.
	Speech SpeechErrors
	// InstanceName is this instance's name; the host name when empty.
	InstanceName string
	// CanStart says why the Reeve could not start here, and the setting to
	// change ("" when it can); nil when nothing stops it.
	CanStart func(ctx context.Context) (why, setting string)
}

// ReeveVoice is hands-free voice with this instance's Reeve: whether it is
// ready (Status) and, later, the one voice session a device holds. See
// docs/ai/reeve.md § Hands-free voice.
type ReeveVoice struct {
	opts ReeveVoiceOptions
	// giving: a restart to give the Reeve its voice is under way.
	giving atomic.Bool

	mu      sync.Mutex
	session *VoiceSession
	device  string // the device holding the voice session, "" for none
	open    bool
}

var (
	errNoTranscription = errors.New("this Legato has no transcription")
	errNoReeve         = errors.New("this Legato has no Reeve")
)

// NewReeveVoice makes the Reeve's hands-free voice.
func NewReeveVoice(opts ReeveVoiceOptions) *ReeveVoice {
	if opts.InstanceName == "" {
		opts.InstanceName = instanceName()
	}
	return &ReeveVoice{opts: opts}
}

// VoiceReadiness is GET /api/reeve/voice: whether a spoken conversation
// with the Reeve will work here and, when not, why in one line, with the
// setting to change. It never holds the command line or the credential.
type VoiceReadiness struct {
	Instance      VoiceInstance           `json:"instance"`
	Reeve         VoiceReeveState         `json:"reeve"`
	Speech        VoiceSpeechState        `json:"speech"`
	Transcription VoiceTranscriptionState `json:"transcription"`
	Session       VoiceSessionState       `json:"session"`
	Ready         bool                    `json:"ready"`
	Reason        string                  `json:"reason"`
	Setting       string                  `json:"setting"`
}

// VoiceInstance is the instance answering: a client talks to the one
// whose role is main.
type VoiceInstance struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// VoiceReeveState is the Reeve's session: its state ("" for none) and
// whether its agent is in a turn.
type VoiceReeveState struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Busy  bool   `json:"busy"`
}

// VoiceSpeechState is the Reeve's voice: on, with a command, and the last
// failure.
type VoiceSpeechState struct {
	Enabled   bool   `json:"enabled"`
	Command   bool   `json:"command"`
	LastError string `json:"last_error"`
}

// VoiceTranscriptionState is this process's transcription, and whether
// Whisper answered.
type VoiceTranscriptionState struct {
	Enabled   bool `json:"enabled"`
	Reachable bool `json:"reachable"`
}

// VoiceSessionState is the open voice session, if any.
type VoiceSessionState struct {
	Open   bool   `json:"open"`
	Device string `json:"device"`
}

// Status says whether voice is ready. The reasons are checked in the
// design's order, and the first is given. A ready voice with a suspended
// Reeve starts resuming it, so the resume happens while the person speaks.
func (v *ReeveVoice) Status(ctx context.Context) (VoiceReadiness, error) {
	var out VoiceReadiness
	name := v.opts.InstanceName
	id, err := InstanceID(ctx, v.opts.Store)
	if err != nil {
		return out, err
	}
	out.Instance = VoiceInstance{ID: id, Name: name, Role: InstanceRole(ctx, v.opts.Store)}
	out.Reeve.Name = "The Reeve"

	cfg := &config.Config{}
	if v.opts.Settings != nil {
		if c := v.opts.Settings(ctx); c != nil {
			cfg = c
		}
	}
	out.Speech = VoiceSpeechState{Enabled: cfg.Reeve.Voice.Enabled, Command: strings.TrimSpace(cfg.Reeve.Voice.Command) != ""}
	if v.opts.Speech != nil {
		out.Speech.LastError = v.opts.Speech.LastError()
	}
	v.mu.Lock()
	out.Session = VoiceSessionState{Open: v.open, Device: v.device}
	v.mu.Unlock()

	var current *store.ReeveSession
	if v.opts.Reeve != nil {
		if current, err = v.opts.Reeve.Current(ctx); err != nil {
			return out, err
		}
	}
	if current != nil {
		out.Reeve.State = current.State
		if agent, err := v.opts.Store.GetAgentSessionByTaskID(ctx, current.TaskID); err == nil {
			out.Reeve.Busy = agent.Activity == "working"
		}
	}

	out.Transcription.Enabled = v.opts.Transcription != nil
	// Whether Whisper answered, when it was asked.
	answered := false
	asked := func() bool {
		answered = v.reachable(ctx)
		return answered
	}
	switch {
	case !cfg.Voice.Enabled || v.opts.Transcription == nil && strings.TrimSpace(cfg.Voice.WhisperURL) != "":
		out.notReady("Transcription is off on "+name, "voice.enabled")
	case strings.TrimSpace(cfg.Voice.WhisperURL) == "":
		out.notReady("Transcription is off on "+name, "voice.whisper_url")
	case !asked():
		out.notReady("Whisper isn't answering on "+name, "voice.whisper_url")
	case !cfg.Reeve.Voice.Enabled:
		out.notReady("The Reeve's voice is off on "+name, "reeve.voice.enabled")
	case !out.Speech.Command:
		out.notReady("The Reeve has no voice command on "+name, "reeve.voice.command")
	case v.opts.Reeve == nil:
		out.notReady("The Reeve can't start on "+name, "")
	default:
		out.Ready = true
		if v.opts.CanStart != nil {
			if why, setting := v.opts.CanStart(ctx); why != "" {
				out.notReady("The Reeve can't start on "+name+": "+why, setting)
			}
		}
	}
	out.Transcription.Reachable = answered
	if out.Ready && current != nil && current.State == store.ReeveSuspended {
		v.warmUp(current.TaskID)
	}
	// A Reeve started before its voice was on has neither the voice section
	// nor speak: it is restarted to take them up, between its turns.
	if voicer, ok := v.opts.Reeve.(reeveVoicer); ok && out.Ready && current != nil && current.State == store.ReeveRunning {
		if on, known := voicer.StartedWithVoice(current.TaskID); known && !on {
			out.notReady("The Reeve on "+name+" is getting its voice: try again in a moment", "")
			if !out.Reeve.Busy {
				v.giveVoice(voicer, current.TaskID)
			}
		}
	}
	return out, nil
}

func (r *VoiceReadiness) notReady(reason, setting string) {
	r.Ready, r.Reason, r.Setting = false, reason, setting
}

// reachable asks Whisper to describe itself within voiceProbeTimeout.
func (v *ReeveVoice) reachable(ctx context.Context) bool {
	if v.opts.Transcription == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, voiceProbeTimeout)
	defer cancel()
	return v.opts.Transcription.Probe(ctx) == nil
}

// giveVoice restarts the Reeve with its voice in the background, one
// restart at a time.
func (v *ReeveVoice) giveVoice(voicer reeveVoicer, taskID string) {
	if !v.giving.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer v.giving.Store(false)
		if err := voicer.GiveVoice(context.Background(), taskID); err != nil {
			log.Printf("reeve voice: giving the Reeve its voice: %v", err)
		}
	}()
}

// warmUp resumes a suspended Reeve in the background.
func (v *ReeveVoice) warmUp(taskID string) {
	go func() {
		if _, err := v.opts.Reeve.Resume(context.Background(), taskID, ReeveStart{StartedBy: "voice"}); err != nil {
			log.Printf("reeve voice: warming up the Reeve: %v", err)
		}
	}()
}
