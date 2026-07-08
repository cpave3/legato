package audio

import (
	"os/exec"
	"testing"
)

func TestComputeLevels(t *testing.T) {
	// 16-bit PCM, mono, 44100 Hz. Each sample is 2 bytes (little-endian).
	// Create 4 chunks of ~50ms each. At 44100 Hz, 50ms = 2205 samples = 4410 bytes.
	// Use simple amplitude patterns: silent, loud, medium, silent.
	sampleRate := 44100
	chunkDurationMs := 50
	samplesPerChunk := sampleRate * chunkDurationMs / 1000 // 2205
	bytesPerSample := 2
	chunkBytes := samplesPerChunk * bytesPerSample

	// Build PCM data: chunk 0 = silence (zeros), chunk 1 = loud (max amplitude),
	// chunk 2 = medium (half amplitude), chunk 3 = silence.
	pcm := make([]byte, chunkBytes*4)
	for i := 0; i < samplesPerChunk; i++ {
		// Chunk 1: max amplitude (32767)
		off := chunkBytes + i*2
		pcm[off] = 0xFF
		pcm[off+1] = 0x7F
		// Chunk 2: half amplitude (16384)
		off2 := chunkBytes*2 + i*2
		pcm[off2] = 0x00
		pcm[off2+1] = 0x40
	}

	levels := computeLevels(pcm, sampleRate, chunkDurationMs)

	if len(levels) != 4 {
		t.Fatalf("got %d levels, want 4", len(levels))
	}

	// Chunk 0: silence → ~0
	if levels[0] > 0.01 {
		t.Errorf("level[0] = %f, want ~0 (silence)", levels[0])
	}
	// Chunk 1: max amplitude → highest
	if levels[1] < 0.9 {
		t.Errorf("level[1] = %f, want >0.9 (loud)", levels[1])
	}
	// Chunk 2: half amplitude → ~0.5
	if levels[2] < 0.3 || levels[2] > 0.7 {
		t.Errorf("level[2] = %f, want ~0.5 (medium)", levels[2])
	}
	// Chunk 3: silence → ~0
	if levels[3] > 0.01 {
		t.Errorf("level[3] = %f, want ~0 (silence)", levels[3])
	}

	// Loud should be louder than medium
	if levels[1] <= levels[2] {
		t.Errorf("level[1] (%f) should be > level[2] (%f)", levels[1], levels[2])
	}
}

func TestComputeLevelsEmpty(t *testing.T) {
	levels := computeLevels(nil, 44100, 50)
	if len(levels) != 0 {
		t.Errorf("got %d levels for nil input, want 0", len(levels))
	}
}

func TestRecorderNotRecordingByDefault(t *testing.T) {
	r := New()
	if r.IsRecording() {
		t.Error("new recorder should not be recording")
	}
}

func TestRecorderStartFailsWhenAlreadyRecording(t *testing.T) {
	r := New()
	r.started = true // simulate already recording
	err := r.Start("default")
	if err == nil {
		t.Error("expected error when starting while already recording")
	}
}

func TestRecorderStopFailsWhenNotRecording(t *testing.T) {
	r := New()
	_, err := r.Stop()
	if err == nil {
		t.Error("expected error when stopping while not recording")
	}
}

func TestNewRecorderFailsWhenArecordNotFound(t *testing.T) {
	_, err := NewWithLookPath(func(name string) (string, error) {
		return "", exec.ErrNotFound
	})
	if err == nil {
		t.Error("expected error when arecord not found")
	}
}
