package service

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"
)

// VoiceMarker starts every spoken message: the Reeve answers it by voice.
const VoiceMarker = "[legato voice]"

// VoiceMessage is a spoken turn as the Reeve is given it.
func VoiceMessage(text string) string { return VoiceMarker + " " + text }

const (
	// voiceClipTTL is how long a line's audio is kept for its device.
	voiceClipTTL = 10 * time.Minute
	// voiceAnswerTTL is how long a voice message counts as being
	// answered when its session has gone.
	voiceAnswerTTL = 10 * time.Minute
	// VoiceLinePath is where a device fetches a line's audio.
	VoiceLinePath = "/api/reeve/voice/lines/"
)

// VoiceLine is a line the Reeve said, ready for the listening device.
type VoiceLine struct {
	ID   string
	Text string
	URL  string
}

// voiceClip is a line's audio, kept for the device until it has played.
type voiceClip struct {
	path        string
	contentType string
	expires     time.Time
	session     *VoiceSession
}

func (v *ReeveVoice) now() time.Time {
	if v.opts.Now != nil {
		return v.opts.Now()
	}
	return time.Now()
}

// AnsweringVoice reports whether the Reeve is answering a spoken message:
// its lines then play even in quiet hours, and are dropped when the
// session that asked has gone.
func (v *ReeveVoice) AnsweringVoice() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.answeringLocked()
}

func (v *ReeveVoice) answeringLocked() bool {
	return !v.answeredAt.IsZero() && v.now().Sub(v.answeredAt) < voiceAnswerTTL
}

// setAnswering marks a voice message delivered (true) or its turn over.
func (v *ReeveVoice) setAnswering(on bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.answeredAt, v.dropTurn = time.Time{}, false
	if on {
		v.answeredAt = v.now()
	}
}

// Deliver routes a line the Reeve made (SpeechOutput). A line that answers
// no spoken message plays on the host's speakers. One that does goes to the
// session's device when it plays lines, else to the host; it is dropped when
// the person stopped that turn, or its session has gone.
func (v *ReeveVoice) Deliver(line SpokenLine, path, contentType string) LineRoute {
	v.mu.Lock()
	v.sweepLocked()
	s, answering, dropping := v.session, v.answeringLocked(), v.dropTurn
	switch {
	case !answering:
		v.mu.Unlock()
		return RouteHost
	case dropping, s == nil:
		v.mu.Unlock()
		return RouteDrop
	case !s.hello.Play:
		v.mu.Unlock()
		return RouteHost
	}
	id := newClipID()
	v.clips[id] = &voiceClip{path: path, contentType: contentType, expires: v.now().Add(v.clipTTL()), session: s}
	v.mu.Unlock()
	if !s.put(voiceInput{line: &VoiceLine{ID: id, Text: line.Text, URL: VoiceLinePath + id}}) {
		// The session ended meanwhile.
		v.release(id)
		return RouteDrop
	}
	return RouteDevice
}

// Clip is a line's audio for its device, while it is kept.
func (v *ReeveVoice) Clip(id string) (path, contentType string, ok bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.sweepLocked()
	c, ok := v.clips[id]
	if !ok {
		return "", "", false
	}
	return c.path, c.contentType, true
}

func (v *ReeveVoice) clipTTL() time.Duration {
	if v.opts.ClipTTL > 0 {
		return v.opts.ClipTTL
	}
	return voiceClipTTL
}

// release removes a line's audio.
func (v *ReeveVoice) release(id string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if c, ok := v.clips[id]; ok {
		_ = os.Remove(c.path)
		delete(v.clips, id)
	}
}

// releaseSession removes the audio of every line sent to a session.
func (v *ReeveVoice) releaseSession(s *VoiceSession) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for id, c := range v.clips {
		if c.session == s {
			_ = os.Remove(c.path)
			delete(v.clips, id)
		}
	}
}

func (v *ReeveVoice) sweepLocked() {
	now := v.now()
	for id, c := range v.clips {
		if now.After(c.expires) {
			_ = os.Remove(c.path)
			delete(v.clips, id)
		}
	}
}

func newClipID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// onLine announces a line to the device; the session speaks until the
// device has played every line.
func (s *VoiceSession) onLine(line VoiceLine) {
	if s.state == VoiceIdle && s.sent.IsZero() {
		// The turn was given up: nobody waits for the line.
		s.v.release(line.ID)
		return
	}
	if s.times.firstLine.IsZero() {
		s.times.firstLine, s.times.lineMade = time.Now(), time.Now()
	}
	s.spoke, s.sawWorking = true, true
	s.lines = append(s.lines, line)
	s.send(VoiceOut{Type: "line", ID: line.ID, Text: line.Text, URL: line.URL})
	if s.state != VoiceSpeaking {
		s.grace = false
		s.setState(VoiceSpeaking, "")
	}
}

func (s *VoiceSession) onPlaying(id string) {
	s.playing = id
	if s.times.playing.IsZero() {
		s.times.playing = time.Now()
	}
}

func (s *VoiceSession) onPlayed(id string) {
	s.v.release(id)
	for i, l := range s.lines {
		if l.ID == id {
			s.lines = append(s.lines[:i], s.lines[i+1:]...)
			break
		}
	}
	if s.playing == id {
		s.playing = ""
	}
	if s.state != VoiceSpeaking || len(s.lines) > 0 {
		return
	}
	if s.sent.IsZero() {
		// The Reeve's turn is over and its last line has played.
		s.afterSpeaking()
		return
	}
	s.setState(VoiceThinking, "")
}

// dropLines forgets the lines the device has not played, and any more
// the Reeve says in this turn.
func (s *VoiceSession) dropLines() {
	for _, l := range s.lines {
		s.v.release(l.ID)
	}
	s.lines, s.playing = nil, ""
	if !s.sent.IsZero() {
		s.v.mu.Lock()
		s.v.dropTurn = true
		s.v.mu.Unlock()
	}
}
