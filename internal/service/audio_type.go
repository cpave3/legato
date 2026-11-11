package service

import (
	"bytes"
	"os"
	"strings"
)

// AudioType reads an audio file's kind from its first bytes: WAV (RIFF
// and WAVE), Ogg, or MP3 (an ID3 tag or a frame), which is also what an
// unknown start is taken for, as voice commands mostly write MP3.
func AudioType(head []byte) (ext, contentType string) {
	switch {
	case len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WAVE")):
		return ".wav", "audio/wav"
	case bytes.HasPrefix(head, []byte("OggS")):
		return ".ogg", "audio/ogg"
	}
	return ".mp3", "audio/mpeg"
}

// nameByType renames a line's audio to the extension its content has,
// and returns the new path and its type. The command is still given a
// .mp3 path, which commands that pick a format from the name rely on.
func nameByType(path string) (string, string) {
	head := make([]byte, 12)
	f, err := os.Open(path)
	if err != nil {
		return path, "audio/mpeg"
	}
	n, _ := f.Read(head)
	f.Close()
	ext, contentType := AudioType(head[:n])
	if strings.HasSuffix(path, ext) {
		return path, contentType
	}
	renamed := strings.TrimSuffix(path, ".mp3") + ext
	if os.Rename(path, renamed) != nil {
		return path, contentType
	}
	return renamed, contentType
}
