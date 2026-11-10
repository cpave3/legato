package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/cpave3/legato/internal/engine/engram"
	"github.com/cpave3/legato/internal/engine/store"
)

// SpawnReport is what a spawn tells its caller past success or failure:
// the engram the agent started as, or why it started without one.
type SpawnReport struct {
	Engram        string
	EngramWarning string
}

type spawnReportKey struct{}

// WithSpawnReport asks SpawnAgent to fill report.
func WithSpawnReport(ctx context.Context, report *SpawnReport) context.Context {
	return context.WithValue(ctx, spawnReportKey{}, report)
}

func spawnReportFrom(ctx context.Context) *SpawnReport {
	report, _ := ctx.Value(spawnReportKey{}).(*SpawnReport)
	return report
}

// spawnedEngram is an engram a spawn booted.
type spawnedEngram struct {
	EngramBoot
	name    string
	session string
	resumed bool
	// failed is why the engram asked for did not boot; the agent then
	// starts without it, and nothing else here is set.
	failed string
}

// booted says whether the agent starts as the engram.
func (b *spawnedEngram) booted() bool { return b != nil && b.failed == "" }

// endIfFresh ends the engram session a spawn that failed had booted. A
// resumed session is left as it was.
func (b *spawnedEngram) endIfFresh(ctx context.Context, engrams AgentEngrams, dir string) {
	if !b.booted() || b.resumed || engrams == nil {
		return
	}
	if err := engrams.EndSession(ctx, EngramSessionRef{Name: b.name, Session: b.session, Dir: dir}); err != nil {
		log.Printf("engram: ending the session of %s: %v", b.name, err)
	}
}

// bootEngram boots the engram a spawn names. nil is a spawn without one;
// one whose boot failed says why in failed, and the agent then starts
// without it and the caller's report says why. An error stops the spawn.
func (a *agentService) bootEngram(ctx context.Context, taskID, executionID, agentKind, workDir string, opt AgentSpawnOptions) (*spawnedEngram, error) {
	if opt.Engram == "" {
		return nil, nil
	}
	if opt.Role != "" || opt.ParentTaskID != "" || opt.SubtaskID != "" {
		return nil, errors.New("swarm agents are not started as engrams")
	}
	report := spawnReportFrom(ctx)
	fail := func(err error) (*spawnedEngram, error) {
		if opt.EngramRequired && errors.Is(err, engram.ErrUnknownIdentity) {
			return nil, fmt.Errorf("engram %s: %w", opt.Engram, err)
		}
		reason := EngramFailure(opt.Engram, err)
		log.Printf("engram: %s for %s did not boot, starting without it: %s", opt.Engram, taskID, reason)
		if report != nil {
			report.EngramWarning = fmt.Sprintf("%s did not boot, so the agent started without it: %s", opt.Engram, reason)
		}
		return &spawnedEngram{name: opt.Engram, failed: reason}, nil
	}
	engrams := a.engramBooter()
	if engrams == nil {
		return fail(ErrEngramsOff)
	}
	resume := opt.SessionPolicy == "resume"
	session := executionID
	if resume {
		session = opt.EngramSession
		if session == "" {
			if last, err := a.store.LastEngramSession(ctx, taskID, opt.Engram); err == nil {
				session = last
			}
		}
		if session == "" {
			session = executionID
		}
	}
	boot, err := engrams.Boot(ctx, EngramBootRequest{
		Name: opt.Engram, Session: session, Harness: EngramHarness(agentKind), Model: opt.Model,
		TaskID: taskID, ExecutionID: executionID, Dir: workDir, Resume: resume,
	})
	if err != nil {
		return fail(err)
	}
	if report != nil {
		report.Engram = opt.Engram
	}
	// Workflows never read or write bindings.
	if opt.WorkflowRunID == "" && opt.ExecutionKind != store.ExecutionWorkflowAgent {
		if err := engrams.Touch(ctx, workDir, opt.Engram); err != nil {
			log.Printf("engram: recording the use of %s in %s: %v", opt.Engram, workDir, err)
		}
	}
	return &spawnedEngram{EngramBoot: boot, name: opt.Engram, session: session, resumed: resume}, nil
}

type engramSessionKeptKey struct{}

// withEngramSessionKept marks an agent stopped to be resumed: its engram
// session is not ended with its execution.
func withEngramSessionKept(ctx context.Context) context.Context {
	return context.WithValue(ctx, engramSessionKeptKey{}, true)
}

// EndEngramSession ends the engram session of an execution that ended for
// good outside EndExecution (a finished workflow agent).
func (a *agentService) EndEngramSession(ctx context.Context, executionID string) {
	a.endEngramSession(ctx, executionID)
}

// endEngramSession runs `engram session end` for an execution that ran as
// an engram and has ended for good. Legato asks the agent for no handoff:
// engram's practice section does.
func (a *agentService) endEngramSession(ctx context.Context, executionID string) {
	if kept, _ := ctx.Value(engramSessionKeptKey{}).(bool); kept {
		return
	}
	engrams := a.engramBooter()
	if engrams == nil {
		return
	}
	name, session, err := a.store.ExecutionEngram(ctx, executionID)
	if err != nil || name == "" {
		return
	}
	dir := ""
	if execution, err := a.store.GetExecution(ctx, executionID); err == nil {
		if info, statErr := os.Stat(execution.WorkingDir); statErr == nil && info.IsDir() {
			dir = execution.WorkingDir
		}
	}
	if err := engrams.EndSession(context.WithoutCancel(ctx), EngramSessionRef{Name: name, Session: session, Dir: dir}); err != nil {
		log.Printf("engram: ending %s's session %s: %v", name, session, err)
	}
}

// SpawnReportFromContext is the report WithSpawnReport asked for, nil when
// none was: for agent services that wrap SpawnAgent.
func SpawnReportFromContext(ctx context.Context) *SpawnReport { return spawnReportFrom(ctx) }
