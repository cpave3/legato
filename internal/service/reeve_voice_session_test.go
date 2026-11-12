package service

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// fakeTranscriber transcribes by the audio's length, so a test can tell
// which part of a turn was transcribed.
type fakeTranscriber struct {
	mu    sync.Mutex
	calls []int // bytes of each call
	text  func(bytes int) string
	gate  chan struct{}
}

func (f *fakeTranscriber) Probe(context.Context) error { return nil }
func (f *fakeTranscriber) TranscribePCM(ctx context.Context, pcm []byte, _ int) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, len(pcm))
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.text != nil {
		return f.text(len(pcm)), nil
	}
	return "turn the lights off", nil
}

func (f *fakeTranscriber) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeVoiceReeve is a running Reeve that records the messages it is sent.
type fakeVoiceReeve struct {
	mu       sync.Mutex
	messages []string
	by       []string
	// gate, when set, holds Message until it is closed: a Reeve resuming.
	gate chan struct{}
}

func (f *fakeVoiceReeve) Current(context.Context) (*store.ReeveSession, error) {
	return &store.ReeveSession{TaskID: "reeve-1", State: store.ReeveRunning}, nil
}
func (f *fakeVoiceReeve) Resume(context.Context, string, ReeveStart) (*store.ReeveSession, error) {
	return nil, nil
}
func (f *fakeVoiceReeve) Message(_ context.Context, text, by string) error {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, text)
	f.by = append(f.by, by)
	return nil
}
func (f *fakeVoiceReeve) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.messages...)
}

// fakeReeveVoiceAgents is the Reeve's conversation and its interrupts.
type fakeReeveVoiceAgents struct {
	mu          sync.Mutex
	tool        string
	interrupted []string
}

func (f *fakeReeveVoiceAgents) ConversationTail(context.Context, string, int) (*ConversationTail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tool == "" {
		return &ConversationTail{}, nil
	}
	return &ConversationTail{Entries: []ConversationEntry{{Kind: EntryTool, Tool: "Read", Text: f.tool}}}, nil
}
func (f *fakeReeveVoiceAgents) interrupts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.interrupted...)
}

func (f *fakeReeveVoiceAgents) InterruptAgent(_ context.Context, taskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interrupted = append(f.interrupted, taskID)
	return nil
}

type voiceFixture struct {
	reads   atomic.Int64 // settings read
	voice   *ReeveVoice
	db      *store.Store
	reeve   *fakeVoiceReeve
	agents  *fakeReeveVoiceAgents
	whisper *fakeTranscriber
	cfg     *config.Config
	cfgMu   sync.Mutex
}

func newVoiceFixture(t *testing.T) *voiceFixture {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "reeve-1", TmuxSession: "legato-reeve-1", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	f := &voiceFixture{db: db, reeve: &fakeVoiceReeve{}, agents: &fakeReeveVoiceAgents{}, whisper: &fakeTranscriber{}, cfg: readyVoiceConfig()}
	off := false
	f.cfg.Reeve.Voice.Partials = &off
	f.voice = NewReeveVoice(ReeveVoiceOptions{
		Store: db, InstanceName: "tessitura", Reeve: f.reeve, Agents: f.agents, Transcription: f.whisper,
		Settings: func(context.Context) *config.Config {
			f.reads.Add(1)
			f.cfgMu.Lock()
			defer f.cfgMu.Unlock()
			c := *f.cfg
			return &c
		},
		Poll: 5 * time.Millisecond,
		Logf: func(string, ...any) {},
	})
	return f
}

func (f *voiceFixture) setActivity(t *testing.T, activity string) {
	t.Helper()
	if err := f.db.UpdateAgentActivity(context.Background(), "reeve-1", activity); err != nil {
		t.Fatal(err)
	}
}

// voiceDevice is a device's end of a session: what it was sent.
type voiceDevice struct {
	session *VoiceSession
	got     chan VoiceOut
	mu      sync.Mutex
	all     []VoiceOut
}

