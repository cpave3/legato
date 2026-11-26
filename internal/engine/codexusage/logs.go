package codexusage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// tailBytes is how much of the end of a rollout file is read: enough for
// many turns' token counts.
const tailBytes = 512 << 10

// logFiles is how many of the newest rollout files are tried before there
// is no data: a session that never finished a turn has no limits.
const logFiles = 10

// SessionsDir is where Codex keeps its session logs: $CODEX_HOME/sessions,
// else ~/.codex/sessions.
func SessionsDir(getenv func(string) string, home string) string {
	if dir := getenv("CODEX_HOME"); dir != "" {
		return filepath.Join(dir, "sessions")
	}
	return filepath.Join(home, ".codex", "sessions")
}

// ReadLogs returns the newest rate limits Codex wrote into the rollout
// files under root (sessions/YYYY/MM/DD/rollout-*.jsonl), as of the event
// that carried them. It only reads.
func ReadLogs(root string) (Reading, error) {
	files := newestRollouts(root, logFiles)
	for _, path := range files {
		if reading, ok := lastRateLimits(path); ok {
			return reading, nil
		}
	}
	return Reading{}, ErrNoData
}

type rollout struct {
	path string
	mod  time.Time
}

// newestRollouts lists the rollout files of the newest day folders, newest
// first by day, then by modification time.
func newestRollouts(root string, limit int) []string {
	days := dayFolders(root)
	var out []string
	for _, day := range days {
		entries, err := os.ReadDir(day)
		if err != nil {
			continue
		}
		var files []rollout
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "rollout-") || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, rollout{filepath.Join(day, e.Name()), info.ModTime()})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
		for _, f := range files {
			out = append(out, f.path)
			if len(out) == limit {
				return out
			}
		}
	}
	return out
}

// dayFolders lists root/YYYY/MM/DD, newest first.
func dayFolders(root string) []string {
	var days []string
	for _, year := range sortedDirs(root) {
		for _, month := range sortedDirs(year) {
			days = append(days, sortedDirs(month)...)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	return days
}

func sortedDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

type logLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type       string      `json:"type"`
		RateLimits *wireLimits `json:"rate_limits"`
	} `json:"payload"`
}

// lastRateLimits reads the tail of a rollout file for its last event with
// rate limits.
func lastRateLimits(path string) (Reading, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Reading{}, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Reading{}, false
	}
	offset := info.Size() - tailBytes
	if offset < 0 {
		offset = 0
	}
	data, err := io.ReadAll(io.NewSectionReader(file, offset, info.Size()-offset))
	if err != nil {
		return Reading{}, false
	}
	lines := bytes.Split(data, []byte("\n"))
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:] // the first line may be cut
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"rate_limits"`)) {
			continue
		}
		var line logLine
		if json.Unmarshal(lines[i], &line) != nil || line.Payload.RateLimits == nil {
			continue
		}
		reading, err := line.Payload.RateLimits.reading(SourceLogs)
		if err != nil {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, line.Timestamp); err == nil {
			reading.AsOf = at.UTC()
		}
		return reading, true
	}
	return Reading{}, false
}
