package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type blockingProgram struct {
	quit chan struct{}
}

func (p *blockingProgram) Run() (tea.Model, error) {
	<-p.quit
	return nil, nil
}

func (p *blockingProgram) Quit() {
	select {
	case <-p.quit:
	default:
		close(p.quit)
	}
}

func TestRunProgramQuitsOnTerminationSignal(t *testing.T) {
	program := &blockingProgram{quit: make(chan struct{})}
	signals := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- runProgram(program, signals) }()
	signals <- syscall.SIGTERM
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("program did not quit after termination signal")
	}
}

func TestStopWithTimeoutProvidesBoundedContext(t *testing.T) {
	var hadDeadline bool
	err := stopWithTimeout(50*time.Millisecond, func(ctx context.Context) error {
		_, hadDeadline = ctx.Deadline()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hadDeadline {
		t.Fatal("stop context had no deadline")
	}
}
