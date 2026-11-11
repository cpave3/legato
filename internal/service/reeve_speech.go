package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
)

// MaxSpokenChars is the longest line the Reeve may say: speech is the
// short version, and each character of a paid voice costs.
const MaxSpokenChars = 400

const (
	// speechQueueSize is how many lines may wait to be said.
	speechQueueSize = 16
	// makeTimeout bounds making one line's audio, playTimeout playing it.
	makeTimeout = time.Minute
	playTimeout = 5 * time.Minute
)

// SpokenLine is one line the Reeve says out loud, and the session that
// said it.
type SpokenLine struct {
	TaskID      string
	ExecutionID string
	Text        string
}

// ReeveVoiceCredentials reads the credential the voice command is given
// (CredentialBroker.UseForLegato).
type ReeveVoiceCredentials interface {
	UseForLegato(ctx context.Context, use CredentialUse) (credential.Secret, error)
}

// ReeveSpeechOptions configures the Reeve's voice.
type ReeveSpeechOptions struct {
	// Settings is reeve.voice, read for each line.
	Settings func(ctx context.Context) config.ReeveVoiceConfig
	// Credentials gives the command reeve.voice.credential; nil when this
	// Legato has no credential broker.
	Credentials ReeveVoiceCredentials
	// Dir holds a line's audio until it has played. It is Legato's own:
	// whatever is in it when the queue starts is removed.
	Dir string
	// Logf records a line that was not said; log.Printf when nil.
	Logf func(format string, args ...any)
	// Spoke is told what became of each line (EventReeveSpoke); nil for
	// no one.
	Spoke func(SpokeEvent)
	// Now is the time, for quiet hours; time.Now when nil.
	Now func() time.Time
}

// LineRoute is where a made line goes.
type LineRoute int

const (
	// RouteHost plays it on this machine's speakers.
	RouteHost LineRoute = iota
	// RouteDevice: the listening device has it, and its file.
	RouteDevice
	// RouteDrop: it answered a voice message nobody listens to any more.
	RouteDrop
)

// SpeechOutput is the device the person listens on (ReeveVoice).
type SpeechOutput interface {
	// AnsweringVoice reports whether the Reeve is answering a spoken
	// message, whose lines play even in quiet hours.
	AnsweringVoice() bool
	// Deliver takes a made line. With RouteDevice it owns path from then.
	Deliver(line SpokenLine, path, contentType string) LineRoute
}

// The states of a line, in SpokeEvent.
const (
	SpokeMade    = "made"
	SpokePlayed  = "played"
	SpokeFailed  = "failed"
	SpokeSent    = "sent"
	SpokeDropped = "dropped"
)

// SpokeEvent is what became of a line: made, sent to the listening
// device, played on the host, failed, or dropped.
type SpokeEvent struct {
	TaskID string `json:"task_id"`
	Text   string `json:"text"`
	State  string `json:"state"`
}

// errQuietHours is speak's answer in quiet hours.
var errQuietHours = errors.New("quiet hours: reply in writing (reeve.voice.quiet_hours)")

// ReeveSpeech says the Reeve's lines out loud on this machine, one at a
// time and in order: a command makes each line's audio while the line
// before it plays, and a player plays it. It runs in the host Legato, the
// one process that can reach the sound system.
type ReeveSpeech struct {
	opts  ReeveSpeechOptions
	lines chan SpokenLine
	start sync.Once
	seq   atomic.Int64

	mu      sync.Mutex
	lastErr string
	// The line lastErr is about: only a newer line playing clears it.
	lastErrLine int64
	output      SpeechOutput
}

// SetOutput gives the voice the device the person listens on.
func (s *ReeveSpeech) SetOutput(out SpeechOutput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output = out
}

func (s *ReeveSpeech) currentOutput() SpeechOutput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.output
}

func (s *ReeveSpeech) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now()
}

func (s *ReeveSpeech) spoke(line SpokenLine, state string) {
	if s.opts.Spoke != nil {
		s.opts.Spoke(SpokeEvent{TaskID: line.TaskID, Text: line.Text, State: state})
	}
}

