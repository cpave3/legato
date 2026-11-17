package mcpstdio

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

// proc is a process as /proc names it: its ID and its start time, so a
// reused ID is not taken for it.
type proc struct {
	pid   int
	start string
}

func statOf(pid int) (ppid int, start string, ok bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, "", false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, "", false
	}
	// After the command: state, ppid, ... starttime is field 22 overall,
	// the 20th after the command.
	fields := strings.Fields(s[i+1:])
	if len(fields) < 20 || fields[0] == "Z" {
		return 0, "", false
	}
	ppid, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, "", false
	}
	return ppid, fields[19], true
}

func (p proc) same() bool {
	_, start, ok := statOf(p.pid)
	return ok && start == p.start
}

// descendants are the processes below root now.
func descendants(root int) []proc {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	children := map[int][]proc{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		ppid, start, ok := statOf(pid)
		if !ok {
			continue
		}
		children[ppid] = append(children[ppid], proc{pid: pid, start: start})
	}
	out := []proc{}
	queue := []int{root}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, c := range children[next] {
			out = append(out, c)
			queue = append(queue, c.pid)
		}
	}
	return out
}
