package gitrepo

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Progress is one line of git's progress: what it is doing and how far.
type Progress struct {
	// Phase is git's own name for the step: "Receiving objects",
	// "Resolving deltas", "Counting objects".
	Phase   string
	Percent int
	// Received and Rate are set while objects are received:
	// "310.00 MiB" and "3.00 MiB/s".
	Received string
	Rate     string
}

var progressLine = regexp.MustCompile(`^(?:remote: )?([A-Za-z][A-Za-z ]*?):\s+(\d{1,3})% \(\d+/\d+\)(?:, ([\d.]+ [KMGT]?i?B)(?: \| ([\d.]+ [KMGT]?i?B/s))?)?`)

// ParseProgress reads one line of git's progress output; ok is false for
// any other line.
func ParseProgress(line string) (Progress, bool) {
	m := progressLine.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return Progress{}, false
	}
	percent, err := strconv.Atoi(m[2])
	if err != nil || percent > 100 {
		return Progress{}, false
	}
	return Progress{Phase: m[1], Percent: percent, Received: m[3], Rate: m[4]}, true
}

// stderrTail is the most of git's output kept to say what went wrong.
const stderrTail = 8 << 10

// progressWriter is git's standard error: each progress line goes to
// report as it comes (git ends them with \r while it counts), and the end
// of the output is kept for an error.
type progressWriter struct {
	mu      sync.Mutex
	report  func(Progress)
	pending []byte
	tail    []byte
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tail = append(w.tail, p...)
	if len(w.tail) > stderrTail {
		w.tail = w.tail[len(w.tail)-stderrTail:]
	}
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexAny(w.pending, "\r\n")
		if i < 0 {
			break
		}
		if progress, ok := ParseProgress(string(w.pending[:i])); ok && w.report != nil {
			w.report(progress)
		}
		w.pending = w.pending[i+1:]
	}
	if len(w.pending) > stderrTail {
		w.pending = w.pending[len(w.pending)-stderrTail:]
	}
	return len(p), nil
}

func (w *progressWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.tail)
}
