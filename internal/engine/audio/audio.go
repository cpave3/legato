// Package audio provides microphone recording via arecord (ALSA) for the
// voice dictation feature. It captures raw 16-bit PCM audio and computes
// rolling amplitude levels for the TUI waveform display.
//
// arecord is Linux/ALSA-specific. The package uses an injectable command
// factory at the system boundary so tests don't need a real audio device.
package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// levelWindow is the number of amplitude bars kept for the waveform display.
const levelWindow = 40

// levelChunkMs is the duration each amplitude bar represents.
const levelChunkMs = 50

// sampleRate is the recording sample rate. 16kHz mono is what
// wyoming-faster-whisper expects for efficient transcription.
const sampleRate = 16000

// channels is 1 (mono) for whisper transcription.
const channels = 1

// bytesPerSample is 2 for 16-bit audio.
const bytesPerSample = 2

// Recorder captures audio via arecord and tracks amplitude levels for
// the TUI waveform. Audio is piped through the Go process (stdout) rather
// than written to a file by arecord, so amplitude updates are real-time.
// Safe for concurrent use.
type Recorder struct {
	mu       sync.Mutex
	lookPath func(name string) (string, error)
	started  bool
	cmd      *exec.Cmd
	pcmBuf   []byte   // accumulated raw PCM from the pipe
	pcmPath  string   // temp file path written by Stop, cleaned by Cleanup
	levels   []float64
	stopChan chan struct{}
}

// New creates a Recorder that uses exec.LookPath to find arecord.
func New() *Recorder {
	return &Recorder{
		lookPath: exec.LookPath,
	}
}

// NewWithLookPath creates a Recorder with a custom LookPath function for testing.
func NewWithLookPath(lookPath func(string) (string, error)) (*Recorder, error) {
	if _, err := lookPath("arecord"); err != nil {
		return nil, fmt.Errorf("arecord not found: %w", err)
	}
	return &Recorder{lookPath: lookPath}, nil
}

// IsRecording reports whether recording is currently active.
func (r *Recorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started
}

// Start begins recording audio by piping arecord's stdout into the Go process.
// The device can be an ALSA hw: or "default" identifier. Returns an error if
// already recording or arecord is unavailable.
func (r *Recorder) Start(device string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return fmt.Errorf("already recording")
	}

	arecordPath, err := r.lookPath("arecord")
	if err != nil {
		return fmt.Errorf("arecord not found: %w", err)
	}

	if device == "" {
		device = "default"
	}

	// Pipe stdout → we read raw PCM in real-time.
	r.cmd = exec.Command(arecordPath, "-q",
		"-D", device,
		"-r", "16000", "-c", "1", "-f", "S16_LE",
		"-t", "raw",
		"-",
	)

	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("starting arecord: %w", err)
	}

	r.started = true
	r.pcmBuf = make([]byte, 0)
	r.levels = make([]float64, 0, levelWindow)
	r.stopChan = make(chan struct{})

	// Read PCM from the pipe in real-time: accumulate for transcription.
	go r.readPipe(stdout)

	// Compute rolling amplitude levels for the waveform display.
	go r.trackLevels()

	return nil
}

// readPipe reads raw PCM from arecord's stdout. It accumulates the full
// audio in pcmBuf for transcription and computes amplitude levels on a
// timer for the waveform display.
func (r *Recorder) readPipe(stdout io.Reader) {
	// Copy audio into pcmBuf for later transcription.
	buf := make([]byte, 4096)
	for {
		select {
		case <-r.stopChan:
			return
		default:
		}
		n, err := stdout.Read(buf)
		if n > 0 {
			r.mu.Lock()
			r.pcmBuf = append(r.pcmBuf, buf[:n]...)
			r.mu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				// Pipe closed — arecord exited (likely killed).
			}
			return
		}
	}
}

