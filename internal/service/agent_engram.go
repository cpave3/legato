package service

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cpave3/legato/internal/engine/engram"
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
}

// endIfFresh ends the engram session a spawn that failed had booted. A
// resumed session is left as it was.
func (b *spawnedEngram) endIfFresh(ctx context.Context, engrams AgentEngrams, dir string) {
	if b == nil || b.resumed || engrams == nil {
		return
	}
	if err := engrams.EndSession(ctx, EngramSessionRef{Name: b.name, Session: b.session, Dir: dir}); err != nil {
		log.Printf("engram: ending the session of %s: %v", b.name, err)
	}
}

// bootEngram boots the engram a spawn names. nil, nil is a spawn without
// one, or one whose boot failed: the agent then starts without it and the
// caller's report says why. An error stops the spawn.
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
		return nil, nil
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
	return &spawnedEngram{EngramBoot: boot, name: opt.Engram, session: session, resumed: resume}, nil
}

// SpawnReportFromContext is the report WithSpawnReport asked for, nil when
// none was: for agent services that wrap SpawnAgent.
func SpawnReportFromContext(ctx context.Context) *SpawnReport { return spawnReportFrom(ctx) }
