// Package whisper provides a client for wyoming-faster-whisper using the
// Wyoming protocol (raw TCP). The Wyoming protocol sends JSON event headers
// terminated by newlines, followed by optional binary payloads whose length
// is specified by the payload_length field in the header.
//
// The transcription flow is:
//  1. Send a "transcribe" event with the language
//  2. Send an "audio-start" event with the audio format (rate, width, channels)
//  3. Send "audio-chunk" events with raw 16-bit PCM payloads
//  4. Send an "audio-stop" event
//  5. Read the "transcript" event response containing the transcribed text
package whisper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

// Client transcribes audio via a wyoming-faster-whisper server.
type Client struct {
	host string
	port string
}

// NewClient creates a whisper transcription client targeting the given
// Wyoming protocol URL (e.g. "tcp://192.168.1.220:10300").
func NewClient(rawURL string) *Client {
	// Allow bare "host:port" for convenience; parse to extract host/port.
	host := rawURL
	port := "10300"
	if u, err := url.Parse(rawURL); err == nil && u.Scheme == "tcp" {
		host = u.Hostname()
		if u.Port() != "" {
			port = u.Port()
		}
	} else if h, p, err := net.SplitHostPort(rawURL); err == nil {
		host = h
		port = p
	}
	return &Client{host: host, port: port}
}

// wyomingEvent is the JSON header sent over the TCP connection.
type wyomingEvent struct {
	Type           string                 `json:"type"`
	Data           map[string]interface{} `json:"data,omitempty"`
	DataLength     int                    `json:"data_length,omitempty"`
	PayloadLength  int                    `json:"payload_length,omitempty"`
}

// Transcribe sends raw 16-bit little-endian PCM audio (mono) to the Wyoming
// server and returns the transcribed text. The sampleRate must match the
// audio's sample rate (e.g. 16000).
func (c *Client) Transcribe(ctx context.Context, pcmData []byte, sampleRate int) (string, error) {
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	conn, err = d.DialContext(ctx, "tcp", net.JoinHostPort(c.host, c.port))
	if err != nil {
		return "", fmt.Errorf("whisper: connecting to %s:%s: %w", c.host, c.port, err)
	}
	defer conn.Close()

	// Set a deadline based on the context.
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	writer := bufio.NewWriter(conn)
	reader := bufio.NewReader(conn)

	// 1. Send transcribe event
	if err := writeEvent(writer, wyomingEvent{
		Type: "transcribe",
		Data: map[string]interface{}{"language": "en"},
	}); err != nil {
		return "", fmt.Errorf("whisper: sending transcribe event: %w", err)
	}

	// 2. Send audio-start
	if err := writeEvent(writer, wyomingEvent{
		Type: "audio-start",
		Data: map[string]interface{}{
			"rate":     sampleRate,
			"width":    2,
			"channels": 1,
		},
	}); err != nil {
		return "", fmt.Errorf("whisper: sending audio-start: %w", err)
	}

	// 3. Send audio-chunk(s) — chunk to avoid overwhelming the server.
	const chunkSize = 32000 // ~1s at 16kHz 16-bit mono
	for offset := 0; offset < len(pcmData); offset += chunkSize {
		end := offset + chunkSize
		if end > len(pcmData) {
			end = len(pcmData)
		}
		chunk := pcmData[offset:end]
		if err := writeEventWithPayload(writer, wyomingEvent{
			Type: "audio-chunk",
			Data: map[string]interface{}{
				"rate":     sampleRate,
				"width":    2,
				"channels": 1,
			},
		}, chunk); err != nil {
			return "", fmt.Errorf("whisper: sending audio-chunk: %w", err)
		}
	}

	// 4. Send audio-stop
	if err := writeEvent(writer, wyomingEvent{
		Type: "audio-stop",
		Data: map[string]interface{}{},
	}); err != nil {
		return "", fmt.Errorf("whisper: sending audio-stop: %w", err)
	}

	if err := writer.Flush(); err != nil {
		return "", fmt.Errorf("whisper: flushing: %w", err)
	}

	// 5. Read response events until we get a transcript.
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return "", fmt.Errorf("whisper: reading response: %w", err)
		}

		line = bytes.TrimRight(line, "\n\r")
		if len(line) == 0 {
			continue
		}

		var event wyomingEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		// Consume any data_length bytes following the header.
		if event.DataLength > 0 {
			dataBuf := make([]byte, event.DataLength)
			if _, err := io.ReadFull(reader, dataBuf); err != nil {
				return "", fmt.Errorf("whisper: reading data: %w", err)
			}
			// The transcript text may be in the data_length content.
			if event.Type == "transcript" {
				var data struct {
					Text string `json:"text"`
				}
				if json.Unmarshal(dataBuf, &data); err == nil && data.Text != "" {
					return strings.TrimSpace(data.Text), nil
				}
			}
		}

		// Consume any payload_length bytes following the header.
		if event.PayloadLength > 0 {
			payloadBuf := make([]byte, event.PayloadLength)
			if _, err := io.ReadFull(reader, payloadBuf); err != nil {
				return "", fmt.Errorf("whisper: reading payload: %w", err)
			}
		}

		// Transcript text may be in the header data directly.
		if event.Type == "transcript" {
			if text, ok := event.Data["text"].(string); ok && text != "" {
				return strings.TrimSpace(text), nil
			}
		}

		if event.Type == "error" {
			msg := "unknown error"
			if m, ok := event.Data["message"].(string); ok {
				msg = m
			}
			return "", fmt.Errorf("whisper: server error: %s", msg)
		}
	}
}

