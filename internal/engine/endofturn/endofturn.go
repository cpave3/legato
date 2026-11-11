// Package endofturn decides when a person has finished speaking, from a
// stream of 16-bit mono PCM. It is an energy detector in pure Go, ported
// from PSITTAC's vad.ts, with a tracked noise floor, a sentence boundary
// and a configurable pause added. See docs/analysis/reeve-voice-conversation.md
// § 4 "End of turn".
//
// # Use
//
//	d := endofturn.New(endofturn.Config{Pause: time.Second})
//	for chunk := range audio {
//		for _, ev := range d.Write(chunk) { ... }
//	}
//	d.Reset() // listen for the next turn
//
// Write takes s16le mono PCM of any length, an odd trailing byte included,
// and returns what happened, in order. The audio is cut into 20 ms frames,
// and everything is timed by counting samples, never by when the bytes
// arrived: the same audio gives the same events however it is chunked.
// Event.At is the position in the audio written since New or the last Reset.
//
// # Events
//
//   - SpeechStarted: the turn has at least MinSpeech of speech. At is where
//     that speech began, so it is reported up to MinSpeech late. Speech
//     shorter than MinSpeech followed by Pause of quiet (a click, a cough)
//     is forgotten, and no event is sent for it.
//   - Pause: during quiet after speech, every 100 ms of silence, with Silence
//     (the quiet so far) and Of (the configured Pause), for a pause bar. When
//     speech comes back after a Pause event, a Pause with Silence 0 is sent so
//     the bar empties.
//   - Boundary: once per quiet gap, when it reaches Boundary (400 ms). The
//     caller can start transcribing the turn so far.
//   - Resumed: speech came back after a Boundary; drop that transcription.
//   - TurnEnded: Reason ReasonPause when the quiet reaches Pause (Silence
//     is the quiet, so the speech ended at At-Silence), or ReasonMaxLength
//     when the turn has lasted MaxTurn since its speech began.
//   - NoSpeech: NoSpeech passed since listening began with no turn.
//
// After TurnEnded or NoSpeech the detector sends nothing until Reset. It
// still follows the level and the noise floor.
//
// # Speech, the noise floor and the thresholds
//
// Each frame's RMS is compared with two thresholds (hysteresis, as in
// PSITTAC): a frame at or over the start threshold starts speech after
// 45 ms of such frames in a row (three frames); once speech has started, a
// frame at or over the keep threshold counts as speech, so quiet syllables
// and breaths stay in the turn. Anything under is quiet.
//
// The noise floor is an average of the RMS of every frame under the start
// threshold, whatever the state. It rises slowly (time constant 3 s), so
// soft syllables and a passing noise barely move it, and falls fast
// (100 ms), so it follows the quietest sound in the room. It starts at 0,
// so until it has learned the room the absolute figures below decide, and
// speech at the very start is heard. Frames inside a turn count too: a fan
// over the absolute keep threshold, met before the floor has learned it,
// would otherwise keep a turn going until MaxTurn. The floor is kept across
// Reset.
//
// The thresholds are relative to it, with PSITTAC's absolute figures as
// their least, so a silent room behaves as PSITTAC does:
//
//	start = max(650, 4 × floor)   // 12 dB over the floor
//	keep  = max(300, 2 × floor)   //  6 dB over the floor
//
// While SetSpeaking(true), both are 2.5× higher, so the Reeve's own voice
// from the speakers rarely counts as speech, and the floor is not updated,
// so it does not learn that voice.
//
// A loud noise that starts abruptly (over the start threshold) and does not
// stop is taken for speech; such a turn ends at MaxTurn.
//
// A Detector is safe for use from several goroutines.
package endofturn