// trackLevels runs a ticker that computes amplitude from the accumulated
// PCM buffer. Launched as a goroutine by Start.
func (r *Recorder) trackLevels() {
	ticker := time.NewTicker(time.Duration(levelChunkMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			level := r.computeCurrentLevel()
			r.mu.Lock()
			r.levels = append(r.levels, level)
			if len(r.levels) > levelWindow {
				r.levels = r.levels[len(r.levels)-levelWindow:]
			}
			r.mu.Unlock()
		}
	}
}

// computeCurrentLevel computes the RMS amplitude of the most recent chunk
// of accumulated PCM data.
func (r *Recorder) computeCurrentLevel() float64 {
	r.mu.Lock()
	pcm := r.pcmBuf
	r.mu.Unlock()

	if len(pcm) == 0 {
		return 0
	}

	// Read only the last chunk worth of data.
	chunkBytes := sampleRate * channels * bytesPerSample * levelChunkMs / 1000
	if len(pcm) > chunkBytes {
		pcm = pcm[len(pcm)-chunkBytes:]
	}

	levels := computeLevels(pcm, sampleRate*channels, levelChunkMs)
	if len(levels) == 0 {
		return 0
	}
	return levels[len(levels)-1]
}

// Stop stops recording and returns the path to a temp file containing the
// raw PCM data. The caller is responsible for cleaning up via Cleanup().
func (r *Recorder) Stop() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return "", fmt.Errorf("not recording")
	}

	close(r.stopChan)
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
		_, _ = r.cmd.Process.Wait()
	}
	r.started = false

	// Write accumulated PCM to a temp file for the transcription path.
	tmpDir, err := os.MkdirTemp("", "legato-voice-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	tmpPath := filepath.Join(tmpDir, "recording.pcm")
	if err := os.WriteFile(tmpPath, r.pcmBuf, 0644); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("writing PCM file: %w", err)
	}

	r.pcmPath = tmpPath

	return tmpPath, nil
}

// Cleanup removes the temp PCM file and its parent directory.
func (r *Recorder) Cleanup() {
	r.mu.Lock()
	path := r.pcmPath
	r.pcmPath = ""
	r.pcmBuf = nil
	r.mu.Unlock()
	if path != "" {
		os.RemoveAll(filepath.Dir(path))
	}
}

// Levels returns a copy of the current amplitude history (0.0–1.0), with the
// most recent level at the end. Safe for concurrent use.
func (r *Recorder) Levels() []float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.levels) == 0 {
		return nil
	}
	out := make([]float64, len(r.levels))
	copy(out, r.levels)
	return out
}

// computeLevels computes normalized RMS amplitude per chunk from raw 16-bit
// little-endian PCM data. Returns one float64 (0.0–1.0) per chunk of
// chunkDurationMs worth of samples.
func computeLevels(pcm []byte, sampleRate, chunkDurationMs int) []float64 {
	if len(pcm) == 0 {
		return nil
	}

	samplesPerChunk := sampleRate * chunkDurationMs / 1000
	if samplesPerChunk == 0 {
		samplesPerChunk = 1
	}
	bytesPerChunk := samplesPerChunk * bytesPerSample

	numChunks := (len(pcm) + bytesPerChunk - 1) / bytesPerChunk
	levels := make([]float64, 0, numChunks)

	for i := 0; i < len(pcm); i += bytesPerChunk {
		end := i + bytesPerChunk
		if end > len(pcm) {
			end = len(pcm)
		}
		chunk := pcm[i:end]

		var sumSq float64
		count := 0
		for j := 0; j+1 < len(chunk); j += 2 {
			sample := int16(binary.LittleEndian.Uint16(chunk[j : j+2]))
			normalized := float64(sample) / 32768.0
			sumSq += normalized * normalized
			count++
		}
		if count == 0 {
			levels = append(levels, 0)
			continue
		}
		rms := math.Sqrt(sumSq / float64(count))
		levels = append(levels, rms)
	}

	return levels
}
