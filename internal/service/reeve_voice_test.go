package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

type fakeVoiceProbe struct{ err error }

func (f fakeVoiceProbe) Probe(context.Context) error { return f.err }

type fakeSpeechErrors struct{ last string }

func (f fakeSpeechErrors) LastError() string { return f.last }

// readyVoiceConfig is a configuration with everything voice needs.
func readyVoiceConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Voice.Enabled = true
	cfg.Voice.WhisperURL = "tcp://whisper:10300"
	cfg.Reeve.Voice.Enabled = true
	cfg.Reeve.Voice.Command = "/home/me/bin/reeve-say.sh --radio"
	cfg.Reeve.Voice.Credential = "elevenlabs"
	return cfg
}

func newReeveVoiceFixture(t *testing.T, cfg *config.Config, probe VoiceProbe) (*ReeveVoice, *ReeveService, *store.Store, *fakeReeveAgents) {
	t.Helper()
	reeve, db, agents, _ := newReeveFixture(t)
	voice := NewReeveVoice(ReeveVoiceOptions{
		Store:         db,
		Settings:      func(context.Context) *config.Config { return cfg },
		Transcription: probe,
		Reeve:         reeve,
		Speech:        fakeSpeechErrors{},
		InstanceName:  "tessitura",
	})
	return voice, reeve, db, agents
}

func TestVoiceIsReadyWhenTranscriptionSpeechAndTheReeveAreAllThere(t *testing.T) {
	voice, _, _, _ := newReeveVoiceFixture(t, readyVoiceConfig(), fakeVoiceProbe{})
	got, err := voice.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ready || got.Reason != "" || got.Setting != "" {
		t.Fatalf("not ready: %+v", got)
	}
	if got.Instance.Name != "tessitura" || got.Instance.Role != "standalone" || !strings.HasPrefix(got.Instance.ID, "srv-") {
		t.Fatalf("instance: %+v", got.Instance)
	}
	if !got.Speech.Enabled || !got.Speech.Command || !got.Transcription.Enabled || !got.Transcription.Reachable {
		t.Fatalf("parts: %+v", got)
	}
}

func TestVoiceSaysWhyItIsNotReadyInOneLineAndNamesTheSetting(t *testing.T) {
	cases := []struct {
		name    string
		cfg     func(*config.Config)
		probe   VoiceProbe
		reason  string
		setting string
	}{
		{"no transcription in this process", nil, nil, "Transcription is off on tessitura", "voice.enabled"},
		{"no whisper address", func(c *config.Config) { c.Voice.WhisperURL = "" }, nil, "Transcription is off on tessitura", "voice.whisper_url"},
		{"whisper silent", nil, fakeVoiceProbe{err: errors.New("i/o timeout")}, "Whisper isn't answering on tessitura", "voice.whisper_url"},
		{"voice off", func(c *config.Config) { c.Reeve.Voice.Enabled = false }, fakeVoiceProbe{}, "The Reeve's voice is off on tessitura", "reeve.voice.enabled"},
		{"no command", func(c *config.Config) { c.Reeve.Voice.Command = " " }, fakeVoiceProbe{}, "The Reeve has no voice command on tessitura", "reeve.voice.command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := readyVoiceConfig()
			if tc.cfg != nil {
				tc.cfg(cfg)
			}
			voice, _, _, _ := newReeveVoiceFixture(t, cfg, tc.probe)
			got, err := voice.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.Ready || got.Reason != tc.reason || got.Setting != tc.setting {
				t.Fatalf("got ready=%v %q (%s), want %q (%s)", got.Ready, got.Reason, got.Setting, tc.reason, tc.setting)
			}
		})
	}
}

func TestWhisperIsReachableOnlyWhenItWasAskedAndAnswered(t *testing.T) {
	cfg := readyVoiceConfig()
	cfg.Voice.Enabled = false
	voice, _, _, _ := newReeveVoiceFixture(t, cfg, fakeVoiceProbe{})
	got, _ := voice.Status(context.Background())
	if !got.Transcription.Enabled || got.Transcription.Reachable {
		t.Fatalf("transcription switched off since start: %+v, want enabled but not reachable (never asked)", got.Transcription)
	}
}

func TestVoiceIsNotReadyWithoutAReeve(t *testing.T) {
	_, db, _, _ := newReeveFixture(t)
	voice := NewReeveVoice(ReeveVoiceOptions{Store: db, InstanceName: "tessitura",
		Settings: func(context.Context) *config.Config { return readyVoiceConfig() }, Transcription: fakeVoiceProbe{}})
	got, _ := voice.Status(context.Background())
	if got.Ready || got.Reason != "The Reeve can't start on tessitura" {
		t.Fatalf("got %+v", got)
	}
}