// madeLine is a line whose audio is ready to play; n is its place in the
// queue.
type madeLine struct {
	line SpokenLine
	path string
	n    int64
}

// NewReeveSpeech makes the Reeve's voice. Nothing runs until the first
// line.
func NewReeveSpeech(opts ReeveSpeechOptions) *ReeveSpeech {
	if opts.Logf == nil {
		opts.Logf = log.Printf
	}
	return &ReeveSpeech{opts: opts, lines: make(chan SpokenLine, speechQueueSize)}
}

// Enabled reports whether the voice is on (reeve.voice.enabled).
func (s *ReeveSpeech) Enabled(ctx context.Context) bool {
	return s.opts.Settings(ctx).Enabled
}

// Say puts a line in the queue and returns at once. A line that is empty,
// too long, or finds the queue full is refused.
func (s *ReeveSpeech) Say(_ context.Context, line SpokenLine) error {
	line.Text = strings.TrimSpace(line.Text)
	if line.Text == "" {
		return errors.New("give the text to say")
	}
	if n := utf8.RuneCountInString(line.Text); n > MaxSpokenChars {
		return fmt.Errorf("%d characters is too long to say: say less, in %d characters at most, and leave the detail to your written reply", n, MaxSpokenChars)
	}
	if s.opts.Settings(context.Background()).QuietAt(s.now()) {
		if out := s.currentOutput(); out == nil || !out.AnsweringVoice() {
			return errQuietHours
		}
	}
	s.start.Do(s.run)
	select {
	case s.lines <- line:
		return nil
	default:
		return errors.New("too many lines are waiting to be said: wait for them, or say less")
	}
}

// LastError is why the last line was not said, "" once a line has played
// since. It holds no text the Reeve said and no key.
func (s *ReeveSpeech) LastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// failed records how line n went: an error, or nil when it played, which
// clears an error about an older line only.
func (s *ReeveSpeech) failed(n int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.lastErr, s.lastErrLine = err.Error(), n
		return
	}
	if n >= s.lastErrLine {
		s.lastErr = ""
	}
}

// run clears the audio folder and starts the queue's two workers.
func (s *ReeveSpeech) run() {
	if err := os.RemoveAll(s.opts.Dir); err != nil {
		s.opts.Logf("reeve voice: clearing %s: %v", s.opts.Dir, err)
	}
	ready := make(chan madeLine, 1)
	go s.makeLines(ready)
	go s.playLines(ready)
}

func (s *ReeveSpeech) makeLines(ready chan<- madeLine) {
	var n int64
	for line := range s.lines {
		n++
		voice := s.opts.Settings(context.Background())
		if !voice.Enabled {
			continue
		}
		path, err := s.make(voice, line)
		if err != nil {
			s.failed(n, err)
			s.spoke(line, SpokeFailed)
			s.opts.Logf("reeve voice: a line of %d characters was not said: %v", utf8.RuneCountInString(line.Text), err)
			continue
		}
		path, contentType := nameByType(path)
		s.spoke(line, SpokeMade)
		route := RouteHost
		if out := s.currentOutput(); out != nil {
			route = out.Deliver(line, path, contentType)
		}
		switch {
		case route == RouteDevice:
			s.failed(n, nil)
			s.spoke(line, SpokeSent)
			continue
		case route == RouteDrop:
			_ = os.Remove(path)
			s.spoke(line, SpokeDropped)
			continue
		case voice.QuietAt(s.now()):
			_ = os.Remove(path)
			s.spoke(line, SpokeDropped)
			s.opts.Logf("reeve voice: a line was not played on the host: quiet hours")
			continue
		}
		ready <- madeLine{line: line, path: path, n: n}
	}
}

func (s *ReeveSpeech) playLines(ready <-chan madeLine) {
	for made := range ready {
		voice := s.opts.Settings(context.Background())
		if !voice.Enabled {
			// Turned off since the line was made.
			s.spoke(made.line, SpokeDropped)
		} else {
			if err := s.play(voice, made.path); err != nil {
				s.failed(made.n, err)
				s.spoke(made.line, SpokeFailed)
				s.opts.Logf("reeve voice: a line was not played: %v", err)
			} else {
				s.failed(made.n, nil)
				s.spoke(made.line, SpokePlayed)
			}
		}
		_ = os.Remove(made.path)
	}
}

