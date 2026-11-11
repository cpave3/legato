package main

import (
	"context"
	"path/filepath"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// processSpeech is the Reeve's voice in this process. Its lines play here,
// on the host, where the Reeve's tools run while this process serves the
// broker: the Reeve's sandbox cannot reach the sound system.
var processSpeech *service.ReeveSpeech

// newReeveVoice makes the Reeve's voice, reading reeve.voice for each line
// and keeping each line's audio under the data folder until it has played.
func newReeveVoice(cfg *config.Config, db *store.Store, credentials *service.CredentialBroker) *service.ReeveSpeech {
	opts := service.ReeveSpeechOptions{
		Settings: func(ctx context.Context) config.ReeveVoiceConfig {
			effective, err := settingsFor(db).Effective(ctx)
			if err != nil || effective == nil {
				return config.ReeveVoiceConfig{}
			}
			return effective.Reeve.Voice
		},
		Dir: filepath.Join(config.ResolveDataDir(cfg), "reeve-voice"),
	}
	if credentials != nil {
		opts.Credentials = credentials
	}
	return service.NewReeveSpeech(opts)
}

// newVoiceService is transcription, and dictation's delivery to agents,
// when voice.enabled and voice.whisper_url are set at start; nil
// otherwise. Both settings take a restart.
func newVoiceService(cfg *config.Config, tmux service.TmuxManager, agents service.AgentService) *service.VoiceService {
	if !cfg.Voice.Enabled || cfg.Voice.WhisperURL == "" {
		return nil
	}
	var deliver service.VoiceAgents
	if agents != nil {
		deliver = agents
	}
	return service.NewVoiceService(cfg.Voice.WhisperURL, tmux, deliver, service.VoiceServiceOptions{
		AutoSend:  cfg.Voice.AutoSend,
		MicDevice: cfg.Voice.MicDevice,
	})
}

// wireReeveVoice serves dictation and hands-free voice with the Reeve on
// a web server. voice is nil when this process has no transcription, and
// reeve nil when it has no Reeve: the voice status then says why.
func wireReeveVoice(srv *server.Server, cfg *config.Config, db *store.Store, reeve *service.ReeveService, voice *service.VoiceService) *service.ReeveVoice {
	opts := service.ReeveVoiceOptions{
		Store: db,
		Settings: func(ctx context.Context) *config.Config {
			effective, err := settingsFor(db).Effective(ctx)
			if err != nil {
				return nil
			}
			return effective
		},
	}
	if voice != nil {
		srv.SetVoiceService(voice, cfg.Voice.AutoSend)
		opts.Transcription = voice
	}
	if reeve != nil {
		opts.Reeve = reeve
	}
	// The Reeve runs only in the sandbox, which takes a restart to turn on.
	sandboxOn := cfg.Agents.Sandbox.Enabled
	opts.CanStart = func(context.Context) (string, string) {
		if !sandboxOn {
			return "the sandbox is off", "agents.sandbox.enabled"
		}
		return "", ""
	}
	if processSpeech != nil {
		opts.Speech = processSpeech
	}
	rv := service.NewReeveVoice(opts)
	srv.SetReeveVoice(rv)
	return rv
}
