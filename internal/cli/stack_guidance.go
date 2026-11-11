package cli

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/service"
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

// stackGuidanceFor tells an agent how to build and push a stack of
// branches with Staccato, its branches named after its own task or, for a
// handed-off task, after the main's (see GuidanceFor). Guidance adds it
// when StackSupport says st can.
func stackGuidanceFor(branches service.SessionBranches) string {
	if branches.Base == "" {
		return stackGuidance
	}
	text := strings.Replace(stackGuidance, "legato/$LEGATO_TASK_ID-<a few words>", branches.Base+"-<a few words>", 1)
	if branches.Granted {
		onTop := branches.Base
		if branches.Given != "" {
			onTop = branches.Given
		}
		text = strings.Replace(text, "with no further /.", "with no further /, on top of "+onTop+". Pushing them is already allowed.", 1)
		// st pr make opens pull requests that are not drafts.
		text = strings.Replace(text, "Open the stack's pull requests with st pr.", "Open each branch's pull request as a draft, from the bottom of the stack up, with gh pr create --draft --head <branch> --base <the branch below it>: st pr make cannot open a draft.", 1)
	}
	return text
}

// stackGuidance tells an ordinary session how to build and push a stack of
// branches with Staccato.
const stackGuidance = `

Stacks of branches with Staccato (st): when a change is easier to review as several small pull requests, build it as a stack with st (st help explains how). Name every branch legato/$LEGATO_TASK_ID-<a few words>, with no further /. Push the stack with st sync, not git push: it restacks and pushes each branch. st sync exits non-zero when a branch was not pushed, and st sync --json lists each in failed_pushes with git's reason. A reason with pending ap-… means the operator is being asked: carry on with other work or end your turn, and run st sync again after the [legato approvals] message. Open the stack's pull requests with st pr. Legato records the stack when you finish, so the operator can carry it on in their own checkout.`
