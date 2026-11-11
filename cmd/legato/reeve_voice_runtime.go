package main

import (
	"context"
	"path/filepath"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
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