// make runs the voice command for one line, with its credential, and
// returns the audio file it wrote.
func (s *ReeveSpeech) make(voice config.ReeveVoiceConfig, line SpokenLine) (string, error) {
	command := strings.Fields(voice.Command)
	if len(command) == 0 {
		return "", errors.New("reeve.voice.command is not set")
	}
	command[0] = expandHome(command[0])
	if err := os.MkdirAll(s.opts.Dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(s.opts.Dir, fmt.Sprintf("line-%d.mp3", s.seq.Add(1)))
	ctx, cancel := context.WithTimeout(context.Background(), makeTimeout)
	defer cancel()

	env := soundEnv()
	var key string
	if name := strings.TrimSpace(voice.Credential); name != "" {
		if s.opts.Credentials == nil {
			return "", fmt.Errorf("reeve.voice.credential names %s, and this Legato has no credentials", name)
		}
		secret, err := s.opts.Credentials.UseForLegato(ctx, CredentialUse{TaskID: line.TaskID, ExecutionID: line.ExecutionID, Credential: name,
			Operation: "read", Amounts: map[string]int64{"characters": int64(utf8.RuneCountInString(line.Text))}, Call: "the Reeve's voice"})
		if err != nil {
			return "", fmt.Errorf("credential %s: %w", name, err)
		}
		key = string(secret.Value)
		env = append(env, voice.CredentialVariable()+"="+key)
	}

	cmd := exec.CommandContext(ctx, command[0], append(command[1:], line.Text, path)...)
	cmd.Env = env
	if strings.Contains(command[0], "/") {
		// A script finds the files beside it from its own folder.
		cmd.Dir = filepath.Dir(command[0])
	}
	if out, err := runQuiet(cmd); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("%s: %w%s", filepath.Base(command[0]), err, redacted(out, key))
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		_ = os.Remove(path)
		return "", fmt.Errorf("%s wrote no audio to its out-file", filepath.Base(command[0]))
	}
	return path, nil
}

// play plays one file with reeve.voice.player, else pw-play, else ffplay.
func (s *ReeveSpeech) play(voice config.ReeveVoiceConfig, path string) error {
	players := [][]string{{"pw-play"}, {"ffplay", "-nodisp", "-autoexit", "-loglevel", "error"}}
	if fields := strings.Fields(voice.Player); len(fields) > 0 {
		fields[0] = expandHome(fields[0])
		players = [][]string{fields}
	}
	var failures []string
	for _, player := range players {
		ctx, cancel := context.WithTimeout(context.Background(), playTimeout)
		cmd := exec.CommandContext(ctx, player[0], append(player[1:], path)...)
		cmd.Env = soundEnv()
		out, err := runQuiet(cmd)
		cancel()
		if err == nil {
			return nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v%s", filepath.Base(player[0]), err, redacted(out, "")))
	}
	return errors.New(strings.Join(failures, "; "))
}

// runQuiet runs a command and returns the end of what it wrote.
func runQuiet(cmd *exec.Cmd) (string, error) {
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	// A child that keeps the output open does not hold the queue.
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if len(text) > 500 {
		text = "…" + text[len(text)-500:]
	}
	return text, err
}

// redacted is a command's output for the log, without the key.
func redacted(out, key string) string {
	if out == "" {
		return ""
	}
	if key != "" {
		out = strings.ReplaceAll(out, key, "[credential]")
	}
	return " (" + out + ")"
}

// soundEnv is this process's environment, with XDG_RUNTIME_DIR, where the
// sound server's socket is, when the process was started without it.
func soundEnv() []string {
	env := os.Environ()
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		dir := fmt.Sprintf("/run/user/%d", os.Getuid())
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			env = append(env, "XDG_RUNTIME_DIR="+dir)
		}
	}
	return env
}

// expandHome turns a leading ~/ into the home directory.
func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}
