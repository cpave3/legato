package service

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/endofturn"
)

// The states a voice session reports to its device.
const (
	VoiceIdle      = "idle"
	VoiceListening = "listening"
	VoiceThinking  = "thinking"
	VoiceSpeaking  = "speaking"
)

const (
	voiceSampleRate = 16000
	// voiceBytesPerSecond is 16 kHz mono s16le.
	voiceBytesPerSecond = voiceSampleRate * 2
	// voiceNoSpeech is how long a turn waits for speech to begin.
	voiceNoSpeech = 8 * time.Second
	// voiceResumeWindow: speech that starts this soon after a turn was
	// sent, before the Reeve answers, is sent as a second message.
	voiceResumeWindow = 2 * time.Second
	// voicePartialEvery and voicePartialsFor bound partial words: about
	// once a second, for a turn's first 20 seconds.
	voicePartialEvery = time.Second
	voicePartialsFor  = 20 * time.Second
	// voiceAnswerWait is how long the session waits for the Reeve to start
	// on a turn before it gives up on an answer.
	voiceAnswerWait = 2 * time.Minute
	// voiceMaxFrame is the largest audio frame a device may send.
	voiceMaxFrame = voiceBytesPerSecond
)

// VoiceTranscription is transcription for hands-free voice
// (VoiceService): checked by the status route, used by the session.
type VoiceTranscription interface {
	VoiceProbe
	TranscribePCM(ctx context.Context, pcm []byte, sampleRate int) (string, error)
}

// ReeveVoiceAgents is what a voice session needs of the agent service: the
// Reeve's conversation, for what it is doing, and interrupting its turn.
type ReeveVoiceAgents interface {
	ConversationTail(ctx context.Context, taskID string, limit int) (*ConversationTail, error)
	InterruptAgent(ctx context.Context, taskID string) error
}

// VoiceHello is who opened a voice session: the device's name, the kind
// of client (desktop, android, web), and whether it can play lines.
type VoiceHello struct {
	Device string `json:"device"`
	Client string `json:"client"`
	Play   bool   `json:"play"`
}

// VoiceIn is a message from the device (a JSON text frame).
type VoiceIn struct {
	Type   string `json:"type"`
	Device string `json:"device,omitempty"`
	Client string `json:"client,omitempty"`
	Play   bool   `json:"play,omitempty"`
	Line   string `json:"line,omitempty"`
}

// VoiceOut is a message to the device (a JSON text frame).
type VoiceOut struct {
	Type   string `json:"type"`
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	Text   string `json:"text,omitempty"`
	// Ms of Of: the pause bar.
	Ms int64 `json:"ms,omitempty"`
	Of int64 `json:"of,omitempty"`
	// ID and URL: a line to play.
	ID  string `json:"id,omitempty"`
	URL string `json:"url,omitempty"`
}

// The reasons a session goes idle or ends.
const (
	VoiceReasonNoSpeech     = "no_speech"
	VoiceReasonNothingHeard = "nothing_heard"
	VoiceReasonCancelled    = "cancelled"
	VoiceReasonNoAnswer     = "no_answer"
	VoiceReasonFailed       = "failed"
	VoiceReasonBye          = "bye"
)

// voiceInput is one thing the session's loop handles.
type voiceInput struct {
	audio      []byte
	msg        *VoiceIn
	transcript *voiceTranscript
	delivered  *voiceDelivery
	line       *VoiceLine
	end        string
}

// The kinds of transcription a session asks for.
const (
	transcribePartial = iota
	transcribeSpeculative
	transcribeFinal
)

type voiceTranscript struct {
	kind  int
	turn  int
	gen   int
	bytes int
	text  string
	err   error
}

type voiceDelivery struct {
	turn int
	err  error
}

// voiceTimings is one turn's times, logged when it is over.
type voiceTimings struct {
	speechEnd, transcript, delivered, firstLine, lineMade, playing time.Time
}