func (f *voiceFixture) open(name string) *voiceDevice {
	d := &voiceDevice{got: make(chan VoiceOut, 4096)}
	d.session = f.voice.Open(VoiceHello{Device: name, Client: "web", Play: true}, func(out VoiceOut) {
		d.mu.Lock()
		d.all = append(d.all, out)
		d.mu.Unlock()
		d.got <- out
	})
	return d
}

// expect waits for a message the device was sent that matches, skipping
// the others.
func (d *voiceDevice) expect(t *testing.T, what string, match func(VoiceOut) bool) VoiceOut {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case out := <-d.got:
			if match(out) {
				return out
			}
		case <-timeout:
			d.mu.Lock()
			defer d.mu.Unlock()
			t.Fatalf("no %s; the device got %+v", what, d.all)
		}
	}
}

func (d *voiceDevice) expectState(t *testing.T, state string) VoiceOut {
	t.Helper()
	return d.expect(t, "state "+state, func(o VoiceOut) bool { return o.Type == "state" && o.State == state })
}

func (d *voiceDevice) expectType(t *testing.T, typ string) VoiceOut {
	t.Helper()
	return d.expect(t, typ, func(o VoiceOut) bool { return o.Type == typ })
}

// sentNo checks that no message of type typ was sent so far.
func (d *voiceDevice) sentNo(t *testing.T, typ string) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, o := range d.all {
		if o.Type == typ {
			t.Fatalf("the device was sent %+v", o)
		}
	}
}

// speak sends ms of a loud tone, and quiet sends ms of silence, in 20 ms
// frames as a device does.
func (d *voiceDevice) speak(ms int) { d.frames(ms, 4000) }
func (d *voiceDevice) quiet(ms int) { d.frames(ms, 0) }

func (d *voiceDevice) frames(ms int, amplitude float64) {
	for i := 0; i < ms/20; i++ {
		frame := make([]byte, 640)
		for n := 0; n < 320; n++ {
			v := int16(amplitude * math.Sin(2*math.Pi*220*float64(i*320+n)/16000))
			binary.LittleEndian.PutUint16(frame[2*n:], uint16(v))
		}
		d.session.Audio(frame)
	}
}

func TestAVoiceTurnEndsAfterAPauseAndIsSentToTheReeve(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("Cameron's desktop")
	d.expectState(t, VoiceIdle)

	d.session.Handle(VoiceIn{Type: "listen"})
	d.expectState(t, VoiceListening)
	d.speak(1000)
	d.quiet(1200)

	pause := d.expectType(t, "pause")
	if pause.Of != 1000 {
		t.Fatalf("pause bar of %d ms, want the 1 s pause", pause.Of)
	}
	d.expectState(t, VoiceThinking)
	heard := d.expectType(t, "heard")
	if heard.Text != "turn the lights off" {
		t.Fatalf("heard %q", heard.Text)
	}
	waitUntil(t, func() bool { return len(f.reeve.sent()) == 1 })
	if got := f.reeve.sent()[0]; !strings.HasSuffix(got, "turn the lights off") {
		t.Fatalf("the Reeve was sent %q", got)
	}
	if f.reeve.by[0] != "voice:Cameron's desktop" {
		t.Fatalf("sent by %q", f.reeve.by[0])
	}
}

func TestTheTurnUsesTheTextTranscribedAtTheSentenceBoundary(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(1200)
	d.expectType(t, "heard")
	if n := f.whisper.count(); n != 1 {
		t.Fatalf("Whisper was asked %d times, want once: at the boundary, its text reused", n)
	}
}

func TestSpeechThatResumesDropsTheTextFromTheBoundary(t *testing.T) {
	f := newVoiceFixture(t)
	f.whisper.text = func(bytes int) string { return fmt.Sprintf("%d bytes", bytes) }
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(600) // past the 400 ms boundary, short of the 1 s pause
	d.speak(800)
	d.quiet(1200)

	heard := d.expectType(t, "heard")
	if heard.Text == fmt.Sprintf("%d bytes", f.whisper.calls[0]) {
		t.Fatalf("the turn was sent as the text from before the speech resumed: %q", heard.Text)
	}
	if n := f.whisper.count(); n != 2 {
		t.Fatalf("Whisper was asked %d times, want 2", n)
	}
	var b int
	fmt.Sscanf(heard.Text, "%d bytes", &b)
	if b < (1000+600+800)*32 {
		t.Fatalf("the turn sent covers %d bytes, not the whole turn", b)
	}
	d.sentNo(t, "ended")
}

