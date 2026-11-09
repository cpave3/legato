package main

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/selfupdate"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/version"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// exitRestart is the exit status of `legato serve` shutting down to start
// again on an update. systemd's Restart=on-failure starts it again, and so
// does RestartForceExitStatus=75 with any Restart=
// (site/src/content/docs/running/agent-server.md).
const exitRestart = 75

// restartRequested is set once an update asked `legato serve` to exit for a
// restart, so it exits with exitRestart instead of 0.
var restartRequested atomic.Bool

// releaseRepo is where Legato's releases are published.
const releaseRepo = "cpave3/legato"

// processRestarter is how this process starts again on a binary just
// installed, and whether it can:
//   - under the hot-reload supervisor, which restarts its child whenever the
//     binary changes, there is nothing to do;
//   - as the main process of a systemd service (SYSTEMD_EXEC_PID is this
//     process; a shell in a terminal run as a service inherits another), it
//     shuts down as on SIGTERM and exits with exitRestart;
//   - otherwise it cannot, and the update waits for the person.
func processRestarter(getenv func(string) string, pid int, signalSelf func(), requested *atomic.Bool) func() bool {
	return func() bool {
		if getenv(hotReloadChildEnv) != "" {
			return true
		}
		if getenv("SYSTEMD_EXEC_PID") != strconv.Itoa(pid) {
			return false
		}
		requested.Store(true)
		// Let the answer to whoever asked for the update go out first.
		go func() {
			time.Sleep(time.Second)
			signalSelf()
		}()
		return true
	}
}

// serveRestarter restarts `legato serve`.
func serveRestarter() func() bool {
	return processRestarter(os.Getenv, os.Getpid(), func() { _ = syscall.Kill(os.Getpid(), syscall.SIGTERM) }, &restartRequested)
}

// tuiRestarter restarts the TUI only under the hot-reload supervisor: the
// person is at its terminal, and Legato does not quit from under them.
func tuiRestarter() func() bool {
	return func() bool { return os.Getenv(hotReloadChildEnv) != "" }
}

// newUpdateService is this Legato's updater: releases of releaseRepo, put
// in place of the running binary.
func newUpdateService(db *store.Store, bus *events.Bus, agents service.AgentService, restart func() bool) *service.UpdateService {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	return service.NewUpdateService(service.UpdateOptions{
		Store: service.StringSetting{Settings: settingsFor(db)}, Bus: bus, Current: version.String(), Executable: exe,
		Releases: selfupdate.Client{Repo: releaseRepo, HTTP: &http.Client{Timeout: 10 * time.Minute}},
		// st, Staccato's command, is kept current beside Legato when it is
		// a release build on Legato's PATH.
		Tools: []*service.ToolUpdater{service.NewSTUpdater(service.ToolUpdaterOptions{})},
		Busy: func(ctx context.Context) (bool, error) {
			if agents == nil {
				return false, nil
			}
			sessions, err := agents.ListAgents(ctx)
			return service.AgentsMidTurn(sessions), err
		},
		Restart:         restart,
		FirstCheckAfter: time.Minute, CheckEvery: 6 * time.Hour,
		QuietPoll: 30 * time.Second, MaxWait: 3 * time.Hour,
	})
}

// wireUpdates serves the updater and starts its checks.
func wireUpdates(srv *server.Server, updates *service.UpdateService) {
	srv.SetUpdateService(updates)
	srv.StartUpdateEvents()
	go updates.Run(context.Background())
}
