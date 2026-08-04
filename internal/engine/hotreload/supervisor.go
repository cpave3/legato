//go:build unix

package hotreload

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type Options struct {
	Path            string
	Args            []string
	Env             []string
	Dir             string
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	PollInterval    time.Duration
	StablePolls     int
	ShutdownTimeout time.Duration
	StartupWindow   time.Duration
	Signals         <-chan os.Signal
}

type identity struct {
	Device  uint64
	Inode   uint64
	Size    int64
	ModTime int64
}

type childResult struct {
	err error
}

type child struct {
	cmd  *exec.Cmd
	done chan childResult
}

func Run(options Options) int {
	defaults(&options)
	if options.Path == "" {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintf(options.Stderr, "hot reload: resolve executable: %v\n", err)
			return 1
		}
		options.Path = executable
	}
	resolved, err := filepath.EvalSymlinks(options.Path)
	if err != nil {
		fmt.Fprintf(options.Stderr, "hot reload: resolve executable symlinks: %v\n", err)
		return 1
	}
	options.Path = resolved
	current, err := fileIdentity(options.Path)
	if err != nil {
		fmt.Fprintf(options.Stderr, "hot reload: inspect executable: %v\n", err)
		return 1
	}
	active, err := startChild(options)
	if err != nil {
		fmt.Fprintf(options.Stderr, "hot reload: start child: %v\n", err)
		return 1
	}

	signals := options.Signals
	var ownedSignals chan os.Signal
	if signals == nil {
		ownedSignals = make(chan os.Signal, 2)
		signal.Notify(ownedSignals, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(ownedSignals)
		signals = ownedSignals
	}
	ticker := time.NewTicker(options.PollInterval)
	defer ticker.Stop()

	var candidate identity
	stable := 0
	for {
		var activeDone <-chan childResult
		if active != nil {
			activeDone = active.done
		}
		select {
		case sig := <-signals:
			if active == nil {
				return 0
			}
			if sig != nil {
				_ = active.cmd.Process.Signal(sig)
			}
			return exitCode((<-active.done).err)
		case result := <-activeDone:
			return exitCode(result.err)
		case <-ticker.C:
			next, statErr := fileIdentity(options.Path)
			if statErr != nil {
				fmt.Fprintf(options.Stderr, "hot reload: executable unavailable: %v\n", statErr)
				stable = 0
				continue
			}
			if next == current {
				stable = 0
				continue
			}
			if next != candidate {
				candidate, stable = next, 1
				continue
			}
			stable++
			if stable < options.StablePolls || !openable(options.Path) {
				continue
			}

			if active != nil && !stopForReload(active, options.ShutdownTimeout) {
				fmt.Fprintln(options.Stderr, "hot reload: child did not stop before the graceful shutdown timeout; keeping the current child")
				current, stable = candidate, 0
				continue
			}
			replacement, startErr := startChild(options)
			if startErr != nil {
				fmt.Fprintf(options.Stderr, "hot reload: start replacement: %v\n", startErr)
				active = nil
				current, stable = candidate, 0
				continue
			}
			active = replacement
			current, stable = candidate, 0
			if result, exited := observeStartup(active, options.StartupWindow); exited {
				if result.err != nil {
					fmt.Fprintf(options.Stderr, "hot reload: replacement exited during startup with status %d\n", exitCode(result.err))
				}
				active = nil
			}
		}
	}
}

func defaults(options *Options) {
	if options.Env == nil {
		options.Env = os.Environ()
	}
	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 500 * time.Millisecond
	}
	if options.StablePolls < 2 {
		options.StablePolls = 2
	}
	if options.ShutdownTimeout <= 0 {
		options.ShutdownTimeout = 10 * time.Second
	}
	if options.StartupWindow <= 0 {
		options.StartupWindow = time.Second
	}
}

func startChild(options Options) (*child, error) {
	cmd := exec.Command(options.Path, options.Args...)
	cmd.Env, cmd.Dir = options.Env, options.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = options.Stdin, options.Stdout, options.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &child{cmd: cmd, done: make(chan childResult, 1)}
	go func() { c.done <- childResult{err: cmd.Wait()} }()
	return c, nil
}

func observeStartup(active *child, window time.Duration) (childResult, bool) {
	select {
	case result := <-active.done:
		return result, true
	case <-time.After(window):
		return childResult{}, false
	}
}

func stopForReload(active *child, timeout time.Duration) bool {
	if err := active.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return false
	}
	select {
	case <-active.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func fileIdentity(path string) (identity, error) {
	info, err := os.Stat(path)
	if err != nil {
		return identity{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return identity{}, fmt.Errorf("unsupported file metadata")
	}
	return identity{Device: uint64(stat.Dev), Inode: stat.Ino, Size: info.Size(), ModTime: info.ModTime().UnixNano()}, nil
}

func openable(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	return file.Close() == nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}