// VoiceSession is one device's spoken conversation with the Reeve. Every
// change to its state happens on its loop, fed by Audio, Handle and the
// work it starts, so it needs no locks of its own.
type VoiceSession struct {
	v     *ReeveVoice
	hello VoiceHello
	out   func(VoiceOut)
	in    chan voiceInput
	done  chan struct{}
	once  sync.Once
	ctx   context.Context
	stop  context.CancelFunc
	// outMu keeps a message from following ended.
	outMu sync.Mutex
	// oversize: a frame too large was dropped, logged once.
	oversize sync.Once

	// The loop's state.
	state      string
	followUp   bool
	det        *endofturn.Detector
	buf        []byte
	turn       int
	gen        int
	heard      int64 // samples since listening began
	started    bool  // speech began in this turn
	busy       bool  // a transcription is running
	spec       *voiceTranscript
	wantSpec   bool
	wantFinal  bool
	lastPart   int64
	grace      bool // listening for speech that resumes after a send
	sent       time.Time
	sawWorking bool
	activity   string
	lines      []VoiceLine
	playing    string
	spoke      bool
	stopped    bool // the person stopped this turn's lines
	times      voiceTimings
	// The turn's settings, read when it starts.
	pause, followUpWait time.Duration
	partials            bool
	// The turn being delivered, and one the person stopped meanwhile: the
	// Reeve is interrupted once it lands.
	delivering, interruptOnDelivery int
	// The turn that reached the Reeve.
	delivered int
}

// Open opens a voice session for a device. One session at a time: an
// open one is ended, "taken over by <device>". out receives the session's
// messages from its loop, in order, and must not block for long.
func (v *ReeveVoice) Open(hello VoiceHello, out func(VoiceOut)) *VoiceSession {
	if strings.TrimSpace(hello.Device) == "" {
		hello.Device = "a device"
	}
	ctx, stop := context.WithCancel(context.Background())
	s := &VoiceSession{v: v, hello: hello, out: out, in: make(chan voiceInput, 256), done: make(chan struct{}), ctx: ctx, stop: stop, state: VoiceIdle}

	v.mu.Lock()
	old := v.session
	v.session, v.open, v.device = s, true, hello.Device
	v.mu.Unlock()
	if old != nil {
		old.end("taken over by " + hello.Device)
	}
	go s.loop()
	s.send(VoiceOut{Type: "state", State: VoiceIdle})
	return s
}

// Done is closed when the session has ended.
func (s *VoiceSession) Done() <-chan struct{} { return s.done }

// Audio gives the session a frame of 16 kHz mono s16le PCM.
func (s *VoiceSession) Audio(pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	if len(pcm) > voiceMaxFrame {
		s.oversize.Do(func() {
			s.v.logf("reeve voice: %s sent an audio frame of %d bytes, over a second: frames that large are dropped", s.hello.Device, len(pcm))
		})
		return
	}
	s.put(voiceInput{audio: append([]byte(nil), pcm...)})
}

// Handle gives the session a message from its device.
func (s *VoiceSession) Handle(msg VoiceIn) { s.put(voiceInput{msg: &msg}) }

// Close ends the session: its device went away.
func (s *VoiceSession) Close() { s.end("") }

// end stops the session, telling the device why when there is a reason.
func (s *VoiceSession) end(reason string) {
	s.once.Do(func() {
		s.stop()
		s.outMu.Lock()
		if reason != "" {
			s.out(VoiceOut{Type: "ended", Reason: reason})
		}
		s.v.mu.Lock()
		if s.v.session == s {
			s.v.session, s.v.open, s.v.device = nil, false, ""
		}
		s.v.mu.Unlock()
		close(s.done)
		s.outMu.Unlock()
		s.v.releaseSession(s)
	})
}

func (s *VoiceSession) put(in voiceInput) {
	select {
	case <-s.done:
	case s.in <- in:
	default:
		// A device that sends faster than the session keeps up loses
		// audio, not the conversation.
		if in.audio == nil {
			select {
			case <-s.done:
			case s.in <- in:
			}
		}
	}
}

