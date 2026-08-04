package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

func configuredHotReloadBuildTrigger() func(<-chan struct{}) (bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	root, ok := hotReloadCheckoutRoot(os.Getenv(hotReloadChildEnv) == "1", executable)
	if !ok {
		return nil
	}
	return newHotReloadBuildTrigger(root)
}

func hotReloadCheckoutRoot(child bool, executable string) (string, bool) {
	if !child {
		return "", false
	}
	executable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", false
	}
	root := filepath.Dir(executable)
	if info, err := os.Stat(filepath.Join(root, "Taskfile.yml")); err != nil || info.IsDir() {
		return "", false
	}
	return root, true
}

func newHotReloadBuildTrigger(root string) func(<-chan struct{}) (bool, error) {
	var mu sync.Mutex
	running := false

	return func(start <-chan struct{}) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if running {
			return false, nil
		}

		logDir := filepath.Join(root, ".legato", "run")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return false, fmt.Errorf("create build log directory: %w", err)
		}
		logFile, err := os.OpenFile(filepath.Join(logDir, "hot-reload-build.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return false, fmt.Errorf("open build log: %w", err)
		}

		taskPath, err := exec.LookPath("task")
		if err != nil {
			_ = logFile.Close()
			return false, fmt.Errorf("find task executable: %w", err)
		}
		command := exec.Command(taskPath, "build:full")
		command.Dir = root
		command.Stdout = logFile
		command.Stderr = logFile
		running = true
		go func() {
			<-start
			if err := command.Start(); err == nil {
				_ = command.Wait()
			} else {
				_, _ = fmt.Fprintf(logFile, "start full build: %v\n", err)
			}
			_ = logFile.Close()
			mu.Lock()
			running = false
			mu.Unlock()
		}()
		return true, nil
	}
}
