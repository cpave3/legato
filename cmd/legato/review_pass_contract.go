package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The review-pass commands act for whoever runs them (B4, LEG-571). Inside an
// agent session that is the session: its loop is found from the session the
// broker token names, and only the author task's main slot may answer. --task
// is refused there, and so is --loop. Outside any session it is the operator
// on the host, who names a loop by its ID with --loop (LEG-583), or a task
// with --task as before.

// reviewPassInSession reports whether the command runs for an agent session:
// it has a broker, or a session's identity (the host sets both variables
// when it runs a forwarded command for a session, from the broker token).
func reviewPassInSession() bool {
	if _, _, ok := brokerSession(); ok {
		return true
	}
	return os.Getenv("LEGATO_TASK_ID") != "" || os.Getenv("LEGATO_EXECUTION_ID") != ""
}

// reviewPassAnswerer is who the command acts for, or why it is refused.
func reviewPassAnswerer(verb string, parsed parsedArgs) (service.ReviewAnswerer, *commandError) {
	if reviewPassInSession() {
		if parsed.Present["loop"] {
			return service.ReviewAnswerer{}, loopInSession(verb)
		}
		if parsed.Present["task"] {
			return service.ReviewAnswerer{}, &commandError{Code: "task_in_session", Exit: exitForbidden,
				Message: "--task is refused inside an agent session: review-pass " + verb + " finds the review loop your own task authors, so give no task"}
		}
		return service.ReviewAnswerer{TaskID: strings.TrimSpace(os.Getenv("LEGATO_TASK_ID")), ExecutionID: strings.TrimSpace(os.Getenv("LEGATO_EXECUTION_ID"))}, nil
	}
	loop, task := strings.TrimSpace(parsed.Values["loop"]), strings.TrimSpace(parsed.Values["task"])
	switch {
	case loop != "" && task != "":
		return service.ReviewAnswerer{}, usageError("conflicting_input", "give --loop or --task, not both")
	case loop != "":
		return service.ReviewAnswerer{LoopID: loop, Operator: true}, nil
	case task == "":
		return service.ReviewAnswerer{}, usageError("missing_argument", "outside an agent session, review-pass "+verb+" needs --loop <loop-id> (or --task)")
	}
	return service.ReviewAnswerer{TaskID: task, Operator: true}, nil
}

// loopInSession refuses --loop to an agent session: a loop by its ID is the
// operator's, on the host (B4).
func loopInSession(verb string) *commandError {
	return &commandError{Code: "loop_in_session", Exit: exitForbidden,
		Message: "--loop is refused inside an agent session: it is the operator's, on the host; review-pass " + verb + " in a session acts only on the review loop your own task authors"}
}

// reviewPassHostAnswer is what asking the host's broker came to.
type reviewPassHostAnswer int

const (
	// reviewPassAskedNobody: no broker, or none answered outside the
	// sandbox. The command opens the store, with the session's identity.
	reviewPassAskedNobody reviewPassHostAnswer = iota
	// reviewPassHostAnswered: the host decided, with the result or an error.
	reviewPassHostAnswered
	// reviewPassHostTooOld: a sandboxed session's host has no review_pass
	// operations; the command is forwarded whole, as before.
	reviewPassHostTooOld
)

// reviewPassFromHost asks the host's broker to run op for this session.
func reviewPassFromHost(op string, args, out any) (reviewPassHostAnswer, error) {
	socket, token, ok := brokerSession()
	if !ok {
		return reviewPassAskedNobody, nil
	}
	err := broker.Call(fallbackCtx(context.Background()), socket, token, op, args, out)
	var refused *broker.Error
	switch {
	case err == nil:
		return reviewPassHostAnswered, nil
	case errors.As(err, &refused) && refused.Code == "unknown_op":
		if sandboxed() {
			return reviewPassHostTooOld, nil
		}
		return reviewPassAskedNobody, nil
	case errors.Is(err, broker.ErrUnreachable) && !sandboxed():
		return reviewPassAskedNobody, nil
	}
	return reviewPassHostAnswered, err
}

// reviewPassBrokerError is a host's refusal in the command's terms.
func reviewPassBrokerError(err error) *commandError {
	var refused *broker.Error
	if errors.As(err, &refused) {
		switch refused.Code {
		case "forbidden", "unauthorized":
			return &commandError{Code: refused.Code, Message: refused.Message, Exit: exitForbidden}
		case "rejected":
			return commandDomainError(errors.New(refused.Message))
		}
	}
	if errors.Is(err, broker.ErrUnreachable) {
		return &commandError{Code: "broker_unreachable", Message: err.Error(), Exit: exitEnvironment}
	}
	return commandDomainError(err)
}

// reviewPassError is a store-side error in the command's terms.
func reviewPassError(err error) *commandError {
	switch {
	case errors.Is(err, service.ErrReviewAnswerRefused):
		return &commandError{Code: "forbidden", Message: err.Error(), Exit: exitForbidden}
	case errors.Is(err, store.ErrReviewLoopClosed):
		return &commandError{Code: "loop_closed", Message: err.Error(), Exit: exitNotFound}
	case errors.Is(err, service.ErrReviewNotStuck):
		return &commandError{Code: "loop_not_stuck", Message: err.Error(), Exit: exitInternal}
	}
	return commandDomainError(err)
}