func (s *VoiceSession) send(out VoiceOut) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	select {
	case <-s.done:
	default:
		s.out(out)
	}
}

func (s *VoiceSession) setState(state, reason string) {
	s.state = state
	s.send(VoiceOut{Type: "state", State: state, Reason: reason})
}

func (s *VoiceSession) settings() config.ReeveVoiceConfig {
	if s.v.opts.Settings == nil {
		return config.ReeveVoiceConfig{}
	}
	if cfg := s.v.opts.Settings(s.ctx); cfg != nil {
		return cfg.Reeve.Voice
	}
	return config.ReeveVoiceConfig{}
}

func (s *VoiceSession) loop() {
	poll := s.v.opts.Poll
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case in := <-s.in:
			switch {
			case in.audio != nil:
				s.onAudio(in.audio)
			case in.msg != nil:
				s.onMessage(*in.msg)
			case in.transcript != nil:
				s.onTranscript(*in.transcript)
			case in.delivered != nil:
				s.onDelivered(*in.delivered)
			case in.line != nil:
				s.onLine(*in.line)
			}
		case <-ticker.C:
			s.onTick()
		}
	}
}

func (s *VoiceSession) onMessage(msg VoiceIn) {
	switch msg.Type {
	case "listen":
		if s.state == VoiceSpeaking {
			s.dropLines()
		}
		s.listen(false)
	case "send":
		if s.state == VoiceListening {
			s.endTurn()
		}
	case "cancel":
		if s.state == VoiceListening {
			s.turn++
			s.setState(VoiceIdle, VoiceReasonCancelled)
		}
	case "stop":
		s.onStop()
	case "playing":
		s.onPlaying(msg.Line)
	case "played":
		s.onPlayed(msg.Line)
	case "bye":
		s.end(VoiceReasonBye)
	}
}

// listen starts a turn: from the key, Reply, or the follow-up window.
func (s *VoiceSession) listen(followUp bool) {
	cfg := s.settings()
	s.pause, s.followUpWait, s.partials = cfg.PauseDuration(), cfg.FollowUpDuration(), cfg.PartialsOn()
	pause := s.pause
	if s.det == nil || s.det.Config().Pause != pause {
		// The session times the wait for speech itself, so the detector's
		// own is out of the way.
		s.det = endofturn.New(endofturn.Config{SampleRate: voiceSampleRate, Pause: pause, NoSpeech: time.Hour})
	} else {
		s.det.Reset()
	}
	s.det.SetSpeaking(false)
	s.turn++
	s.gen, s.heard, s.started, s.buf = 0, 0, false, s.buf[:0]
	s.spec, s.wantSpec, s.wantFinal, s.lastPart, s.grace = nil, false, false, 0, false
	s.followUp, s.stopped = followUp, false
	s.times = voiceTimings{}
	s.setState(VoiceListening, "")
}

func (s *VoiceSession) onAudio(pcm []byte) {
	switch {
	case s.state == VoiceListening:
	case s.state == VoiceThinking && s.grace:
	default:
		// Idle, speaking (no echo cancellation yet), or thinking past
		// the window: the microphone is not listened to.
		return
	}
	s.buf = append(s.buf, pcm...)
	s.heard += int64(len(pcm) / 2)
	for _, ev := range s.det.Write(pcm) {
		s.onEvent(ev)
		if s.state != VoiceListening && !(s.state == VoiceThinking && s.grace) {
			return
		}
	}
	elapsed := samplesToDuration(s.heard)
	if s.grace {
		if elapsed > voiceResumeWindow+endofturn.DefaultMinSpeech {
			s.grace, s.buf = false, s.buf[:0]
		}
		return
	}
	if !s.started {
		wait := voiceNoSpeech
		if s.followUp {
			wait = s.followUpWait
		}
		if elapsed >= wait {
			s.turn++
			s.setState(VoiceIdle, VoiceReasonNoSpeech)
		}
		return
	}
	s.maybePartial()
}