import (
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// Defaults and limits of Config.
const (
	DefaultSampleRate = 16000
	DefaultPause      = time.Second
	MinPause          = 300 * time.Millisecond
	MaxPause          = 2 * time.Second
	DefaultBoundary   = 400 * time.Millisecond
	DefaultMinSpeech  = 300 * time.Millisecond
	DefaultNoSpeech   = 8 * time.Second
	DefaultMaxTurn    = 60 * time.Second
)

// Fixed parts of the detector.
const (
	frameDuration   = 20 * time.Millisecond
	onsetDuration   = 45 * time.Millisecond
	pauseReportStep = 100 * time.Millisecond

	startFloorRMS   = 650.0 // least start threshold
	keepFloorRMS    = 300.0 // least keep threshold
	startOverFloor  = 4.0   // 12 dB
	keepOverFloor   = 2.0   // 6 dB
	speakingFactor  = 2.5
	floorRise       = 3 * time.Second
	floorFall       = 100 * time.Millisecond
	levelFloorDBFS  = -60.0
	fullScaleSample = 32768.0
)

// Config sets the detector's timings. A zero field takes its default.
type Config struct {
	// SampleRate of the PCM, 16000 when 0.
	SampleRate int
	// Pause of quiet that ends the turn: 1 s when 0, clamped to 0.3–2 s.
	Pause time.Duration
	// Boundary is the quiet that marks a sentence boundary: 400 ms when 0.
	// When it is not shorter than Pause, it is Pause/2.
	Boundary time.Duration
	// MinSpeech is the least speech in a turn: 300 ms when 0.
	MinSpeech time.Duration
	// NoSpeech is how long to listen for a turn: 8 s when 0.
	NoSpeech time.Duration
	// MaxTurn is the longest turn, from its first speech: 60 s when 0.
	MaxTurn time.Duration
}

// Normalized returns c with its defaults and limits applied: the values a
// Detector made from c uses.
func (c Config) Normalized() Config {
	if c.SampleRate <= 0 {
		c.SampleRate = DefaultSampleRate
	}
	if c.Pause <= 0 {
		c.Pause = DefaultPause
	}
	c.Pause = min(max(c.Pause, MinPause), MaxPause)
	if c.Boundary <= 0 {
		c.Boundary = DefaultBoundary
	}
	if c.Boundary >= c.Pause {
		c.Boundary = c.Pause / 2
	}
	if c.MinSpeech <= 0 {
		c.MinSpeech = DefaultMinSpeech
	}
	if c.NoSpeech <= 0 {
		c.NoSpeech = DefaultNoSpeech
	}
	if c.MaxTurn <= 0 {
		c.MaxTurn = DefaultMaxTurn
	}
	return c
}

// EventKind says what an Event reports.
type EventKind int

const (
	SpeechStarted EventKind = iota + 1
	Pause
	Boundary
	Resumed
	TurnEnded
	NoSpeech
)

func (k EventKind) String() string {
	switch k {
	case SpeechStarted:
		return "speech_started"
	case Pause:
		return "pause"
	case Boundary:
		return "boundary"
	case Resumed:
		return "resumed"
	case TurnEnded:
		return "turn_ended"
	case NoSpeech:
		return "no_speech"
	}
	return "unknown"
}

// Reasons a turn ends.
const (
	ReasonPause     = "pause"
	ReasonMaxLength = "max_length"
)

// Event is one thing the detector noticed.
type Event struct {
	Kind EventKind
	// Reason is ReasonPause or ReasonMaxLength, for TurnEnded.
	Reason string
	// Silence is the quiet so far, for Pause, Boundary and TurnEnded{pause}.
	Silence time.Duration
	// Of is the configured Pause, for Pause.
	Of time.Duration
	// At is the position in the audio written since New or Reset.
	At time.Duration
}

type state int

const (
	listening state = iota // no speech yet
	candidate              // speech, less than MinSpeech of it
	inTurn                 // a turn: SpeechStarted sent
	done                   // TurnEnded or NoSpeech sent
)

// Detector finds the end of a spoken turn. Make one with New.
type Detector struct {
	mu  sync.Mutex
	cfg Config

	// In samples.
	frame, onset, pause, boundary, minSpeech, noSpeech, maxTurn, step int64
	riseAlpha, fallAlpha                                              float64

	carry    []byte
	samples  []int16
	pos      int64
	state    state
	speaking bool
	level    float64

	floor float64

	onsetRun    int64
	onsetStart  int64
	speechStart int64
	voiced      int64
	silence     int64
	boundaryOn  bool
	paused      bool
	nextReport  int64
}

// New returns a Detector listening for a turn.
func New(cfg Config) *Detector {
	cfg = cfg.Normalized()
	rate := float64(cfg.SampleRate)
	samples := func(d time.Duration) int64 { return int64(math.Round(d.Seconds() * rate)) }
	frameSec := frameDuration.Seconds()
	d := &Detector{
		cfg:       cfg,
		frame:     samples(frameDuration),
		onset:     samples(onsetDuration),
		pause:     samples(cfg.Pause),
		boundary:  samples(cfg.Boundary),
		minSpeech: samples(cfg.MinSpeech),
		noSpeech:  samples(cfg.NoSpeech),
		maxTurn:   samples(cfg.MaxTurn),
		step:      samples(pauseReportStep),
		riseAlpha: 1 - math.Exp(-frameSec/floorRise.Seconds()),
		fallAlpha: 1 - math.Exp(-frameSec/floorFall.Seconds()),
	}
	d.samples = make([]int16, 0, d.frame)
	return d
}

// Config returns the configuration in use, defaults applied.
func (d *Detector) Config() Config { return d.cfg }

// Write feeds s16le mono PCM and returns the events it caused, in order.
func (d *Detector) Write(pcm []byte) []Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	var evs []Event
	if len(d.carry) > 0 && len(pcm) > 0 {
		d.addSample(int16(binary.LittleEndian.Uint16([]byte{d.carry[0], pcm[0]})), &evs)
		d.carry = d.carry[:0]
		pcm = pcm[1:]
	}
	for len(pcm) >= 2 {
		d.addSample(int16(binary.LittleEndian.Uint16(pcm)), &evs)
		pcm = pcm[2:]
	}
	if len(pcm) == 1 {
		d.carry = append(d.carry, pcm[0])
	}
	return evs
}