// writeEvent writes a JSON event header followed by a newline.
func writeEvent(w io.Writer, event wyomingEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// writeEventWithPayload writes a JSON event header with payload_length set,
// followed by a newline, then the raw binary payload.
func writeEventWithPayload(w io.Writer, event wyomingEvent, payload []byte) error {
	event.PayloadLength = len(payload)
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// ExtractPCM reads a WAV file and returns the raw 16-bit PCM data, skipping
// the WAV header. Also returns the sample rate and channel count from the
// header. For stereo audio, the channels are downmixed to mono.
func ExtractPCM(wavData []byte) (pcm []byte, sampleRate int, err error) {
	if len(wavData) < 44 {
		return nil, 0, fmt.Errorf("WAV too short")
	}
	if string(wavData[0:4]) != "RIFF" || string(wavData[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("not a RIFF/WAVE file")
	}

	// Walk subchunks to find fmt and data.
	offset := 12
	var format, channels, bitsPerSample uint16
	var sr uint32
	var dataOffset, dataSize int

	for offset+8 <= len(wavData) {
		id := string(wavData[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(wavData[offset+4 : offset+8]))
		bodyStart := offset + 8

		if id == "fmt " {
			format = binary.LittleEndian.Uint16(wavData[bodyStart : bodyStart+2])
			channels = binary.LittleEndian.Uint16(wavData[bodyStart+2 : bodyStart+4])
			sr = binary.LittleEndian.Uint32(wavData[bodyStart+4 : bodyStart+8])
			bitsPerSample = binary.LittleEndian.Uint16(wavData[bodyStart+14 : bodyStart+16])
		} else if id == "data" {
			dataOffset = bodyStart
			dataSize = size
			break
		}

		// Chunks are word-aligned.
		offset = bodyStart + size + (size % 2)
	}

	if format != 1 {
		return nil, 0, fmt.Errorf("unsupported WAV format %d (only PCM)", format)
	}
	if bitsPerSample != 16 {
		return nil, 0, fmt.Errorf("unsupported bit depth %d (only 16-bit)", bitsPerSample)
	}

	if dataOffset == 0 || dataOffset+dataSize > len(wavData) {
		return nil, 0, fmt.Errorf("WAV missing or truncated data chunk")
	}

	raw := wavData[dataOffset : dataOffset+dataSize]

	if channels == 1 {
		return raw, int(sr), nil
	}

	// Downmix stereo → mono.
	if channels == 2 {
		sampleCount := len(raw) / 4 // 2 bytes per sample * 2 channels
		mono := make([]byte, sampleCount*2)
		for i := 0; i < sampleCount; i++ {
			left := int16(binary.LittleEndian.Uint16(raw[i*4 : i*4+2]))
			right := int16(binary.LittleEndian.Uint16(raw[i*4+2 : i*4+4]))
			mixed := int16((int32(left) + int32(right)) / 2)
			binary.LittleEndian.PutUint16(mono[i*2:i*2+2], uint16(mixed))
		}
		return mono, int(sr), nil
	}

	return nil, 0, fmt.Errorf("unsupported channel count %d", channels)
}
