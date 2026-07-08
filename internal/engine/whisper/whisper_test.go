package whisper

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
)

func TestTranscribeReturnsText(t *testing.T) {
	srv := newWyomingTestServer(t, "hello world")
	defer srv.Close()

	c := NewClient(srv.Addr())
	text, err := c.Transcribe(context.Background(), fakePCM, 16000)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello world" {
		t.Errorf("got %q, want %q", text, "hello world")
	}
}

func TestTranscribePreservesWhitespace(t *testing.T) {
	srv := newWyomingTestServer(t, "  leading and trailing  ")
	defer srv.Close()

	c := NewClient(srv.Addr())
	text, err := c.Transcribe(context.Background(), fakePCM, 16000)
	if err != nil {
		t.Fatal(err)
	}
	if text != "leading and trailing" {
		t.Errorf("got %q, want %q (trimmed)", text, "leading and trailing")
	}
}

func TestTranscribeServerErr(t *testing.T) {
	srv := newWyomingErrorServer(t, "model not loaded")
	defer srv.Close()

	c := NewClient(srv.Addr())
	_, err := c.Transcribe(context.Background(), fakePCM, 16000)
	if err == nil {
		t.Fatal("expected error for server error event")
	}
}

func TestTranscribeConnectionRefused(t *testing.T) {
	c := NewClient("tcp://127.0.0.1:1") // port 1 should refuse
	_, err := c.Transcribe(context.Background(), fakePCM, 16000)
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestTranscribeSendsWyomingEvents(t *testing.T) {
	var receivedEvents []string
	var receivedPCM []byte
	srv := newWyomingInspectServer(t, "ok", func(eventType string, payload []byte) {
		receivedEvents = append(receivedEvents, eventType)
		if len(payload) > 0 {
			receivedPCM = append(receivedPCM, payload...)
		}
	})
	defer srv.Close()

	c := NewClient(srv.Addr())
	_, err := c.Transcribe(context.Background(), fakePCM, 16000)
	if err != nil {
		t.Fatal(err)
	}

	// Should see transcribe → audio-start → audio-chunk → audio-stop
	expected := []string{"transcribe", "audio-start", "audio-chunk", "audio-stop"}
	if len(receivedEvents) != len(expected) {
		t.Fatalf("received %d events, want %d: %v", len(receivedEvents), len(expected), receivedEvents)
	}
	for i, want := range expected {
		if receivedEvents[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, receivedEvents[i], want)
		}
	}

	// Should have received the PCM payload via audio-chunk.
	if len(receivedPCM) == 0 {
		t.Error("expected PCM payload in audio-chunk")
	}
}

func TestExtractPCM_Mono(t *testing.T) {
	// Minimal WAV: 16-bit mono, 16000 Hz, 4 samples of silence.
	wav := buildWAV(1, 16000, []byte{0, 0, 0, 0, 0, 0, 0, 0})
	pcm, sr, err := ExtractPCM(wav)
	if err != nil {
		t.Fatal(err)
	}
	if sr != 16000 {
		t.Errorf("sampleRate = %d, want 16000", sr)
	}
	if len(pcm) != 8 {
		t.Errorf("pcm length = %d, want 8 (4 samples * 2 bytes)", len(pcm))
	}
}

func TestExtractPCM_StereoDownmix(t *testing.T) {
	// 4 stereo samples: L=100, R=200 → mono = 150
	pcm := make([]byte, 16) // 4 samples * 2 channels * 2 bytes
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint16(pcm[i*4:i*4+2], 100)
		binary.LittleEndian.PutUint16(pcm[i*4+2:i*4+4], 200)
	}
	wav := buildWAV(2, 44100, pcm)
	out, sr, err := ExtractPCM(wav)
	if err != nil {
		t.Fatal(err)
	}
	if sr != 44100 {
		t.Errorf("sampleRate = %d, want 44100", sr)
	}
	if len(out) != 8 {
		t.Errorf("mono pcm length = %d, want 8 (4 samples * 2 bytes)", len(out))
	}
	for i := 0; i < 4; i++ {
		s := binary.LittleEndian.Uint16(out[i*2 : i*2+2])
		if s != 150 {
			t.Errorf("mono sample[%d] = %d, want 150", i, s)
		}
	}
}

