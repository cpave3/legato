package cli

import (
	"context"
	"os/exec"
	"time"
)

// stackProbeTimeout is how long StackSupport waits for st to answer.
const stackProbeTimeout = 2 * time.Second

// StackSupport reports whether the st on PATH can move a stack between
// clones (st graph import), which comes with st sync reporting the pushes it
// could not make. Agents are told how to stack only when it can: without
// it a refused push goes unnoticed, and the stack cannot follow the work.
func StackSupport(ctx context.Context) bool {
	st, err := exec.LookPath("st")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, stackProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, st, "graph", "import", "--help")
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Run() == nil
}

// stackGuidance tells an agent how to build and push a stack of branches
// with Staccato. Guidance adds it when StackSupport says st can.
const stackGuidance = `

Stacks of branches with Staccato (st): when a change is easier to review as several small pull requests, build it as a stack with st (st help explains how). Name every branch legato/$LEGATO_TASK_ID-<a few words>, with no further /. Push the stack with st sync, not git push: it restacks and pushes each branch. st sync exits non-zero when a branch was not pushed, and st sync --json lists each in failed_pushes with git's reason. A reason with pending ap-… means the person is being asked: carry on with other work or end your turn, and run st sync again after the [legato approvals] message. Open the stack's pull requests with st pr. Legato records the stack when you finish, so the person can carry it on in their own checkout.`