// reviewLoopError is reviewPassError for a command that named a loop by
// its ID: an unknown ID is the loop's, not a task's.
func reviewLoopError(err error, loopID string) *commandError {
	if loopID != "" && errors.Is(err, store.ErrNotFound) {
		return &commandError{Code: "loop_not_found", Message: err.Error(), Exit: exitNotFound}
	}
	return reviewPassError(err)
}

// forwardReviewPass runs the command whole on a sandboxed session's host
// that has no review_pass operations; the host runs it as the session.
func forwardReviewPass(args []string, stdin []byte) int {
	out, err := runOnHost(args, stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "legato: %v\n", err)
		return exitEnvironment
	}
	_, _ = os.Stdout.Write(out.Stdout)
	_, _ = os.Stderr.Write(out.Stderr)
	return out.Exit
}

// runReviewPassFeedbackContract prints the current round of the feedback
// on the loop the caller's task authors. Its text is data: it is printed as
// JSON whatever the flags, so text from GitHub never reads as the author's
// own instructions.
func runReviewPassFeedbackContract(args []string) int {
	const command = "review-pass.feedback"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("review-pass", "feedback"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	who, refusal := reviewPassAnswerer("feedback", parsed)
	if refusal != nil {
		return renderCommandError(command, refusal, jsonMode)
	}
	var view service.ReviewFeedbackView
	asked, err := reviewPassFromHost("review_pass.feedback", map[string]any{}, &view)
	switch {
	case asked == reviewPassHostTooOld:
		return forwardReviewPass(append([]string{"review-pass", "feedback"}, args...), nil)
	case asked == reviewPassHostAnswered && err != nil:
		return renderCommandError(command, reviewPassBrokerError(err), jsonMode)
	case asked == reviewPassAskedNobody:
		cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
		if db == nil {
			return code
		}
		defer db.Close()
		// The guidance for a stack names the skill that works through it,
		// when the author's agent has it.
		view, err = service.OwnReviewFeedback(context.Background(), db, who, service.ReviewFeedbackOptions{HasSkill: agentHasSkill(cfg, db)})
		if err != nil {
			return renderCommandError(command, reviewLoopError(err, who.LoopID), jsonMode)
		}
	}
	text, _ := json.MarshalIndent(view, "", "  ")
	return writeCommandSuccess(command, view, string(text), jsonMode)
}

// runReviewPassAnswerContract records the author's answer to one item.
func runReviewPassAnswerContract(args []string) int {
	const command = "review-pass.answer"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("review-pass", "answer"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 1 {
		return renderCommandError(command, usageError("missing_argument", "review-pass answer takes one item ID"), jsonMode)
	}
	who, refusal := reviewPassAnswerer("answer", parsed)
	if refusal != nil {
		return renderCommandError(command, refusal, jsonMode)
	}
	var answers []string
	for _, a := range []string{service.AnswerFixed, service.AnswerReplied, service.AnswerSkipped} {
		if parsed.Present[a] {
			answers = append(answers, a)
		}
	}
	if len(answers) != 1 {
		return renderCommandError(command, usageError("missing_argument", "give one of --fixed, --replied or --skipped"), jsonMode)
	}
	note, inputErr := readNamedTextInput("note", "--note", "--file", parsed.Values["note"], parsed.Present["note"], parsed.Values["file"], parsed.Present["file"], os.Stdin)
	if inputErr != nil {
		return renderCommandError(command, inputErr, jsonMode)
	}
	id := parsed.Positionals[0]
	var result cli.ReviewPassAnswerResult
	asked, err := reviewPassFromHost("review_pass.answer", map[string]string{"item": id, "answer": answers[0], "note": note}, &result)
	switch {
	case asked == reviewPassHostTooOld:
		// The text was read here: the host gets it on stdin.
		forwarded := []string{"review-pass", "answer", id, "--" + answers[0]}
		if note != "" {
			forwarded = append(forwarded, "--file", "-")
		}
		return forwardReviewPass(forwarded, []byte(note))
	case asked == reviewPassHostAnswered && err != nil:
		return renderCommandError(command, reviewPassBrokerError(err), jsonMode)
	case asked == reviewPassAskedNobody:
		db, code := openCommandStore(command, jsonMode)
		if db == nil {
			return code
		}
		defer db.Close()
		loopTask, err := service.AnswerOwnReviewItem(context.Background(), db, who, id, answers[0], note, time.Now())
		if err != nil {
			return renderCommandError(command, reviewLoopError(err, who.LoopID), jsonMode)
		}
		result = cli.ReviewPassAnswerResult{TaskID: loopTask, Item: id, Answer: answers[0]}
	}
	return writeCommandSuccess(command, map[string]any{"task_id": result.TaskID, "item": result.Item, "answer": result.Answer},
		fmt.Sprintf("Answered %s (%s). Legato posts it to GitHub.", result.Item, result.Answer), jsonMode)
}
