package codexusage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary is also the fake `codex app-server` the app-server tests
// run: FAKE_CODEX says how it answers.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_CODEX"); mode != "" {
		fakeAppServer(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A reply in the shape `account/rateLimits/read` gives (openai/codex PR
// #5302), the one this package relies on.
const rateLimitsResult = `{"rateLimits":{"primary":{"usedPercent":5.0,"windowDurationMins":10080,"resetsAt":1795252211},"secondary":{"usedPercent":61.5,"windowDurationMins":300,"resetsAt":1795200000},"credits":{"hasCredits":true,"unlimited":false,"balance":"62500.0"},"planType":"pro"}}`

func fakeAppServer(mode string) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "app-server" {
		fmt.Fprintln(os.Stderr, "expected app-server")
		os.Exit(2)
	}
	if mode == "hang" {
		time.Sleep(time.Minute)
		return
	}
	in := bufio.NewScanner(os.Stdin)
	initialized := false
	for in.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		json.Unmarshal(in.Bytes(), &msg)
		// A notification Codex sends whenever it likes; readers skip it.
		fmt.Println(`{"method":"account/updated","params":{}}`)
		switch msg.Method {
		case "initialize":
			fmt.Printf(`{"id":%s,"result":{"userAgent":"codex/0.169.0"}}`+"\n", msg.ID)
		case "initialized":
			initialized = true
		case "account/rateLimits/read":
			switch {
			case !initialized:
				fmt.Printf(`{"id":%s,"error":{"code":-32600,"message":"Not initialized"}}`+"\n", msg.ID)
			case mode == "ok":
				fmt.Printf(`{"id":%s,"result":%s}`+"\n", msg.ID, rateLimitsResult)
			case mode == "unknown-method":
				fmt.Printf(`{"id":%s,"error":{"code":-32601,"message":"Method not found"}}`+"\n", msg.ID)
			case mode == "signed-out":
				fmt.Printf(`{"id":%s,"error":{"code":-32603,"message":"codex account authentication required to read rate limits"}}`+"\n", msg.ID)
			case mode == "empty":
				fmt.Printf(`{"id":%s,"result":{"rateLimits":{"primary":null,"secondary":null}}}`+"\n", msg.ID)
			}
		}
	}
}

func fakeServer(t *testing.T, mode string) AppServer {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return AppServer{Command: exe, Env: append(os.Environ(), "FAKE_CODEX="+mode), Timeout: 5 * time.Second}
}

func TestClassifyByWindowLength(t *testing.T) {
	for minutes, want := range map[int]string{300: FiveHour, 360: FiveHour, 10080: Weekly, 10020: Weekly, 1440: "", 0: ""} {
		if got := Classify(minutes); got != want {
			t.Errorf("Classify(%d) = %q, want %q", minutes, got, want)
		}
	}
}

func TestAppServerReadsTheRateLimits(t *testing.T) {
	reading, err := fakeServer(t, "ok").Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []Window{
		{Minutes: 10080, UsedPercent: 5, ResetsAt: time.Unix(1795252211, 0).UTC()},
		{Minutes: 300, UsedPercent: 61.5, ResetsAt: time.Unix(1795200000, 0).UTC()},
	}
	if len(reading.Windows) != 2 || reading.Windows[0] != want[0] || reading.Windows[1] != want[1] {
		t.Fatalf("windows = %+v, want %+v", reading.Windows, want)
	}
	if reading.Credits == nil || !reading.Credits.HasCredits || reading.Credits.Unlimited || reading.Credits.Balance != "62500.0" {
		t.Fatalf("credits = %+v", reading.Credits)
	}
	if reading.PlanType != "pro" || reading.Source != SourceAppServer || !reading.AsOf.IsZero() {
		t.Fatalf("reading = %+v", reading)
	}
}

func TestAppServerFailuresAreTyped(t *testing.T) {
	for mode, want := range map[string]error{
		"unknown-method": ErrUnsupported,
		"signed-out":     ErrNotSignedIn,
		"empty":          ErrNoData,
	} {
		t.Run(mode, func(t *testing.T) {
			_, err := fakeServer(t, mode).Read(context.Background())
			if !errors.Is(err, want) {
				t.Fatalf("err = %v, want %v", err, want)
			}
		})
	}
}

func TestAMissingCodexIsReported(t *testing.T) {
	_, err := AppServer{Command: filepath.Join(t.TempDir(), "codex")}.Read(context.Background())
	if !errors.Is(err, ErrCodexMissing) {
		t.Fatalf("err = %v, want ErrCodexMissing", err)
	}
}

func TestAHungAppServerIsStoppedAtTheTimeout(t *testing.T) {
	server := fakeServer(t, "hang")
	server.Timeout = 300 * time.Millisecond
	start := time.Now()
	_, err := server.Read(context.Background())
	if err == nil {
		t.Fatal("a hung app server gave a reading")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Read took %s", elapsed)
	}
}

func writeRollout(t *testing.T, root, day, name string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, day)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func tokenCount(at, limits string) string {
	return `{"timestamp":"` + at + `","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":` + limits + `}}`
}

