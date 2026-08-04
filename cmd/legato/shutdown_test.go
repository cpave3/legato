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
	quit       chan struct{}
	ignoreQuit bool
	killCalls  chan struct{}
	runError   error
}

func (p *blockingProgram) Run() (tea.Model, error) {
	<-p.quit
	return nil, p.runError
}

func (p *blockingProgram) Quit() {
	if p.ignoreQuit {
		return
	}
	p.stop()
}

func (p *blockingProgram) Kill() {
	if p.killCalls != nil {
		select {
		case p.killCalls <- struct{}{}:
		default:
		}
	}
	p.stop()
}

func (p *blockingProgram) stop() {
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

func TestRunProgramKillsAfterGracefulQuitTimeout(t *testing.T) {
	program := &blockingProgram{quit: make(chan struct{}), ignoreQuit: true, killCalls: make(chan struct{}, 1), runError: tea.ErrProgramKilled}
	signals := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- runProgramWithTimeout(program, signals, 20*time.Millisecond) }()
	signals <- syscall.SIGTERM

	select {
	case <-program.killCalls:
	case <-time.After(time.Second):
		t.Fatal("program was not killed after graceful quit timeout")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runProgram remained blocked after kill")
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
