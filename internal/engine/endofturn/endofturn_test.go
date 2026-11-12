package endofturn

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
	"time"
)

const rate = 16000

// tone is a 220 Hz sine with the given RMS level, standing in for speech.
func tone(rms float64, d time.Duration) []int16 {
	n := int(d.Seconds() * rate)
	peak := rms * math.Sqrt2
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(peak * math.Sin(2*math.Pi*220*float64(i)/rate))
	}
	return out
}

// noise is uniform white noise with the given RMS level, standing in for a fan.
func noise(rms float64, d time.Duration, seed int64) []int16 {
	n := int(d.Seconds() * rate)
	r := rand.New(rand.NewSource(seed))
	a := rms * math.Sqrt(3)
	out := make([]int16, n)
	for i := range out {
		out[i] = int16((r.Float64()*2 - 1) * a)
	}
	return out
}

func silence(d time.Duration) []int16 { return make([]int16, int(d.Seconds()*rate)) }

// mix adds b over a, sample by sample, from the start of a.
func mix(a, b []int16) []int16 {
	out := append([]int16(nil), a...)
	for i := range out {
		if i < len(b) {
			out[i] = int16(max(-32768, min(32767, int(out[i])+int(b[i]))))
		}
	}
	return out
}

func pcm(parts ...[]int16) []byte {
	var out []byte
	for _, p := range parts {
		for _, s := range p {
			out = binary.LittleEndian.AppendUint16(out, uint16(s))
		}
	}
	return out
}

func feed(d *Detector, audio []byte, chunk int) []Event {
	var evs []Event
	for len(audio) > 0 {
		n := min(chunk, len(audio))
		evs = append(evs, d.Write(audio[:n])...)
		audio = audio[n:]
	}
	return evs
}

