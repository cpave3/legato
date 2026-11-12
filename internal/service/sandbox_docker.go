package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/dockerproxy"
	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
)

// DockerCleaner removes what executions made from the Docker daemon.
// dockerproxy.Cleaner is one.
type DockerCleaner interface {
	Clean(ctx context.Context, execution string) error
	// Executions lists the executions that have anything on the daemon.
	Executions(ctx context.Context) ([]string, error)
}

// SandboxDocker is agents.sandbox.docker: each sandboxed execution gets a
// Docker API socket of its own, in its runtime folder, served by the one
// process of this Legato that holds the proxy's lock (cmd/legato), through
// a dockerproxy.Proxy that keeps it within what its sandbox shows. Agents
// outside the sandbox are not touched.
type SandboxDocker struct {
	// RuntimeDir is <runtime>/legato; each execution's socket and view are
	// in its sandbox folder there.
	RuntimeDir string
	// Cleaner removes an execution's containers, networks and volumes when
	// it ends.
	Cleaner DockerCleaner

	mu        sync.Mutex
	lastSweep string // the sweep's last failure, logged once
}

func (d *SandboxDocker) executionDir(executionID string) string {
	return filepath.Join(d.RuntimeDir, "sandbox", executionID)
}

// SocketPath is the socket an execution's agent reaches Docker through.
// Its folder is what the sandbox mounts.
func (d *SandboxDocker) SocketPath(executionID string) string {
	return filepath.Join(d.executionDir(executionID), "docker", "docker.sock")
}

// viewFile is what the execution's sandbox shows, for the proxy. The
// sandbox itself does not see it.
func (d *SandboxDocker) viewFile(executionID string) string {
	return filepath.Join(d.executionDir(executionID), "docker-view.json")
}

// HasProxy says whether an execution was started with a Docker socket.
func (d *SandboxDocker) HasProxy(executionID string) bool {
	_, err := os.Stat(d.viewFile(executionID))
	return err == nil
}

// View reads what an execution's sandbox shows.
func (d *SandboxDocker) View(executionID string) (dockerproxy.View, error) {
	var view dockerproxy.View
	data, err := os.ReadFile(d.viewFile(executionID))
	if err != nil {
		return view, err
	}
	err = json.Unmarshal(data, &view)
	return view, err
}

// WriteView records what a sandbox about to start shows, and makes its
// socket's folder.
func (d *SandboxDocker) WriteView(executionID string, plan sandbox.Plan) error {
	if err := os.MkdirAll(filepath.Dir(d.SocketPath(executionID)), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(dockerViewOf(plan))
	if err != nil {
		return err
	}
	return os.WriteFile(d.viewFile(executionID), data, 0o600)
}

// dockerViewOf is what a plan shows, in the order bubblewrap mounts it: the
// home first, then each mount. A throwaway layer's host path is read-only
// to a container, which would otherwise write the host's copy. An optional
// bind whose path is missing is not mounted, so it is left out.
func dockerViewOf(plan sandbox.Plan) dockerproxy.View {
	var view dockerproxy.View
	if plan.Home != "" && plan.HomeSource != "" {
		view.Mounts = append(view.Mounts, dockerproxy.Mount{Kind: dockerproxy.ReadWrite, Source: plan.HomeSource, Dest: plan.Home})
	}
	for _, m := range plan.Mounts {
		dest := m.Dest
		if dest == "" {
			dest = m.Path
		}
		if m.Optional {
			if _, err := os.Stat(m.Path); err != nil {
				continue
			}
		}
		switch m.Kind {
		case sandbox.ReadOnly, sandbox.Overlay:
			view.Mounts = append(view.Mounts, dockerproxy.Mount{Kind: dockerproxy.ReadOnly, Source: m.Path, Dest: dest})
		case sandbox.ReadWrite:
			view.Mounts = append(view.Mounts, dockerproxy.Mount{Kind: dockerproxy.ReadWrite, Source: m.Path, Dest: dest})
		case sandbox.Symlink:
			view.Mounts = append(view.Mounts, dockerproxy.Mount{Kind: dockerproxy.Symlink, Dest: dest, Target: m.Target})
		case sandbox.Hide:
			view.Mounts = append(view.Mounts, dockerproxy.Mount{Kind: dockerproxy.Hide, Dest: dest})
		case sandbox.HideDir:
			view.Mounts = append(view.Mounts, dockerproxy.Mount{Kind: dockerproxy.HideDir, Dest: dest})
		}
	}
	return view
}

// cleanTimeout bounds removing one execution's containers.
const cleanTimeout = 2 * time.Minute

// cleanLogged removes what an ended execution made, in the background: an
// agent's end does not wait for its containers to stop. A failure is
// logged, and the sweep tries again.
func (d *SandboxDocker) cleanLogged(executionID string) {
	if d == nil || d.Cleaner == nil || executionID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanTimeout)
		defer cancel()
		if err := d.Cleaner.Clean(ctx, executionID); err != nil {
			log.Printf("docker: removing what %s made: %v", executionID, err)
		}
	}()
}

// Sweep removes what ended executions made: those whose end Legato did not
// see, or whose removal failed. What a running execution made, or one this
// Legato does not know (another Legato's), is left alone. It returns how
// many executions it cleaned.
func (d *SandboxDocker) Sweep(ctx context.Context, statusOf ExecutionStatus) (int, error) {
	if d == nil || d.Cleaner == nil {
		return 0, nil
	}
	ids, err := d.Cleaner.Executions(ctx)
	if err != nil {
		return 0, err
	}
	cleaned := 0
	var errs []error
	for _, id := range ids {
		status, found, err := statusOf(ctx, id)
		if err != nil {
			return cleaned, err
		}
		if !found || status == store.ExecutionRunning {
			continue
		}
		if err := d.Cleaner.Clean(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		cleaned++
	}
	return cleaned, errors.Join(errs...)
}

// sweepLogged sweeps, logging a failure only when it differs from the last
// one: a daemon that is not running fails every sweep the same way.
func (d *SandboxDocker) sweepLogged(ctx context.Context, statusOf ExecutionStatus) {
	if d == nil || d.Cleaner == nil {
		return
	}
	cleaned, err := d.Sweep(ctx, statusOf)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	d.mu.Lock()
	changed := msg != d.lastSweep
	d.lastSweep = msg
	d.mu.Unlock()
	if err != nil && changed {
		log.Printf("docker: sweeping what ended executions made: %v", err)
	}
	if cleaned > 0 {
		log.Printf("docker: removed what %d ended execution(s) made", cleaned)
	}
}