func TestExtractPCM_NotWAV(t *testing.T) {
	_, _, err := ExtractPCM([]byte("not a wav file at all"))
	if err == nil {
		t.Error("expected error for non-WAV data")
	}
}

func TestExtractPCM_TooShort(t *testing.T) {
	_, _, err := ExtractPCM([]byte("RIFF"))
	if err == nil {
		t.Error("expected error for too-short WAV")
	}
}

// fakePCM is 4 bytes of silence (2 samples of 16-bit mono).
var fakePCM = []byte{0, 0, 0, 0}

// buildWAV creates a minimal WAV file with the given channels, sampleRate,
// and raw 16-bit PCM data.
func buildWAV(channels int, sampleRate int, pcm []byte) []byte {
	dataSize := len(pcm)
	byteRate := sampleRate * channels * 2
	blockAlign := channels * 2

	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(header[20:22], 1)   // PCM format
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:36], 16) // bits per sample
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))

	return append(header, pcm...)
}

// wyomingTestServer is a TCP server that mimics wyoming-faster-whisper.
type wyomingTestServer struct {
	ln      net.Listener
	transcript string
	inspect  func(eventType string, payload []byte)
	sendError bool
	errorMsg  string
}

func newWyomingTestServer(t *testing.T, transcript string) *wyomingTestServer {
	t.Helper()
	return newWyomingServerWithOpts(t, transcript, nil, false, "")
}

func newWyomingErrorServer(t *testing.T, msg string) *wyomingTestServer {
	t.Helper()
	return newWyomingServerWithOpts(t, "", nil, true, msg)
}

func newWyomingInspectServer(t *testing.T, transcript string, inspect func(string, []byte)) *wyomingTestServer {
	t.Helper()
	return newWyomingServerWithOpts(t, transcript, inspect, false, "")
}

func newWyomingServerWithOpts(t *testing.T, transcript string, inspect func(string, []byte), sendError bool, errorMsg string) *wyomingTestServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &wyomingTestServer{
		ln:         ln,
		transcript: transcript,
		inspect:    inspect,
		sendError:  sendError,
		errorMsg:   errorMsg,
	}
	go srv.serve()
	return srv
}

func (s *wyomingTestServer) Close() { s.ln.Close() }
func (s *wyomingTestServer) Addr() string { return s.ln.Addr().String() }

func (s *wyomingTestServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *wyomingTestServer) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var event wyomingEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		// Consume payload if present.
		var payload []byte
		if event.PayloadLength > 0 {
			payload = make([]byte, event.PayloadLength)
			if _, err := readFull(reader, payload); err != nil {
				return
			}
		}

		if s.inspect != nil {
			s.inspect(event.Type, payload)
		}

		// When we receive audio-stop, send the response.
		if event.Type == "audio-stop" {
			if s.sendError {
				writeResponse(conn, wyomingEvent{
					Type: "error",
					Data: map[string]interface{}{"message": s.errorMsg},
				})
			} else {
				data, _ := json.Marshal(map[string]string{"text": s.transcript})
				writeResponse(conn, wyomingEvent{
					Type:       "transcript",
					DataLength: len(data),
				})
				conn.Write(data)
			}
			return
		}
	}
}

func writeResponse(w net.Conn, event wyomingEvent) {
	data, _ := json.Marshal(event)
	w.Write(append(data, '\n'))
}

func readFull(r *bufio.Reader, buf []byte) ([]byte, error) {
	remaining := buf
	for len(remaining) > 0 {
		n, err := r.Read(remaining)
		if err != nil {
			return nil, err
		}
		remaining = remaining[n:]
	}
	return buf, nil
}
