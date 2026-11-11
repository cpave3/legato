package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
)

// fakeVoiceKeys hands out one key and records each use.
type fakeVoiceKeys struct {
	mu   sync.Mutex
	key  string
	uses []CredentialUse
}

func (f *fakeVoiceKeys) UseForLegato(_ context.Context, use CredentialUse) (credential.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uses = append(f.uses, use)
	return credential.Secret{Type: credential.TypeValue, Value: []byte(f.key)}, nil
}

// speechFixture is a queue with a fake command and player that write
// what they did to log files.
type speechFixture struct {
	speech *ReeveSpeech
	dir    string
	made   string // the command's log: text|key per line
	played string // the player's log: start/end per line
	keys   *fakeVoiceKeys
	voice  config.ReeveVoiceConfig
	mu     sync.Mutex
	logged []string
}

func script(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newSpeechFixture(t *testing.T) *speechFixture {
	t.Helper()
	bin := t.TempDir()
	f := &speechFixture{dir: filepath.Join(t.TempDir(), "voice"), made: filepath.Join(bin, "made.log"), played: filepath.Join(bin, "played.log"), keys: &fakeVoiceKeys{key: "sk-secret-key"}}
	command := script(t, bin, "say.sh", fmt.Sprintf(`if [ "$1" = "fail" ]; then echo "refused for key $ELEVENLABS_API_KEY" >&2; exit 3; fi
printf '%%s|%%s|%%s\n' "$1" "$ELEVENLABS_API_KEY" "$2" >> %q
printf '%%s' "$1" > "$2"
`, f.made))
	player := script(t, bin, "play.sh", fmt.Sprintf(`echo "start $(cat "$1")" >> %[1]q
sleep 0.2
echo "end $(cat "$1")" >> %[1]q
`, f.played))
	f.voice = config.ReeveVoiceConfig{Enabled: true, Command: command, Credential: "elevenlabs", Player: player}
	f.speech = NewReeveSpeech(ReeveSpeechOptions{
		Settings: func(context.Context) config.ReeveVoiceConfig {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.voice
		},
		Credentials: f.keys,
		Dir:         f.dir,
		Logf: func(format string, args ...any) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.logged = append(f.logged, fmt.Sprintf(format, args...))
		},
	})
	return f
}

func (f *speechFixture) say(t *testing.T, text string) {
	t.Helper()
	if err := f.speech.Say(context.Background(), SpokenLine{TaskID: "reeve-1", ExecutionID: "ex-1", Text: text}); err != nil {
		t.Fatalf("say %q: %v", text, err)
	}
}

func readLines(path string) []string {
	data, _ := os.ReadFile(path)
	return strings.Fields(strings.ReplaceAll(string(data), " ", "_"))
}

// waitForPlayed waits until the player has logged n lines.
func (f *speechFixture) waitForPlayed(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if lines := readLines(f.played); len(lines) >= n {
			return lines
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("played %v, want %d log lines; logged %v", readLines(f.played), n, f.logs())
	return nil
}

func (f *speechFixture) logs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.logged...)
}

func TestSpeechSaysLinesInOrderOneAtATime(t *testing.T) {
	f := newSpeechFixture(t)

	start := time.Now()
	f.say(t, "first")
	f.say(t, "second")
	if took := time.Since(start); took > 150*time.Millisecond {
		t.Fatalf("Say waited %v for the audio", took)
	}

	played := f.waitForPlayed(t, 4)
	want := []string{"start_first", "end_first", "start_second", "end_second"}
	if strings.Join(played, ",") != strings.Join(want, ",") {
		t.Fatalf("played %v, want %v", played, want)
	}
}

