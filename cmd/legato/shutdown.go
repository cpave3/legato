package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	applicationShutdownTimeout = 10 * time.Second
	tuiShutdownTimeout         = 2 * time.Second
)

type teaProgram interface {
	Run() (tea.Model, error)
	Quit()
	Kill()
}

func runProgram(program teaProgram, signals <-chan os.Signal) error {
	return runProgramWithTimeout(program, signals, tuiShutdownTimeout)
}

func runProgramWithTimeout(program teaProgram, signals <-chan os.Signal, timeout time.Duration) error {
	var ownedSignals chan os.Signal
	if signals == nil {
		ownedSignals = make(chan os.Signal, 1)
		signal.Notify(ownedSignals, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(ownedSignals)
		signals = ownedSignals
	}
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	select {
	case err := <-done:
		return normalizeProgramShutdownError(err)
	case <-signals:
		program.Quit()
		select {
		case err := <-done:
			return normalizeProgramShutdownError(err)
		case <-time.After(timeout):
			program.Kill()
		}
		select {
		case err := <-done:
			return normalizeProgramShutdownError(err)
		case <-time.After(timeout):
			return context.DeadlineExceeded
		}
	}
}

func normalizeProgramShutdownError(err error) error {
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

func stopWithTimeout(timeout time.Duration, stop func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return stop(ctx)
}