func (s *VoiceSession) onEvent(ev endofturn.Event) {
	switch ev.Kind {
	case endofturn.SpeechStarted:
		if s.grace {
			if ev.At > voiceResumeWindow {
				return
			}
			// Speech resumed right after a send: a second message.
			s.grace = false
			s.turn++
			s.gen, s.spec, s.wantSpec, s.wantFinal, s.lastPart = 0, nil, false, false, 0
			s.started = true
			s.setState(VoiceListening, "")
			return
		}
		s.started = true
	case endofturn.Pause:
		if s.state == VoiceListening {
			s.send(VoiceOut{Type: "pause", Ms: ev.Silence.Milliseconds(), Of: ev.Of.Milliseconds()})
		}
	case endofturn.Boundary:
		if s.state == VoiceListening {
			s.speculate()
		}
	case endofturn.Resumed:
		// The speculative text no longer covers the turn.
		s.gen++
		s.spec, s.wantSpec = nil, false
	case endofturn.TurnEnded:
		if s.state == VoiceListening {
			s.times.speechEnd = time.Now().Add(-ev.Silence)
			s.endTurn()
		}
	}
}

// speculate starts transcribing the turn so far at a sentence boundary,
// so the text is usually ready when the pause completes.
func (s *VoiceSession) speculate() {
	if s.busy {
		s.wantSpec = true
		return
	}
	s.transcribe(transcribeSpeculative)
}

// maybePartial transcribes the words so far about once a second, when
// Whisper is idle, in a turn's first 20 seconds.
func (s *VoiceSession) maybePartial() {
	if s.busy || !s.partials || s.v.opts.Transcription == nil {
		return
	}
	if samplesToDuration(s.heard) > voicePartialsFor || samplesToDuration(s.heard-s.lastPart) < voicePartialEvery {
		return
	}
	s.lastPart = s.heard
	s.transcribe(transcribePartial)
}

func (s *VoiceSession) transcribe(kind int) {
	t := s.v.opts.Transcription
	job := voiceTranscript{kind: kind, turn: s.turn, gen: s.gen, bytes: len(s.buf)}
	pcm := append([]byte(nil), s.buf...)
	if t == nil {
		// Handled here: putting it on the loop's own queue could wait on
		// the loop.
		s.onTranscript(voiceTranscript{kind: kind, turn: job.turn, gen: job.gen, err: errNoTranscription})
		return
	}
	s.busy = true
	go func() {
		job.text, job.err = t.TranscribePCM(s.ctx, pcm, voiceSampleRate)
		s.put(voiceInput{transcript: &job})
	}()
}

// endTurn sends the turn: the speculative text when it still covers it,
// else the turn transcribed now.
func (s *VoiceSession) endTurn() {
	if s.times.speechEnd.IsZero() {
		s.times.speechEnd = time.Now()
	}
	s.setState(VoiceThinking, "")
	if s.spec != nil && s.spec.gen == s.gen && s.spec.err == nil {
		s.deliver(s.spec.text)
		return
	}
	s.wantFinal = true
	if !s.busy {
		s.wantFinal = false
		s.transcribe(transcribeFinal)
	}
}

func (s *VoiceSession) onTranscript(t voiceTranscript) {
	s.busy = false
	if t.turn == s.turn {
		switch t.kind {
		case transcribePartial:
			if s.state == VoiceListening && t.err == nil && t.text != "" {
				s.send(VoiceOut{Type: "partial", Text: t.text})
			}
		case transcribeSpeculative:
			if t.gen == s.gen {
				s.spec = &t
				if s.state == VoiceThinking && s.wantFinal {
					s.wantFinal = false
					if t.err == nil {
						s.deliver(t.text)
						return
					}
					s.transcribe(transcribeFinal)
					return
				}
			}
		case transcribeFinal:
			if s.state == VoiceThinking {
				if t.err != nil {
					s.v.logf("reeve voice: transcribing a turn: %v", t.err)
					s.setState(VoiceIdle, VoiceReasonFailed)
					return
				}
				s.deliver(t.text)
				return
			}
		}
	}
	// Work that waited for Whisper.
	switch {
	case s.state == VoiceThinking && s.wantFinal:
		s.wantFinal = false
		if s.spec != nil && s.spec.gen == s.gen && s.spec.err == nil {
			s.deliver(s.spec.text)
			return
		}
		s.transcribe(transcribeFinal)
	case s.state == VoiceListening && s.wantSpec:
		s.wantSpec = false
		s.transcribe(transcribeSpeculative)
	}
}

