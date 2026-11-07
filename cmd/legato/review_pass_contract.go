package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// reviewPassTask is the task a review-pass command acts on: --task, else
// the session's own.
func reviewPassTask(values map[string]string) string {
	if task := strings.TrimSpace(values["task"]); task != "" {
		return task
	}
	return strings.TrimSpace(os.Getenv("LEGATO_TASK_ID"))
}

// runReviewPassFeedbackContract prints the current round of a reviewed
// handoff's feedback. Its text is data: it is printed as JSON whatever the
// flags, so text from GitHub never reads as the author's own instructions.
func runReviewPassFeedbackContract(args []string) int {
	const command = "review-pass.feedback"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("review-pass", "feedback"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID := reviewPassTask(parsed.Values)
	if taskID == "" {
		return renderCommandError(command, usageError("missing_argument", "review-pass feedback needs --task or a session's LEGATO_TASK_ID"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	view, err := service.ReviewFeedback(context.Background(), db, taskID)
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
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
	taskID := reviewPassTask(parsed.Values)
	if taskID == "" {
		return renderCommandError(command, usageError("missing_argument", "review-pass answer needs --task or a session's LEGATO_TASK_ID"), jsonMode)
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
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	id := parsed.Positionals[0]
	if err := service.AnswerReviewItem(context.Background(), db, taskID, id, answers[0], note, time.Now()); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"task_id": taskID, "item": id, "answer": answers[0]},
		fmt.Sprintf("Answered %s (%s). Legato posts it to GitHub.", id, answers[0]), jsonMode)
}