func TestSendEndsTheTurnAtOnce(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(800)
	d.session.Handle(VoiceIn{Type: "send"})
	d.expectState(t, VoiceThinking)
	d.expectType(t, "heard")
	waitUntil(t, func() bool { return len(f.reeve.sent()) == 1 })
}

func TestCancelDropsTheTurn(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(800)
	d.session.Handle(VoiceIn{Type: "cancel"})
	d.expect(t, "idle, cancelled", func(o VoiceOut) bool { return o.Type == "state" && o.Reason == VoiceReasonCancelled })
	d.quiet(1200)
	time.Sleep(50 * time.Millisecond)
	if len(f.reeve.sent()) != 0 {
		t.Fatalf("a cancelled turn was sent: %v", f.reeve.sent())
	}
}

func TestNoSpeechGoesBackToIdle(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.expectState(t, VoiceListening)
	d.quiet(8200)
	if idle := d.expectState(t, VoiceIdle); idle.Reason != VoiceReasonNoSpeech {
		t.Fatalf("idle for %q", idle.Reason)
	}
	if f.whisper.count() != 0 {
		t.Fatal("silence was transcribed")
	}
}

func TestAudioIsIgnoredWhileNotListening(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.speak(1000)
	d.quiet(1200)
	time.Sleep(50 * time.Millisecond)
	d.sentNo(t, "heard")
	if f.whisper.count() != 0 {
		t.Fatal("audio was transcribed before listen")
	}
}

func TestANewSessionTakesOverTheOpenOne(t *testing.T) {
	f := newVoiceFixture(t)
	desk := f.open("desk")
	phone := f.open("phone")
	ended := desk.expectType(t, "ended")
	if ended.Reason != "taken over by phone" {
		t.Fatalf("ended for %q", ended.Reason)
	}
	select {
	case <-desk.session.Done():
	case <-time.After(time.Second):
		t.Fatal("the old session is still open")
	}
	status, _ := f.voice.Status(context.Background())
	if !status.Session.Open || status.Session.Device != "phone" {
		t.Fatalf("session: %+v", status.Session)
	}
	phone.session.Close()
	status, _ = f.voice.Status(context.Background())
	if status.Session.Open {
		t.Fatalf("closed session still open: %+v", status.Session)
	}
}

func TestSpeechRightAfterASendIsASecondMessage(t *testing.T) {
	f := newVoiceFixture(t)
	f.whisper.text = func(bytes int) string { return fmt.Sprintf("%d bytes", bytes) }
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(1100)
	d.expectType(t, "heard")
	d.speak(800) // the person goes on, before the Reeve answers
	d.quiet(1200)
	d.expectType(t, "heard")
	waitUntil(t, func() bool { return len(f.reeve.sent()) == 2 })
}

func TestSpeechLongAfterASendIsNotListenedTo(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(1100)
	d.expectType(t, "heard")
	d.quiet(3000)
	d.speak(800)
	d.quiet(1200)
	time.Sleep(50 * time.Millisecond)
	if n := len(f.reeve.sent()); n != 1 {
		t.Fatalf("%d messages sent", n)
	}
}

func TestWhileTheReeveThinksTheDeviceIsToldWhatItIsDoingThenThatItAnsweredInWriting(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(1100)
	d.expectType(t, "heard")

	f.agents.mu.Lock()
	f.agents.tool = "Reading config.yaml\nmore"
	f.agents.mu.Unlock()
	f.setActivity(t, "working")
	if got := d.expectType(t, "activity"); got.Text != "Reading config.yaml" {
		t.Fatalf("activity %q", got.Text)
	}
	f.setActivity(t, "waiting")
	d.expectType(t, "written")
	d.expectState(t, VoiceIdle)
}