// deliver gives the turn's text to the Reeve as a spoken message.
func (s *VoiceSession) deliver(text string) {
	text = strings.TrimSpace(text)
	s.times.transcript = time.Now()
	if text == "" {
		s.setState(VoiceIdle, VoiceReasonNothingHeard)
		return
	}
	s.send(VoiceOut{Type: "heard", Text: text})
	s.sent, s.sawWorking, s.activity, s.spoke = time.Now(), false, "", false
	s.v.setAnswering(true)
	turn := s.turn
	s.delivering = turn
	reeve := s.v.opts.Reeve
	go func() {
		var err error
		if reeve == nil {
			err = errNoReeve
		} else {
			err = reeve.Message(s.ctx, VoiceMessage(text), "voice:"+s.hello.Device)
		}
		s.put(voiceInput{delivered: &voiceDelivery{turn: turn, err: err}})
	}()
	// Speech that resumes at once is a second message: keep listening
	// for a moment.
	s.det.Reset()
	s.heard, s.buf, s.grace, s.started = 0, s.buf[:0], true, false
}

func (s *VoiceSession) onDelivered(d voiceDelivery) {
	if s.delivering == d.turn {
		s.delivering = 0
	}
	if d.turn == s.interruptOnDelivery {
		// The person stopped this turn while it was on its way.
		s.interruptOnDelivery = 0
		if d.err == nil {
			s.interruptReeve()
		}
		return
	}
	if d.err != nil {
		s.v.logf("reeve voice: delivering a turn to the Reeve: %v", d.err)
		if d.turn == s.turn && s.state == VoiceThinking {
			s.setState(VoiceIdle, VoiceReasonFailed)
		}
		return
	}
	if d.turn == s.turn {
		s.delivered = d.turn
		s.times.delivered = time.Now()
	}
}

// interruptReeve stops the Reeve's turn.
func (s *VoiceSession) interruptReeve() {
	if s.v.opts.Reeve == nil || s.v.opts.Agents == nil {
		return
	}
	if current, err := s.v.opts.Reeve.Current(s.ctx); err == nil && current != nil {
		if err := s.v.opts.Agents.InterruptAgent(s.ctx, current.TaskID); err != nil {
			s.v.logf("reeve voice: interrupting the Reeve: %v", err)
		}
	}
}

// onTick follows the Reeve while it thinks: what it is doing, and when
// its turn is over.
func (s *VoiceSession) onTick() {
	if s.state != VoiceThinking && s.state != VoiceSpeaking || s.sent.IsZero() {
		return
	}
	working, activity := s.reeveActivity()
	if activity != "" && activity != s.activity && s.state == VoiceThinking {
		s.activity = activity
		s.send(VoiceOut{Type: "activity", Text: activity})
	}
	switch {
	case working:
		s.sawWorking = true
	case s.sawWorking:
		s.turnOver()
	case time.Since(s.sent) > voiceAnswerWait:
		s.setState(VoiceIdle, VoiceReasonNoAnswer)
	}
}