func TestTheVoiceStatusNeverGivesTheCommandOrTheCredential(t *testing.T) {
	voice, _, _, _ := newReeveVoiceFixture(t, readyVoiceConfig(), fakeVoiceProbe{})
	got, _ := voice.Status(context.Background())
	raw, _ := json.Marshal(got)
	text := strings.ToLower(string(raw))
	for _, secret := range []string{"reeve-say", "--radio", "elevenlabs", "whisper:10300"} {
		if strings.Contains(text, secret) {
			t.Fatalf("status gives %q: %s", secret, text)
		}
	}
}

func TestTheVoiceStatusWarmsUpASuspendedReeve(t *testing.T) {
	voice, reeve, db, agents := newReeveVoiceFixture(t, readyVoiceConfig(), fakeVoiceProbe{})
	ctx := context.Background()
	session, err := reeve.Start(ctx, ReeveStart{StartedBy: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetReeveSessionState(ctx, session.TaskID, store.ReeveSuspended); err != nil {
		t.Fatal(err)
	}
	got, err := voice.Status(ctx)
	if err != nil || !got.Ready || got.Reeve.State != store.ReeveSuspended {
		t.Fatalf("status: %+v %v", got, err)
	}
	waitUntil(t, func() bool {
		current, _ := db.CurrentReeveSession(ctx)
		return current != nil && current.State == store.ReeveRunning
	})
	// The row says running before the agent is spawned: Message takes the
	// lock the resume holds, so once it returns the spawn is done.
	_ = reeve.Message(ctx, "hello", "test")
	if len(agents.spawned) != 2 || agents.spawned[1].SessionPolicy != "resume" {
		t.Fatalf("not resumed: %+v", agents.spawned)
	}
}

func TestTheVoiceStatusDoesNotWarmUpWhenVoiceIsNotReady(t *testing.T) {
	cfg := readyVoiceConfig()
	cfg.Reeve.Voice.Enabled = false
	voice, reeve, db, agents := newReeveVoiceFixture(t, cfg, fakeVoiceProbe{})
	ctx := context.Background()
	session, _ := reeve.Start(ctx, ReeveStart{StartedBy: "web"})
	_ = db.SetReeveSessionState(ctx, session.TaskID, store.ReeveSuspended)
	if _, err := voice.Status(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if len(agents.spawned) != 1 {
		t.Fatalf("resumed though voice is off: %d spawns", len(agents.spawned))
	}
}

func TestTheVoiceStatusSaysWhetherTheReeveIsBusy(t *testing.T) {
	voice, reeve, db, _ := newReeveVoiceFixture(t, readyVoiceConfig(), fakeVoiceProbe{})
	ctx := context.Background()
	session, _ := reeve.Start(ctx, ReeveStart{StartedBy: "web"})
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: session.TaskID, TmuxSession: "legato-" + session.TaskID, Status: "running"}); err != nil {
		t.Fatal(err)
	}
	_ = db.UpdateAgentActivity(ctx, session.TaskID, "working")
	got, _ := voice.Status(ctx)
	if !got.Reeve.Busy || got.Reeve.State != store.ReeveRunning {
		t.Fatalf("reeve: %+v", got.Reeve)
	}
}

func TestTheVoiceStatusPassesOnTheSpeechError(t *testing.T) {
	reeve, db, _, _ := newReeveFixture(t)
	voice := NewReeveVoice(ReeveVoiceOptions{Store: db, InstanceName: "tessitura", Reeve: reeve,
		Settings: func(context.Context) *config.Config { return readyVoiceConfig() }, Transcription: fakeVoiceProbe{},
		Speech: fakeSpeechErrors{last: "reeve-say.sh: exit status 1"}})
	got, _ := voice.Status(context.Background())
	if got.Speech.LastError != "reeve-say.sh: exit status 1" {
		t.Fatalf("speech: %+v", got.Speech)
	}
}

// waitUntil waits up to two seconds for cond.
func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestVoiceIsNotReadyWhenTheReeveCannotStart(t *testing.T) {
	reeve, db, _, _ := newReeveFixture(t)
	voice := NewReeveVoice(ReeveVoiceOptions{Store: db, InstanceName: "tessitura", Reeve: reeve, Transcription: fakeVoiceProbe{},
		Settings: func(context.Context) *config.Config { return readyVoiceConfig() },
		CanStart: func(context.Context) (string, string) { return "the sandbox is off", "agents.sandbox.enabled" }})
	got, _ := voice.Status(context.Background())
	if got.Ready || got.Reason != "The Reeve can't start on tessitura: the sandbox is off" || got.Setting != "agents.sandbox.enabled" {
		t.Fatalf("got %+v", got)
	}
}