func (d *Detector) addSample(s int16, evs *[]Event) {
	d.samples = append(d.samples, s)
	if int64(len(d.samples)) == d.frame {
		d.processFrame(evs)
		d.samples = d.samples[:0]
	}
}

// Level is the latest frame's level from 0 to 1: its RMS in dBFS, -60 dB
// and below as 0, full scale as 1. For a meter.
func (d *Detector) Level() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.level
}

// NoiseFloor is the tracked noise floor, as an RMS of 16-bit samples.
func (d *Detector) NoiseFloor() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.floor
}

// Thresholds are the start and keep thresholds for the next frame, as RMS.
func (d *Detector) Thresholds() (start, keep float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.thresholds()
}

// SetSpeaking says whether the Reeve's voice is playing: while it is, the
// thresholds are 2.5× higher and the noise floor is held.
func (d *Detector) SetSpeaking(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.speaking = on
}

// Reset starts listening for a new turn. Positions count from here again,
// and a part frame not yet processed is dropped. The noise floor, the level
// and SetSpeaking are kept.
func (d *Detector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.carry = d.carry[:0]
	d.samples = d.samples[:0]
	d.pos = 0
	d.state = listening
	d.onsetRun = 0
	d.clearSpeech()
}

func (d *Detector) clearSpeech() {
	d.speechStart, d.voiced = 0, 0
	d.clearGap()
}

func (d *Detector) clearGap() {
	d.silence = 0
	d.boundaryOn, d.paused = false, false
	d.nextReport = d.step
}

