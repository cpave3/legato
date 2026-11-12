package service

import "testing"

func TestAnAudioFilesTypeIsReadFromItsStart(t *testing.T) {
	for _, c := range []struct {
		head, ext, typ string
	}{
		{"ID3\x04\x00", ".mp3", "audio/mpeg"},
		{"\xff\xfb\x90\x64", ".mp3", "audio/mpeg"},
		{"RIFF\x00\x00\x00\x00WAVEfmt ", ".wav", "audio/wav"},
		{"OggS\x00\x02", ".ogg", "audio/ogg"},
		{"hello", ".mp3", "audio/mpeg"},
	} {
		ext, typ := AudioType([]byte(c.head))
		if ext != c.ext || typ != c.typ {
			t.Errorf("%q: %s %s", c.head, ext, typ)
		}
	}
}
