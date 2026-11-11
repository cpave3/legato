package service

// VoiceLine is a line the Reeve said, ready for the listening device.
type VoiceLine struct {
	ID   string
	Text string
	URL  string
}

// VoiceMarker starts every spoken message: the Reeve answers it by voice.
const VoiceMarker = "[legato voice]"

// VoiceMessage is a spoken turn as the Reeve is given it.
func VoiceMessage(text string) string { return VoiceMarker + " " + text }

func (s *VoiceSession) onLine(VoiceLine) {}
func (s *VoiceSession) onPlaying(string) {}
func (s *VoiceSession) onPlayed(string)  {}
func (s *VoiceSession) dropLines()       {}