// reeveActivity is whether the Reeve's agent is in a turn, and one line of
// what it is doing.
func (s *VoiceSession) reeveActivity() (bool, string) {
	if s.v.opts.Reeve == nil {
		return false, ""
	}
	current, err := s.v.opts.Reeve.Current(s.ctx)
	if err != nil || current == nil {
		return false, ""
	}
	working := false
	if s.v.opts.Store != nil {
		if agent, err := s.v.opts.Store.GetAgentSessionByTaskID(s.ctx, current.TaskID); err == nil {
			working = agent.Activity == "working"
		}
	}
	line := ""
	if s.v.opts.Agents != nil {
		if tail, err := s.v.opts.Agents.ConversationTail(s.ctx, current.TaskID, 8); err == nil && tail != nil {
			for i := len(tail.Entries) - 1; i >= 0; i-- {
				if e := tail.Entries[i]; e.Kind == EntryTool && !isSpeakTool(e.Tool) {
					line = activityLine(e.Text)
					break
				}
			}
		}
	}
	return working, line
}

// turnOver: the Reeve's turn ended. With lines still to play, the session
// is speaking until they have; with none said, it answered in writing.
func (s *VoiceSession) turnOver() {
	s.sent = time.Time{}
	s.grace = false
	s.v.setAnswering(false)
	if s.stopped {
		s.logTimings()
		s.setState(VoiceIdle, "")
		return
	}
	if s.spoke {
		if len(s.lines) == 0 && s.playing == "" {
			s.afterSpeaking()
		}
		return
	}
	s.logTimings()
	s.send(VoiceOut{Type: "written"})
	s.setState(VoiceIdle, "")
}

// afterSpeaking listens for a reply for reeve.voice.follow_up, or goes
// idle when that is 0.
func (s *VoiceSession) afterSpeaking() {
	s.logTimings()
	if s.settings().FollowUpDuration() <= 0 {
		s.setState(VoiceIdle, "")
		return
	}
	s.listen(true)
}

func (s *VoiceSession) onStop() {
	switch s.state {
	case VoiceSpeaking:
		s.dropLines()
		s.stopped = true
		if s.sent.IsZero() {
			s.setState(VoiceIdle, "")
		} else {
			s.setState(VoiceThinking, "")
		}
	case VoiceThinking:
		// Only a turn that reached the Reeve is interrupted: one still on
		// its way is, once it lands; one not yet sent is dropped, and the
		// Reeve's own work goes on.
		switch {
		case s.delivered == s.turn:
			s.interruptReeve()
		case s.delivering == s.turn:
			s.interruptOnDelivery = s.turn
		}
		s.turn++
		s.sent, s.grace = time.Time{}, false
		s.v.mu.Lock()
		s.v.dropTurn = true
		s.v.mu.Unlock()
		s.setState(VoiceIdle, VoiceReasonCancelled)
	case VoiceListening:
		s.turn++
		s.setState(VoiceIdle, VoiceReasonCancelled)
	}
}

func (s *VoiceSession) logTimings() {
	t := s.times
	if t.speechEnd.IsZero() {
		return
	}
	since := func(at time.Time) string {
		if at.IsZero() {
			return "-"
		}
		return at.Sub(t.speechEnd).Round(10 * time.Millisecond).String()
	}
	s.v.logf("reeve voice: turn from speech end: transcript %s, delivered %s, first speak %s, line made %s, playing %s",
		since(t.transcript), since(t.delivered), since(t.firstLine), since(t.lineMade), since(t.playing))
	s.times = voiceTimings{}
}

func samplesToDuration(n int64) time.Duration {
	return time.Duration(n) * time.Second / voiceSampleRate
}

func activityLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if len(line) > 120 {
		line = line[:117] + "…"
	}
	return line
}

// isSpeakTool reports whether a tool call is the Reeve's speak, by its
// native or MCP name.
func isSpeakTool(name string) bool {
	return name == "speak" || strings.HasSuffix(name, "legato__speak") || strings.HasSuffix(name, "legato.speak") || strings.HasSuffix(name, "legato_speak")
}

func (v *ReeveVoice) logf(format string, args ...any) {
	if v.opts.Logf != nil {
		v.opts.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

var _ ReeveVoiceAgents = (*agentService)(nil)