func TestLogsGiveTheNewestRateLimitsAsOfTheirEvent(t *testing.T) {
	root := t.TempDir()
	writeRollout(t, root, "2026/11/24", "rollout-2026-11-24T10-00-00-a.jsonl",
		tokenCount("2026-11-24T10:05:00.000Z", `{"limit_id":"codex","primary":{"used_percent":1.0,"window_minutes":10080,"resets_at":1795252211},"secondary":null}`))
	newest := writeRollout(t, root, "2026/11/25", "rollout-2026-11-25T09-00-00-b.jsonl",
		tokenCount("2026-11-25T09:01:00.000Z", `{"limit_id":"codex","primary":{"used_percent":4.0,"window_minutes":10080,"resets_at":1795252211},"secondary":null}`),
		// The ticket's sample, the newest event with limits.
		tokenCount("2026-11-25T09:30:00.500Z", `{"limit_id":"codex","primary":{"used_percent":5.0,"window_minutes":10080,"resets_at":1795252211},"secondary":null,"credits":{"has_credits":true,"unlimited":false,"balance":"62500.0"}}`),
		`{"timestamp":"2026-11-25T09:31:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{},"rate_limits":null}}`,
		`not json at all`,
		`{"timestamp":"2026-11-25T09:32:00.000Z","type":"response_item","payload":{"type":"message"}}`,
	)
	// An older day written later still loses to the newer day's file.
	writeRollout(t, root, "2026/11/23", "rollout-2026-11-23T08-00-00-c.jsonl",
		tokenCount("2026-11-23T08:00:00.000Z", `{"primary":{"used_percent":99.0,"window_minutes":300,"resets_at":1795252211}}`))
	_ = newest

	reading, err := ReadLogs(root)
	if err != nil {
		t.Fatalf("ReadLogs: %v", err)
	}
	if len(reading.Windows) != 1 || reading.Windows[0] != (Window{Minutes: 10080, UsedPercent: 5, ResetsAt: time.Unix(1795252211, 0).UTC()}) {
		t.Fatalf("windows = %+v", reading.Windows)
	}
	if !reading.AsOf.Equal(time.Date(2026, 11, 25, 9, 30, 0, 500e6, time.UTC)) {
		t.Fatalf("as of = %s", reading.AsOf)
	}
	if reading.Credits == nil || reading.Credits.Balance != "62500.0" || reading.Source != SourceLogs {
		t.Fatalf("reading = %+v", reading)
	}
}

func TestLogsWithoutRateLimitsHaveNoData(t *testing.T) {
	if _, err := ReadLogs(filepath.Join(t.TempDir(), "none")); !errors.Is(err, ErrNoData) {
		t.Fatalf("missing folder: err = %v", err)
	}
	root := t.TempDir()
	writeRollout(t, root, "2026/11/25", "rollout-x.jsonl", `{"timestamp":"2026-11-25T09:32:00.000Z","type":"response_item","payload":{}}`)
	if _, err := ReadLogs(root); !errors.Is(err, ErrNoData) {
		t.Fatalf("no limits: err = %v", err)
	}
}

// A session resumed from an earlier day stays in that day's folder; its
// newer event wins over an older one in today's folder.
func TestLogsPickTheNewestEventAcrossDayFolders(t *testing.T) {
	root := t.TempDir()
	today := writeRollout(t, root, "2026/11/26", "rollout-2026-11-26T08-00-00-new.jsonl",
		tokenCount("2026-11-26T09:00:00Z", `{"primary":{"used_percent":5.0,"window_minutes":10080,"resets_at":1795252211}}`))
	resumed := writeRollout(t, root, "2026/11/25", "rollout-2026-11-25T08-00-00-old.jsonl",
		tokenCount("2026-11-26T10:00:00Z", `{"primary":{"used_percent":85.0,"window_minutes":10080,"resets_at":1795252211}}`))
	// The resumed file was written last, as it would be.
	os.Chtimes(today, time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC), time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC))
	os.Chtimes(resumed, time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC), time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC))

	reading, err := ReadLogs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reading.Windows) != 1 || reading.Windows[0].UsedPercent != 85 {
		t.Fatalf("windows = %+v, want the resumed session's 85%%", reading.Windows)
	}
	// Even when the older event's file is the newer one, the newer event wins.
	os.Chtimes(today, time.Date(2026, 11, 26, 11, 0, 0, 0, time.UTC), time.Date(2026, 11, 26, 11, 0, 0, 0, time.UTC))
	if reading, _ := ReadLogs(root); reading.Windows[0].UsedPercent != 85 {
		t.Fatalf("windows = %+v, want 85%% by event time", reading.Windows)
	}
}

// A rollout FIFO an agent left in the sessions folder must not block.
func TestARolloutFIFODoesNotBlock(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2026", "11", "26")
	os.MkdirAll(dir, 0o700)
	if err := syscall.Mkfifo(filepath.Join(dir, "rollout-fifo.jsonl"), 0o600); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadLogs(root); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNoData) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
}