func (d *Detector) thresholds() (start, keep float64) {
	start = max(startFloorRMS, startOverFloor*d.floor)
	keep = max(keepFloorRMS, keepOverFloor*d.floor)
	if d.speaking {
		start *= speakingFactor
		keep *= speakingFactor
	}
	return start, keep
}

func (d *Detector) dur(samples int64) time.Duration {
	return time.Duration(samples) * time.Second / time.Duration(d.cfg.SampleRate)
}

func (d *Detector) learnFloor(rms float64) {
	if d.speaking {
		return
	}
	a := d.riseAlpha
	if rms < d.floor {
		a = d.fallAlpha
	}
	d.floor += (rms - d.floor) * a
}

func (d *Detector) processFrame(evs *[]Event) {
	var sum float64
	for _, s := range d.samples {
		sum += float64(s) * float64(s)
	}
	rms := math.Sqrt(sum / float64(len(d.samples)))
	if rms > 0 {
		d.level = min(max((20*math.Log10(rms/fullScaleSample)-levelFloorDBFS)/-levelFloorDBFS, 0), 1)
	} else {
		d.level = 0
	}
	start, keep := d.thresholds()
	if rms < start {
		d.learnFloor(rms)
	}
	frameStart := d.pos
	d.pos += d.frame
	emit := func(e Event) { *evs = append(*evs, e) }

	switch d.state {
	case done:

	case listening:
		if rms >= start {
			if d.onsetRun == 0 {
				d.onsetStart = frameStart
			}
			d.onsetRun += d.frame
			if d.onsetRun >= d.onset {
				d.state = candidate
				d.clearSpeech()
				d.speechStart = d.onsetStart
				d.voiced = d.onsetRun
				d.onsetRun = 0
				d.confirm(emit)
			}
		} else {
			d.onsetRun = 0
		}

	case candidate, inTurn:
		if rms >= keep {
			if d.silence > 0 && d.state == inTurn {
				if d.boundaryOn {
					emit(Event{Kind: Resumed, At: d.dur(d.pos)})
				}
				if d.paused {
					emit(Event{Kind: Pause, Of: d.cfg.Pause, At: d.dur(d.pos)})
				}
			}
			d.clearGap()
			d.voiced += d.frame
			d.confirm(emit)
		} else {
			d.silence += d.frame
			if d.state == candidate {
				if d.silence >= d.pause {
					// Too little speech for a turn: forget it.
					d.state = listening
					d.clearSpeech()
				}
			} else if d.silence >= d.pause {
				emit(Event{Kind: TurnEnded, Reason: ReasonPause, Silence: d.dur(d.silence), At: d.dur(d.pos)})
				d.state = done
			} else {
				if d.silence >= d.nextReport {
					emit(Event{Kind: Pause, Silence: d.dur(d.silence), Of: d.cfg.Pause, At: d.dur(d.pos)})
					d.paused = true
					d.nextReport = (d.silence/d.step + 1) * d.step
				}
				if !d.boundaryOn && d.silence >= d.boundary {
					emit(Event{Kind: Boundary, Silence: d.dur(d.silence), At: d.dur(d.pos)})
					d.boundaryOn = true
				}
			}
		}
		if d.state == inTurn && d.pos-d.speechStart >= d.maxTurn {
			emit(Event{Kind: TurnEnded, Reason: ReasonMaxLength, At: d.dur(d.pos)})
			d.state = done
		}
	}

	if d.state == listening && d.onsetRun == 0 && d.pos >= d.noSpeech {
		emit(Event{Kind: NoSpeech, At: d.dur(d.pos)})
		d.state = done
	}
}

// confirm makes a candidate a turn once it has MinSpeech of speech.
func (d *Detector) confirm(emit func(Event)) {
	if d.state == candidate && d.voiced >= d.minSpeech {
		d.state = inTurn
		emit(Event{Kind: SpeechStarted, At: d.dur(d.speechStart)})
	}
}