func TestStopWhileThinkingInterruptsTheReeve(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(1100)
	d.expectState(t, VoiceThinking)
	waitUntil(t, func() bool { return len(f.reeve.sent()) == 1 })
	d.session.Handle(VoiceIn{Type: "stop"})
	d.expectState(t, VoiceIdle)
	waitUntil(t, func() bool { return len(f.agents.interrupts()) == 1 })
	if got := f.agents.interrupts(); got[0] != "reeve-1" {
		t.Fatalf("interrupted %v", got)
	}
}

func TestStopBeforeTheTurnIsSentInterruptsNothing(t *testing.T) {
	f := newVoiceFixture(t)
	f.whisper.gate = make(chan struct{})
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(1100)
	d.expectState(t, VoiceThinking)
	// Still transcribing: nothing has reached the Reeve, whose own work
	// (a typed task) must go on.
	d.session.Handle(VoiceIn{Type: "stop"})
	d.expectState(t, VoiceIdle)
	close(f.whisper.gate)
	time.Sleep(100 * time.Millisecond)
	if got := f.reeve.sent(); len(got) != 0 {
		t.Fatalf("a stopped turn reached the Reeve: %q", got)
	}
	if got := f.agents.interrupts(); len(got) != 0 {
		t.Fatalf("interrupted %v, though the turn was never sent", got)
	}
}

func TestStopWhileTheTurnIsBeingDeliveredInterruptsOnceItLands(t *testing.T) {
	f := newVoiceFixture(t)
	f.reeve.gate = make(chan struct{})
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(1100)
	d.expectState(t, VoiceThinking)
	d.expectType(t, "heard")
	// The Reeve is still resuming when the person stops.
	d.session.Handle(VoiceIn{Type: "stop"})
	d.expectState(t, VoiceIdle)
	if got := f.agents.interrupts(); len(got) != 0 {
		t.Fatalf("interrupted %v before the turn reached the Reeve", got)
	}
	close(f.reeve.gate)
	waitUntil(t, func() bool { return len(f.agents.interrupts()) == 1 })
}

func TestTheSettingsAreReadOncePerTurnNotPerFrame(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.expectState(t, VoiceListening)
	before := f.reads.Load()
	d.quiet(3000)
	d.speak(2000)
	time.Sleep(50 * time.Millisecond)
	if n := f.reads.Load() - before; n > 2 {
		t.Fatalf("settings read %d times in 5 s of audio, want once per turn", n)
	}
}

func TestPartialWordsAreSentWhileThePersonSpeaks(t *testing.T) {
	f := newVoiceFixture(t)
	f.cfg.Reeve.Voice.Partials = nil
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(2500)
	if got := d.expectType(t, "partial"); got.Text == "" {
		t.Fatal("an empty partial")
	}
}

func TestByeEndsTheSession(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "bye"})
	select {
	case <-d.session.Done():
	case <-time.After(time.Second):
		t.Fatal("still open")
	}
}