func TestSpeechGivesTheCommandTheTextAnOutFileAndTheCredential(t *testing.T) {
	f := newSpeechFixture(t)

	f.say(t, "hello there")
	f.waitForPlayed(t, 2)

	data, _ := os.ReadFile(f.made)
	parts := strings.Split(strings.TrimSpace(string(data)), "|")
	if len(parts) != 3 || parts[0] != "hello there" || parts[1] != "sk-secret-key" {
		t.Fatalf("the command was run as %q", data)
	}
	if filepath.Dir(parts[2]) != f.dir || filepath.Ext(parts[2]) != ".mp3" {
		t.Fatalf("out file %q, want an .mp3 in %s", parts[2], f.dir)
	}
	f.keys.mu.Lock()
	defer f.keys.mu.Unlock()
	if len(f.keys.uses) != 1 || f.keys.uses[0].Credential != "elevenlabs" || f.keys.uses[0].TaskID != "reeve-1" || f.keys.uses[0].ExecutionID != "ex-1" {
		t.Fatalf("credential uses %+v", f.keys.uses)
	}
}

// Audio is not kept: each file goes once played, and files left by a
// process that stopped mid-line go when the queue starts.
func TestSpeechKeepsNoAudio(t *testing.T) {
	f := newSpeechFixture(t)
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(f.dir, "left-over.mp3"), []byte("x"), 0o600)

	f.say(t, "one")
	f.say(t, "fail")
	f.say(t, "two")
	f.waitForPlayed(t, 4)

	deadline := time.Now().Add(2 * time.Second)
	for {
		entries, _ := os.ReadDir(f.dir)
		if len(entries) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("audio kept: %v", entries)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A line whose audio cannot be made is logged, without the key, and the
// next is still said.
func TestSpeechLogsAFailedLineAndCarriesOn(t *testing.T) {
	f := newSpeechFixture(t)

	f.say(t, "fail")
	f.say(t, "after")

	played := f.waitForPlayed(t, 2)
	if strings.Join(played, ",") != "start_after,end_after" {
		t.Fatalf("played %v", played)
	}
	logs := strings.Join(f.logs(), "\n")
	if !strings.Contains(logs, "exit status 3") {
		t.Fatalf("the failure was not logged: %q", logs)
	}
	if strings.Contains(logs, "sk-secret-key") {
		t.Fatalf("the key reached the log: %q", logs)
	}
}

func TestSpeechRefusesALongLine(t *testing.T) {
	f := newSpeechFixture(t)

	err := f.speech.Say(context.Background(), SpokenLine{TaskID: "reeve-1", Text: strings.Repeat("word ", 100)})

	if err == nil || !strings.Contains(err.Error(), "say less") {
		t.Fatalf("err = %v", err)
	}
	if err := f.speech.Say(context.Background(), SpokenLine{TaskID: "reeve-1", Text: "  "}); err == nil {
		t.Fatal("an empty line was queued")
	}
}

// Turning the voice off mutes lines already queued.
func TestSpeechTurnedOffMutesTheQueue(t *testing.T) {
	f := newSpeechFixture(t)

	f.say(t, "first")
	f.say(t, "second")
	f.waitForPlayed(t, 1)
	f.mu.Lock()
	f.voice.Enabled = false
	f.mu.Unlock()
	time.Sleep(500 * time.Millisecond)

	if played := readLines(f.played); strings.Join(played, ",") != "start_first,end_first" {
		t.Fatalf("played %v after the voice was turned off", played)
	}
}

func TestSpeechRemembersItsLastFailureUntilALinePlays(t *testing.T) {
	f := newSpeechFixture(t)
	if got := f.speech.LastError(); got != "" {
		t.Fatalf("a new voice has an error: %q", got)
	}
	f.say(t, "fail")
	waitUntil(t, func() bool { return f.speech.LastError() != "" })
	got := f.speech.LastError()
	if !strings.Contains(got, "exit status 3") || strings.Contains(got, "sk-secret-key") {
		t.Fatalf("last error %q", got)
	}
	f.say(t, "after")
	f.waitForPlayed(t, 2)
	waitUntil(t, func() bool { return f.speech.LastError() == "" })
}
