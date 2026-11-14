package service

import (
	"context"
	"log"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// HostProcess is a process of the host as `ps` shows it: its PID, and its
// command line and environment as words.
type HostProcess struct {
	PID   int
	Words []string
}

// ProcessTable lists the person's processes with their environments.
type ProcessTable func(ctx context.Context) ([]HostProcess, error)

// EndSandboxProcesses ends what an execution left running, for a backend
// with no process namespace (Seatbelt), where a process the agent started
// and detached outlives its pane: every process whose environment still
// names the execution (LEGATO_EXECUTION_ID) is sent SIGTERM, and those
// still there after grace SIGKILL. They stay in the sandbox meanwhile; a
// process that cleared its environment is not found (docs/ai/sandbox.md).
func EndSandboxProcesses(table ProcessTable, kill func(pid int, sig syscall.Signal) error, grace time.Duration) func(ctx context.Context, executionID string) {
	find := func(ctx context.Context, marker string) []int {
		procs, err := table(ctx)
		if err != nil {
			log.Printf("sandbox: listing processes: %v", err)
			return nil
		}
		var pids []int
		for _, p := range procs {
			if p.PID != os.Getpid() && slices.Contains(p.Words, marker) {
				pids = append(pids, p.PID)
			}
		}
		return pids
	}
	return func(ctx context.Context, executionID string) {
		if executionID == "" {
			return
		}
		marker := "LEGATO_EXECUTION_ID=" + executionID
		left := find(ctx, marker)
		for _, pid := range left {
			_ = kill(pid, syscall.SIGTERM)
		}
		if len(left) == 0 {
			return
		}
		time.Sleep(grace)
		for _, pid := range find(ctx, marker) {
			_ = kill(pid, syscall.SIGKILL)
		}
	}
}

// DarwinProcessTable is the person's processes on macOS, with their
// environments: `ps -E`, run by Legato on the host (a sandbox runs no
// setuid program, and ps is one).
func DarwinProcessTable(ctx context.Context) ([]HostProcess, error) {
	out, err := exec.CommandContext(ctx, "/bin/ps", "-E", "-ax", "-ww", "-o", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}
	var procs []HostProcess
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		procs = append(procs, HostProcess{PID: pid, Words: fields[1:]})
	}
	return procs, nil
}