// lineAudio writes a line's audio as the voice command would.
func lineAudio(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "line.mp3")
	if err := os.WriteFile(path, []byte("ID3"+text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// askAndAnswer runs a turn up to the Reeve working on it.
func (f *voiceFixture) askAndAnswer(t *testing.T, d *voiceDevice) {
	t.Helper()
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(1100)
	d.expectType(t, "heard")
	f.setActivity(t, "working")
	waitUntil(t, f.voice.AnsweringVoice)
}

func TestTheReevesLineGoesToTheListeningDeviceAndFollowUpListensForAReply(t *testing.T) {
	f := newVoiceFixture(t)
	f.cfg.Reeve.Voice.FollowUp = "1s"
	d := f.open("desk")
	f.askAndAnswer(t, d)

	path := lineAudio(t, "The lights are off.")
	if route := f.voice.Deliver(SpokenLine{TaskID: "reeve-1", Text: "The lights are off."}, path, "audio/mpeg"); route != RouteDevice {
		t.Fatalf("route %v", route)
	}
	line := d.expectType(t, "line")
	if line.Text != "The lights are off." || line.URL != VoiceLinePath+line.ID || len(line.ID) != 32 {
		t.Fatalf("line %+v", line)
	}
	d.expectState(t, VoiceSpeaking)
	if got, typ, ok := f.voice.Clip(line.ID); !ok || got != path || typ != "audio/mpeg" {
		t.Fatalf("clip %s %s %v", got, typ, ok)
	}

	// Audio while the line plays is not listened to.
	d.session.Handle(VoiceIn{Type: "playing", Line: line.ID})
	d.speak(1000)
	d.quiet(1100)
	f.setActivity(t, "waiting")
	d.session.Handle(VoiceIn{Type: "played", Line: line.ID})
	d.expectState(t, VoiceListening)
	if _, _, ok := f.voice.Clip(line.ID); ok {
		t.Fatal("a played line's audio is kept")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a played line's file is kept")
	}
	if n := len(f.reeve.sent()); n != 1 {
		t.Fatalf("speech during the line was sent: %d messages", n)
	}
	// Nobody replies: the follow-up window closes.
	d.quiet(1100)
	if idle := d.expectState(t, VoiceIdle); idle.Reason != VoiceReasonNoSpeech {
		t.Fatalf("idle for %q", idle.Reason)
	}
}

func TestWithNoFollowUpTheSessionGoesIdleAfterSpeaking(t *testing.T) {
	f := newVoiceFixture(t)
	f.cfg.Reeve.Voice.FollowUp = "0s"
	d := f.open("desk")
	f.askAndAnswer(t, d)
	f.voice.Deliver(SpokenLine{Text: "Done."}, lineAudio(t, "Done."), "audio/mpeg")
	line := d.expectType(t, "line")
	f.setActivity(t, "waiting")
	time.Sleep(30 * time.Millisecond)
	d.session.Handle(VoiceIn{Type: "played", Line: line.ID})
	d.expectState(t, VoiceIdle)
	d.sentNo(t, "written")
}

func TestALineAnsweringAClosedSessionIsDroppedAndOtherwisePlaysOnTheHost(t *testing.T) {
	f := newVoiceFixture(t)
	if route := f.voice.Deliver(SpokenLine{Text: "hi"}, lineAudio(t, "hi"), "audio/mpeg"); route != RouteHost {
		t.Fatalf("with no session and no voice turn: %v", route)
	}
	d := f.open("desk")
	f.askAndAnswer(t, d)
	d.session.Close()
	if route := f.voice.Deliver(SpokenLine{Text: "late"}, lineAudio(t, "late"), "audio/mpeg"); route != RouteDrop {
		t.Fatalf("after the session closed mid-turn: %v", route)
	}
}

func TestADeviceThatCannotPlayLeavesLinesToTheHost(t *testing.T) {
	f := newVoiceFixture(t)
	d := &voiceDevice{got: make(chan VoiceOut, 1024)}
	d.session = f.voice.Open(VoiceHello{Device: "watch", Play: false}, func(o VoiceOut) { d.got <- o })
	if route := f.voice.Deliver(SpokenLine{Text: "hi"}, lineAudio(t, "hi"), "audio/mpeg"); route != RouteHost {
		t.Fatalf("route %v", route)
	}
}

func TestADeviceThatCannotPlayStillHearsTheAnswerToItsTurnOnTheHost(t *testing.T) {
	f := newVoiceFixture(t)
	d := &voiceDevice{got: make(chan VoiceOut, 4096)}
	d.session = f.voice.Open(VoiceHello{Device: "watch", Play: false}, func(o VoiceOut) { d.got <- o })
	f.askAndAnswer(t, d)
	if route := f.voice.Deliver(SpokenLine{Text: "The lights are off."}, lineAudio(t, "x"), "audio/mpeg"); route != RouteHost {
		t.Fatalf("the answer to its own turn went %v, want the host", route)
	}
}

func TestALineThatAnswersNoSpokenTurnPlaysOnTheHostThoughASessionIsOpen(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	d.expectState(t, VoiceIdle)
	if route := f.voice.Deliver(SpokenLine{Text: "The build passed."}, lineAudio(t, "x"), "audio/mpeg"); route != RouteHost {
		t.Fatalf("an idle session took a line it does not want: %v", route)
	}
}

func TestStopBeforeTheTurnIsSentDropsNoLaterLines(t *testing.T) {
	f := newVoiceFixture(t)
	f.whisper.gate = make(chan struct{})
	d := f.open("desk")
	d.session.Handle(VoiceIn{Type: "listen"})
	d.speak(1000)
	d.quiet(1100)
	d.expectState(t, VoiceThinking)
	d.session.Handle(VoiceIn{Type: "stop"})
	d.expectState(t, VoiceIdle)
	close(f.whisper.gate)
	if route := f.voice.Deliver(SpokenLine{Text: "The build passed."}, lineAudio(t, "x"), "audio/mpeg"); route != RouteHost {
		t.Fatalf("a line of the Reeve's own work after a turn that never reached it: %v", route)
	}
}

func TestAfterStopTheReeveIsNoLongerAnsweredOnceItStops(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	f.askAndAnswer(t, d)
	d.session.Handle(VoiceIn{Type: "stop"})
	d.expectState(t, VoiceIdle)
	if route := f.voice.Deliver(SpokenLine{Text: "Stopping."}, lineAudio(t, "x"), "audio/mpeg"); route != RouteDrop {
		t.Fatalf("a line of the stopped turn: %v, want dropped", route)
	}
	// The interrupted Reeve stops; what it says later answers something else.
	f.setActivity(t, "waiting")
	waitUntil(t, func() bool { return !f.voice.AnsweringVoice() })
	if route := f.voice.Deliver(SpokenLine{Text: "The build passed."}, lineAudio(t, "x"), "audio/mpeg"); route != RouteHost {
		t.Fatalf("a later line: %v, want the host", route)
	}
}

func TestStopWhileSpeakingDropsTheLinesLeft(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	f.askAndAnswer(t, d)
	first := lineAudio(t, "one")
	f.voice.Deliver(SpokenLine{Text: "one"}, first, "audio/mpeg")
	d.expectState(t, VoiceSpeaking)
	d.session.Handle(VoiceIn{Type: "stop"})
	d.expectState(t, VoiceThinking)
	waitUntil(t, func() bool { _, err := os.Stat(first); return os.IsNotExist(err) })
	if route := f.voice.Deliver(SpokenLine{Text: "two"}, lineAudio(t, "two"), "audio/mpeg"); route != RouteDrop {
		t.Fatalf("a line after stop: %v", route)
	}
	f.setActivity(t, "waiting")
	d.expectState(t, VoiceIdle)
}

func TestALinesAudioIsKeptForTenMinutesAtMost(t *testing.T) {
	f := newVoiceFixture(t)
	now := time.Now()
	var mu sync.Mutex
	f.voice.opts.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	d := f.open("desk")
	f.askAndAnswer(t, d)
	path := lineAudio(t, "old")
	f.voice.Deliver(SpokenLine{Text: "old"}, path, "audio/mpeg")
	line := d.expectType(t, "line")
	mu.Lock()
	now = now.Add(11 * time.Minute)
	mu.Unlock()
	if _, _, ok := f.voice.Clip(line.ID); ok {
		t.Fatal("kept past ten minutes")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the file is kept")
	}
}

func TestAnEndedSessionsLinesAreRemoved(t *testing.T) {
	f := newVoiceFixture(t)
	d := f.open("desk")
	f.askAndAnswer(t, d)
	path := lineAudio(t, "bye")
	f.voice.Deliver(SpokenLine{Text: "bye"}, path, "audio/mpeg")
	line := d.expectType(t, "line")
	d.session.Close()
	if _, _, ok := f.voice.Clip(line.ID); ok {
		t.Fatal("kept after the session ended")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the file is kept")
	}
}
