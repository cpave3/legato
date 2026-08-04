package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const applicationShutdownTimeout = 10 * time.Second

type teaProgram interface {
	Run() (tea.Model, error)
	Quit()
}

func runProgram(program teaProgram, signals <-chan os.Signal) error {
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
		return err
	case <-signals:
		program.Quit()
		return <-done
	}
}

func stopWithTimeout(timeout time.Duration, stop func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return stop(ctx)
}
