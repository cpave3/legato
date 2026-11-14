package service

import (
	"context"
	"slices"
	"syscall"
	"testing"
	"time"
)

// Without a process namespace (Seatbelt), what an agent started and
// detached can outlive its pane. When the execution ends, every process
// that still carries its ID is ended: asked first, then killed.
func TestTheProcessesAnExecutionLeftAreEnded(t *testing.T) {
	alive := map[int]bool{11: true, 12: true, 13: true, 14: true}
	table := func(context.Context) ([]HostProcess, error) {
		var out []HostProcess
		for pid, ok := range alive {
			if !ok {
				continue
			}
			words := map[int][]string{
				11: {"/usr/bin/sleep", "600", "LEGATO_EXECUTION_ID=ex-1", "PATH=/usr/bin"},
				12: {"node", "server.js", "LEGATO_EXECUTION_ID=ex-1"},
				13: {"node", "server.js", "LEGATO_EXECUTION_ID=ex-10"},
				14: {"grep", "LEGATO_EXECUTION_ID=ex-1x"},
			}[pid]
			out = append(out, HostProcess{PID: pid, Words: words})
		}
		return out, nil
	}
	var signals []string
	kill := func(pid int, sig syscall.Signal) error {
		signals = append(signals, sig.String()+":"+string(rune('0'+pid%10)))
		if pid == 11 { // 12 ignores SIGTERM
			alive[pid] = false
		}
		if sig == syscall.SIGKILL {
			alive[pid] = false
		}
		return nil
	}
	end := EndSandboxProcesses(table, kill, 10*time.Millisecond)
	end(context.Background(), "ex-1")
	slices.Sort(signals)
	want := []string{"killed:2", "terminated:1", "terminated:2"}
	if !slices.Equal(signals, want) {
		t.Fatalf("signals %q, want %q", signals, want)
	}
	if !alive[13] || !alive[14] {
		t.Fatal("another execution's process was ended")
	}
}