func kinds(evs []Event, k EventKind) []Event {
	var out []Event
	for _, e := range evs {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

const frame = 20 * time.Millisecond

func near(got, want time.Duration) bool {
	d := got - want
	return d >= -frame && d <= frame
}

func TestSentenceThenPauseEndsTheTurnAfterThePause(t *testing.T) {
	d := New(Config{})
	evs := feed(d, pcm(silence(500*time.Millisecond), tone(3000, 2*time.Second), silence(3*time.Second)), 3200)

	started := kinds(evs, SpeechStarted)
	if len(started) != 1 || !near(started[0].At, 500*time.Millisecond) {
		t.Fatalf("SpeechStarted = %+v, want one at 500ms", started)
	}
	ended := kinds(evs, TurnEnded)
	if len(ended) != 1 {
		t.Fatalf("TurnEnded = %+v, want one", ended)
	}
	if ended[0].Reason != ReasonPause || !near(ended[0].At, 3500*time.Millisecond) {
		t.Fatalf("TurnEnded = %+v, want reason pause at 3.5s", ended[0])
	}
	if evs[len(evs)-1].Kind != TurnEnded {
		t.Fatalf("events after the turn ended: %+v", evs)
	}
	if b := kinds(evs, Boundary); len(b) != 1 || !near(b[0].At, 2900*time.Millisecond) {
		t.Fatalf("Boundary = %+v, want one at 2.9s", b)
	}
	if len(kinds(evs, NoSpeech)) != 0 || len(kinds(evs, Resumed)) != 0 {
		t.Fatalf("unexpected events: %+v", evs)
	}
}

func TestAPauseShorterThanPauseIsABoundaryAndTheTurnGoesOn(t *testing.T) {
	d := New(Config{})
	evs := feed(d, pcm(silence(300*time.Millisecond), tone(3000, time.Second), silence(600*time.Millisecond),
		tone(3000, time.Second), silence(2*time.Second)), 4000)

	if n := len(kinds(evs, SpeechStarted)); n != 1 {
		t.Fatalf("SpeechStarted %d times, want once: %+v", n, evs)
	}
	b := kinds(evs, Boundary)
	if len(b) != 2 || !near(b[0].At, 1700*time.Millisecond) || !near(b[1].At, 3300*time.Millisecond) {
		t.Fatalf("Boundary = %+v, want at 1.7s and 3.3s", b)
	}
	r := kinds(evs, Resumed)
	if len(r) != 1 || !near(r[0].At, 1900*time.Millisecond) {
		t.Fatalf("Resumed = %+v, want one at 1.9s", r)
	}
	ended := kinds(evs, TurnEnded)
	if len(ended) != 1 || ended[0].Reason != ReasonPause || !near(ended[0].At, 3900*time.Millisecond) {
		t.Fatalf("TurnEnded = %+v, want one at 3.9s", ended)
	}
	if ended[0].Silence != time.Second {
		t.Fatalf("TurnEnded.Silence = %v, want 1s", ended[0].Silence)
	}
}

func TestPauseEventsComeEvery100msAndEmptyWhenSpeechResumes(t *testing.T) {
	d := New(Config{Pause: 500 * time.Millisecond})
	evs := feed(d, pcm(tone(3000, 500*time.Millisecond), silence(300*time.Millisecond),
		tone(3000, 500*time.Millisecond), silence(time.Second)), 640)

	var got []time.Duration
	for _, e := range kinds(evs, Pause) {
		if e.Of != 500*time.Millisecond {
			t.Fatalf("Pause.Of = %v, want 500ms", e.Of)
		}
		got = append(got, e.Silence)
	}
	ms := time.Millisecond
	want := []time.Duration{100 * ms, 200 * ms, 300 * ms, 0, 100 * ms, 200 * ms, 300 * ms, 400 * ms}
	if len(got) != len(want) {
		t.Fatalf("Pause silences = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Pause silences = %v, want %v", got, want)
		}
	}
	// With a 500 ms pause the boundary stays 400 ms; it is reached in the
	// second gap only.
	if b := kinds(evs, Boundary); len(b) != 1 || b[0].Silence != 400*ms {
		t.Fatalf("Boundary = %+v, want one at 400ms of silence", b)
	}
	if e := kinds(evs, TurnEnded); len(e) != 1 || !near(e[0].At, 1800*ms) {
		t.Fatalf("TurnEnded = %+v, want one at 1.8s", e)
	}
}

func TestSilenceOnlyIsNoSpeechAfter8Seconds(t *testing.T) {
	d := New(Config{})
	evs := feed(d, pcm(silence(10*time.Second)), 16000)
	if len(evs) != 1 || evs[0].Kind != NoSpeech || evs[0].At != 8*time.Second {
		t.Fatalf("events = %+v, want one NoSpeech at 8s", evs)
	}
}

func TestABoundaryNotShorterThanThePauseIsHalfThePause(t *testing.T) {
	ms := time.Millisecond
	for _, c := range []struct{ in, pause, boundary time.Duration }{
		{0, time.Second, 400 * ms},
		{100 * ms, 300 * ms, 150 * ms}, // clamped up to 0.3 s
		{5 * time.Second, 2 * time.Second, 400 * ms},
		{300 * ms, 300 * ms, 150 * ms},
	} {
		got := New(Config{Pause: c.in}).Config()
		if got.Pause != c.pause || got.Boundary != c.boundary {
			t.Errorf("Pause %v: got pause %v boundary %v, want %v %v", c.in, got.Pause, got.Boundary, c.pause, c.boundary)
		}
	}
}

func TestAFanIsNotSpeechAndTheFloorLearnsIt(t *testing.T) {
	d := New(Config{})
	evs := feed(d, pcm(noise(400, 10*time.Second, 1)), 3200)
	if len(evs) != 1 || evs[0].Kind != NoSpeech {
		t.Fatalf("events over a fan = %+v, want only NoSpeech", evs)
	}
	if f := d.NoiseFloor(); f < 340 || f > 460 {
		t.Fatalf("NoiseFloor = %.0f, want about 400", f)
	}
	start, keep := d.Thresholds()
	if start < 1360 || start > 1840 || keep < 680 || keep > 920 {
		t.Fatalf("Thresholds = %.0f, %.0f, want about 1600 and 800 (12 dB and 6 dB over the floor)", start, keep)
	}

	d.Reset()
	speech := mix(noise(400, 4*time.Second, 2), append(silence(500*time.Millisecond), tone(3000, 2*time.Second)...))
	evs = feed(d, pcm(speech), 3200)
	if s := kinds(evs, SpeechStarted); len(s) != 1 || !near(s[0].At, 500*time.Millisecond) {
		t.Fatalf("SpeechStarted over the fan = %+v, want one at 500ms", s)
	}
	if e := kinds(evs, TurnEnded); len(e) != 1 || e[0].Reason != ReasonPause || !near(e[0].At, 3500*time.Millisecond) {
		t.Fatalf("TurnEnded over the fan = %+v, want one at 3.5s", e)
	}
}

// A fan over the absolute keep threshold (300) but under the start one
// (650), with speech before the floor has learned it: the floor must
// still learn it during the turn, or the turn would never end.
func TestSpeechStraightAwayOverAFanStillEnds(t *testing.T) {
	d := New(Config{})
	speech := mix(noise(380, 8*time.Second, 5), append(silence(300*time.Millisecond), tone(3000, time.Second)...))
	evs := feed(d, pcm(speech), 3200)
	e := kinds(evs, TurnEnded)
	if len(e) != 1 || e[0].Reason != ReasonPause {
		t.Fatalf("TurnEnded = %+v, want one for the pause", e)
	}
	if e[0].At > 5*time.Second {
		t.Fatalf("TurnEnded at %v, want within a few seconds of the speech", e[0].At)
	}
}

func TestAQuietRoomUsesTheAbsoluteThresholds(t *testing.T) {
	d := New(Config{})
	feed(d, pcm(noise(20, 5*time.Second, 3)), 3200)
	if start, keep := d.Thresholds(); start != 650 || keep != 300 {
		t.Fatalf("Thresholds = %.0f, %.0f, want 650 and 300", start, keep)
	}
}

func TestAClickShorterThanMinSpeechIsIgnored(t *testing.T) {
	d := New(Config{})
	evs := feed(d, pcm(silence(time.Second), tone(3000, 200*time.Millisecond), silence(9*time.Second)), 3200)
	if len(evs) != 1 || evs[0].Kind != NoSpeech || evs[0].At != 8*time.Second {
		t.Fatalf("events = %+v, want only NoSpeech at 8s", evs)
	}
}

func TestShortBurstsWithinThePauseAddUpToATurn(t *testing.T) {
	d := New(Config{})
	ms := time.Millisecond
	evs := feed(d, pcm(silence(time.Second), tone(3000, 200*ms), silence(200*ms), tone(3000, 200*ms), silence(2*time.Second)), 3200)
	if s := kinds(evs, SpeechStarted); len(s) != 1 || !near(s[0].At, time.Second) {
		t.Fatalf("SpeechStarted = %+v, want one at 1s", s)
	}
	if e := kinds(evs, TurnEnded); len(e) != 1 || !near(e[0].At, 2600*ms) {
		t.Fatalf("TurnEnded = %+v, want one at 2.6s", e)
	}
}

func TestALongTurnIsSentAt60Seconds(t *testing.T) {
	d := New(Config{})
	evs := feed(d, pcm(silence(time.Second), tone(3000, 70*time.Second)), 32000)
	if len(evs) != 2 || evs[0].Kind != SpeechStarted || evs[1].Kind != TurnEnded {
		t.Fatalf("events = %+v, want SpeechStarted then TurnEnded", evs)
	}
	if evs[1].Reason != ReasonMaxLength || !near(evs[1].At, 61*time.Second) {
		t.Fatalf("TurnEnded = %+v, want max_length at 61s", evs[1])
	}
}

func TestWhileTheReeveSpeaksTheThresholdsAre2_5TimesHigher(t *testing.T) {
	quiet := pcm(silence(500*time.Millisecond), tone(1200, time.Second), silence(9*time.Second))

	d := New(Config{})
	if e := kinds(feed(d, quiet, 3200), TurnEnded); len(e) != 1 {
		t.Fatalf("1200 RMS speech: TurnEnded = %+v, want one", e)
	}

	d = New(Config{})
	d.SetSpeaking(true)
	if start, keep := d.Thresholds(); start != 1625 || keep != 750 {
		t.Fatalf("Thresholds while speaking = %.0f, %.0f, want 1625 and 750", start, keep)
	}
	if evs := feed(d, quiet, 3200); len(evs) != 1 || evs[0].Kind != NoSpeech {
		t.Fatalf("1200 RMS while speaking: events = %+v, want only NoSpeech", evs)
	}

	d = New(Config{})
	d.SetSpeaking(true)
	loud := pcm(silence(500*time.Millisecond), tone(5000, time.Second), silence(2*time.Second))
	if e := kinds(feed(d, loud, 3200), TurnEnded); len(e) != 1 {
		t.Fatalf("5000 RMS while speaking (barge-in): TurnEnded = %+v, want one", e)
	}
}

func TestNothingAfterTheTurnUntilReset(t *testing.T) {
	d := New(Config{})
	turn := pcm(silence(500*time.Millisecond), tone(3000, time.Second), silence(1500*time.Millisecond))
	if e := kinds(feed(d, turn, 3200), TurnEnded); len(e) != 1 {
		t.Fatalf("first turn: TurnEnded = %+v", e)
	}
	if evs := feed(d, pcm(tone(3000, time.Second), silence(10*time.Second)), 3200); len(evs) != 0 {
		t.Fatalf("events after the turn ended = %+v, want none", evs)
	}

	d.Reset()
	evs := feed(d, turn, 3200)
	if s := kinds(evs, SpeechStarted); len(s) != 1 || !near(s[0].At, 500*time.Millisecond) {
		t.Fatalf("after Reset: SpeechStarted = %+v, want one at 500ms (positions count from Reset)", s)
	}
	if e := kinds(evs, TurnEnded); len(e) != 1 || !near(e[0].At, 2500*time.Millisecond) {
		t.Fatalf("after Reset: TurnEnded = %+v, want one at 2.5s", e)
	}
}

func TestChunkingDoesNotChangeTheEvents(t *testing.T) {
	ms := time.Millisecond
	speech := append(append(append(append(silence(700*ms), tone(2500, 900*ms)...), silence(550*ms)...),
		tone(4000, 1300*ms)...), silence(250*ms)...)
	speech = append(append(speech, tone(1000, 400*ms)...), silence(2*time.Second)...)
	audio := pcm(mix(noise(250, 7*time.Second, 4), speech))

	want := feed(New(Config{Pause: 800 * ms}), audio, len(audio))
	if len(kinds(want, TurnEnded)) != 1 || len(kinds(want, Resumed)) != 1 {
		t.Fatalf("reference events = %+v, want a turn with one resume", want)
	}
	for _, chunk := range []int{1, 2, 37, 641, 32000} {
		got := feed(New(Config{Pause: 800 * ms}), audio, chunk)
		if len(got) != len(want) {
			t.Fatalf("chunk %d: %d events, want %d:\n%+v\n%+v", chunk, len(got), len(want), got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("chunk %d: event %d = %+v, want %+v", chunk, i, got[i], want[i])
			}
		}
	}
}

func TestLevelFollowsTheLatestFrame(t *testing.T) {
	d := New(Config{})
	if d.Level() != 0 {
		t.Fatalf("Level before audio = %v, want 0", d.Level())
	}
	d.Write(pcm(tone(23170, 20*time.Millisecond))) // about -3 dBFS
	if l := d.Level(); l < 0.93 || l > 0.97 {
		t.Fatalf("Level of a -3 dBFS tone = %.3f, want about 0.95", l)
	}
	d.Write(pcm(tone(33, 20*time.Millisecond))) // about -60 dBFS
	if l := d.Level(); l > 0.01 {
		t.Fatalf("Level of a -60 dBFS tone = %.3f, want about 0", l)
	}
	d.Write(pcm(silence(20 * time.Millisecond)))
	if d.Level() != 0 {
		t.Fatalf("Level of silence = %v, want 0", d.Level())
	}
}

func TestResetKeepsAnOddByteSoTheSamplesStayAligned(t *testing.T) {
	hiss := pcm(noise(30, time.Second, 7))
	d := New(Config{})
	d.Write(hiss[:len(hiss)-1])
	d.Reset()
	// The byte that completes the last sample, then more hiss.
	rest := append([]byte{hiss[len(hiss)-1]}, pcm(noise(30, 9*time.Second, 8))...)
	if evs := feed(d, rest, 3201); len(evs) != 1 || evs[0].Kind != NoSpeech {
		t.Fatalf("hiss after a Reset mid-sample: events = %+v, want only NoSpeech", evs)
	}
}

func TestTheNoiseFloorIsHeldWhileTheReeveSpeaks(t *testing.T) {
	d := New(Config{})
	feed(d, pcm(noise(100, 4*time.Second, 3)), 3200)
	before := d.NoiseFloor()
	d.SetSpeaking(true)
	feed(d, pcm(tone(1000, 3*time.Second)), 3200)
	if after := d.NoiseFloor(); after != before {
		t.Fatalf("noise floor while speaking = %.1f, want it held at %.1f", after, before)
	}
}
