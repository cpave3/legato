package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The operator steers a review loop by its ID from the host, outside any
// session (LEG-583, story 24): `legato review-pass another-pass --loop` and
// `message --loop`, the actions a stuck or waiting loop offers on the run
// page and in Attention. They are refused inside a session.

// reviewLoopAgents is the agents `review-pass message` tells; tests replace
// it.
var reviewLoopAgents = func(cfg *config.Config, db *store.Store) (service.ReviewLoopMessenger, error) {
	agents, _, err := commandAgentService(cfg, db, "", nil, events.New(), nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("messaging an agent requires tmux: %w", err)
	}
	messenger, ok := agents.(service.ReviewLoopMessenger)
	if !ok {
		return nil, errors.New("this agent service cannot queue messages")
	}
	return messenger, nil
}

// reviewLoopSteerer is how the steering commands reach a loop: today's
// task-keyed engine actions on the loop's host task (service.ReviewLoopSteering).
// The engine's operator actions by loop ID (LEG-582) can take its place.
type reviewLoopSteerer interface {
	AnotherPass(ctx context.Context, loopID string) error
	MessageAuthor(ctx context.Context, loopID, text string) error
}

// newReviewLoopSteerer builds the steering for this command's store. The
// engine it holds never ticks: the Legato that runs the engine picks up the
// change on its next tick. Agents are only made for a message.
func newReviewLoopSteerer(cfg *config.Config, db *store.Store, message bool) (reviewLoopSteerer, error) {
	steering := service.ReviewLoopSteering{DB: db, Engine: service.NewReviewPassService(db, service.ReviewPassOptions{Config: reviewPassConfig(cfg)})}
	if message {
		agents, err := reviewLoopAgents(cfg, db)
		if err != nil {
			return nil, err
		}
		steering.Agents = agents
	}
	return steering, nil
}

// reviewLoopOperator is the loop the operator names, or why the command is
// refused: inside a session, and without --loop.
func reviewLoopOperator(verb string, parsed parsedArgs) (string, *commandError) {
	if reviewPassInSession() {
		return "", loopInSession(verb)
	}
	loop := strings.TrimSpace(parsed.Values["loop"])
	if loop == "" {
		return "", usageError("missing_argument", "review-pass "+verb+" needs --loop <loop-id>")
	}
	return loop, nil
}

// runReviewLoopAnotherPassContract is Another pass on the loop named.
func runReviewLoopAnotherPassContract(args []string) int {
	const command = "review-pass.another-pass"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("review-pass", "another-pass"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	loop, refusal := reviewLoopOperator("another-pass", parsed)
	if refusal != nil {
		return renderCommandError(command, refusal, jsonMode)
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	steer, err := newReviewLoopSteerer(cfg, db, false)
	if err == nil {
		err = steer.AnotherPass(context.Background(), loop)
	}
	if err != nil {
		return renderCommandError(command, reviewLoopError(err, loop), jsonMode)
	}
	reviewLoopChanged(db, loop)
	return writeCommandSuccess(command, map[string]any{"loop_id": loop, "action": "another_pass"},
		fmt.Sprintf("Another pass of %s: the review engine starts it on its next tick.", loop), jsonMode)
}

// runReviewLoopMessageContract is Message the author of the loop named.
func runReviewLoopMessageContract(args []string) int {
	const command = "review-pass.message"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("review-pass", "message"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	loop, refusal := reviewLoopOperator("message", parsed)
	if refusal != nil {
		return renderCommandError(command, refusal, jsonMode)
	}
	text, inputErr := readNamedTextInput("message", "--text", "--file", parsed.Values["text"], parsed.Present["text"], parsed.Values["file"], parsed.Present["file"], os.Stdin)
	if inputErr != nil {
		return renderCommandError(command, inputErr, jsonMode)
	}
	if strings.TrimSpace(text) == "" {
		return renderCommandError(command, usageError("missing_argument", "review-pass message needs its text (--text, or --file -)"), jsonMode)
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	steer, err := newReviewLoopSteerer(cfg, db, true)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "dependency_unavailable", Message: err.Error(), Exit: exitDependency}, jsonMode)
	}
	if err := steer.MessageAuthor(context.Background(), loop, text); err != nil {
		return renderCommandError(command, reviewLoopError(err, loop), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"loop_id": loop, "action": "message"},
		fmt.Sprintf("Sent to the author of %s.", loop), jsonMode)
}

// reviewLoopChanged tells running Legatos a run's loop changed, so its run
// page refreshes before the engine's next tick.
func reviewLoopChanged(db *store.Store, loopID string) {
	l, err := db.GetReviewLoop(context.Background(), loopID)
	if err != nil || l.RunID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ipc.BroadcastContext(ctx, ipc.Message{Type: "workflow_changed", WorkflowRunID: l.RunID, WorkflowStepID: l.StepPath, PID: os.Getpid()})
}
