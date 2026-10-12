package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func selectedReviewService(command string, parsed parsedArgs, jsonMode bool) (*service.ReviewService, string, func(), int) {
	taskID := parsed.Values["task"]
	if taskID == "" {
		taskID = os.Getenv("LEGATO_TASK_ID")
	}
	if taskID == "" {
		return nil, "", func() {}, renderCommandError(command, &commandError{Code: "agent_context_missing", Message: "no task — pass --task <id> or set LEGATO_TASK_ID", Exit: exitEnvironment}, jsonMode)
	}
	name := parsed.Values["name"]
	if name == "" {
		name = os.Getenv("LEGATO_REVIEW_NAME")
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return nil, "", func() {}, code
	}
	svc := newReviewService(cfg, db, nil, nil)
	tour, err := svc.EnsureReviewTour(ctx(), taskID, name)
	if err != nil {
		db.Close()
		return nil, "", func() {}, renderCommandError(command, reviewCommandError(err), jsonMode)
	}
	return svc, tour.ID, func() { _ = db.Close() }, exitOK
}

func ctx() context.Context { return context.Background() }

func reviewCommandError(err error) *commandError {
	if errors.Is(err, store.ErrNotFound) {
		return &commandError{Code: "review_metadata_not_found", Message: "review metadata ID was not found in the selected review", Exit: exitNotFound}
	}
	message := err.Error()
	if strings.Contains(message, "ambiguous") {
		return &commandError{Code: "ambiguous_id", Message: message, Exit: exitUsage}
	}
	for _, marker := range []string{"required", "must", "cannot select", "not in the", "no fields"} {
		if strings.Contains(message, marker) {
			return &commandError{Code: "invalid_review_metadata", Message: message, Exit: exitUsage}
		}
	}
	return &commandError{Code: "internal_error", Message: message, Exit: exitInternal}
}

func parseOrder(value string) (*int, *commandError) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return nil, usageError("invalid_order", fmt.Sprintf("--order must be a number, got %q", value))
	}
	return &n, nil
}

func validRisk(value string) bool {
	return value == "" || value == "low" || value == "medium" || value == "high" || value == "unsure"
}

func parseLines(value string) (*int, *int, *commandError) {
	a, b, ok := strings.Cut(value, "-")
	start, e1 := strconv.Atoi(a)
	end, e2 := strconv.Atoi(b)
	if !ok || e1 != nil || e2 != nil || start < 1 || end < start {
		return nil, nil, usageError("invalid_lines", fmt.Sprintf("--lines must be a 1-based range such as 4-9, got %q", value))
	}
	return &start, &end, nil
}

func runReviewChapterEditContract(args []string) int {
	const command = "review.chapter.edit"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("review", "chapter", "edit"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) < 1 || len(parsed.Positionals) > 3 {
		return renderCommandError(command, usageError("missing_argument", "chapter edit requires a chapter ID and optional compatibility title/narration"), jsonMode)
	}
	a := service.ChapterEditArgs{}
	if len(parsed.Positionals) >= 2 {
		a.Title = stringPointer(parsed.Positionals[1])
	}
	if len(parsed.Positionals) == 3 {
		a.Narration = stringPointer(parsed.Positionals[2])
	}
	if parsed.Present["title"] {
		if a.Title != nil {
			return renderCommandError(command, usageError("conflicting_input", "provide title positionally or with --title, not both"), jsonMode)
		}
		a.Title = stringPointer(parsed.Values["title"])
	}
	if parsed.Present["narration"] {
		if a.Narration != nil || parsed.Present["clear-narration"] {
			return renderCommandError(command, usageError("conflicting_input", "--narration conflicts with positional narration or --clear-narration"), jsonMode)
		}
		a.Narration = stringPointer(parsed.Values["narration"])
	}
	if parsed.Present["narration-file"] {
		if a.Narration != nil || parsed.Present["clear-narration"] {
			return renderCommandError(command, usageError("conflicting_input", "--narration-file conflicts with other narration input and with --clear-narration"), jsonMode)
		}
		narration, inputErr := readNamedTextInput("narration", "", "--narration-file", "", false, parsed.Values["narration-file"], true, os.Stdin)
		if inputErr != nil {
			return renderCommandError(command, inputErr, jsonMode)
		}
		a.Narration, a.NarrationDir = stringPointer(narration), markdownInputDir(parsed.Values["narration-file"])
	}
	if parsed.Present["clear-narration"] {
		a.Narration = stringPointer("")
	}
	if parsed.Present["risk"] {
		if parsed.Present["clear-risk"] || !validRisk(parsed.Values["risk"]) {
			return renderCommandError(command, usageError("invalid_risk", "--risk must be low, medium, high, or unsure and cannot be combined with --clear-risk"), jsonMode)
		}
		a.Risk = stringPointer(parsed.Values["risk"])
	}
	if parsed.Present["clear-risk"] {
		a.Risk = stringPointer("")
	}
	if parsed.Present["order"] {
		if parsed.Present["clear-order"] {
			return renderCommandError(command, usageError("conflicting_input", "--order conflicts with --clear-order"), jsonMode)
		}
		order, err := parseOrder(parsed.Values["order"])
		if err != nil {
			return renderCommandError(command, err, jsonMode)
		}
		a.OrderHint = order
	}
	a.ClearOrderHint = parsed.Present["clear-order"]
	if parsed.Present["include"] {
		include, err := parseChapterIncludes(parsed.Values["include"])
		if err != nil {
			return renderCommandError(command, err, jsonMode)
		}
		a.Includes, a.ReplaceIncludes = include, true
	}
	if a.Title == nil && a.Narration == nil && a.Risk == nil && a.OrderHint == nil && !a.ClearOrderHint && !a.ReplaceIncludes {
		return renderCommandError(command, usageError("no_changes", "chapter edit requires at least one field to change"), jsonMode)
	}
	svc, tourID, closeStore, code := selectedReviewService(command, parsed, jsonMode)
	if svc == nil {
		return code
	}
	defer closeStore()
	id, err := cli.ReviewChapterEdit(svc, tourID, parsed.Positionals[0], a)
	if err != nil {
		return renderCommandError(command, reviewCommandError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"chapter_id": id}, "", jsonMode)
}

func parseChapterIncludes(value string) ([]service.ChapterInclude, *commandError) {
	parts := strings.Split(value, "\x00")
	out := make([]service.ChapterInclude, 0, len(parts))
	for _, item := range parts {
		i := strings.LastIndex(item, ":")
		if i < 1 {
			return nil, usageError("invalid_include", fmt.Sprintf("--include %q must use <path>:<hunk>", item))
		}
		h, err := strconv.Atoi(item[i+1:])
		if err != nil || h < 1 {
			return nil, usageError("invalid_include", fmt.Sprintf("--include %q must end with a 1-based hunk", item))
		}
		out = append(out, service.ChapterInclude{FilePath: item[:i], Hunk: h})
	}
	return out, nil
}

func runReviewChapterRemoveContract(args []string) int {
	return runReviewRemoveContract("review.chapter.remove", args, true)
}
func runReviewAnnotationRemoveContract(args []string) int {
	return runReviewRemoveContract("review.annotation.remove", args, false)
}
func runReviewRemoveContract(command string, args []string, chapter bool) int {
	path := []string{"review", "annotation", "remove"}
	if chapter {
		path = []string{"review", "chapter", "remove"}
	}
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs(path...))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 1 {
		return renderCommandError(command, usageError("missing_argument", command+" requires exactly one ID"), jsonMode)
	}
	svc, tourID, closeStore, code := selectedReviewService(command, parsed, jsonMode)
	if svc == nil {
		return code
	}
	defer closeStore()
	id := parsed.Positionals[0]
	var err error
	if chapter {
		id, err = cli.ReviewChapterRemove(svc, tourID, id)
	} else {
		id, err = cli.ReviewAnnotationRemove(svc, tourID, id)
	}
	if err != nil {
		return renderCommandError(command, reviewCommandError(err), jsonMode)
	}
	key := "annotation_id"
	if chapter {
		key = "chapter_id"
	}
	return writeCommandSuccess(command, map[string]any{key: id}, "", jsonMode)
}

func runReviewAnnotationEditContract(args []string) int {
	const command = "review.annotation.edit"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("review", "annotation", "edit"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) < 1 || len(parsed.Positionals) > 2 {
		return renderCommandError(command, usageError("missing_argument", "annotation edit requires an annotation ID and optional compatibility text"), jsonMode)
	}
	a := service.AnnotationEditArgs{}
	if len(parsed.Positionals) == 2 {
		a.Text = stringPointer(parsed.Positionals[1])
	}
	if parsed.Present["text"] {
		if a.Text != nil {
			return renderCommandError(command, usageError("conflicting_input", "provide text positionally or with --text, not both"), jsonMode)
		}
		a.Text = stringPointer(parsed.Values["text"])
	}
	if parsed.Present["risk"] {
		if parsed.Present["clear-risk"] || !validRisk(parsed.Values["risk"]) {
			return renderCommandError(command, usageError("invalid_risk", "--risk must be low, medium, high, or unsure and cannot be combined with --clear-risk"), jsonMode)
		}
		a.Risk = stringPointer(parsed.Values["risk"])
	}
	if parsed.Present["clear-risk"] {
		a.Risk = stringPointer("")
	}
	if parsed.Present["order"] {
		if parsed.Present["clear-order"] {
			return renderCommandError(command, usageError("conflicting_input", "--order conflicts with --clear-order"), jsonMode)
		}
		order, err := parseOrder(parsed.Values["order"])
		if err != nil {
			return renderCommandError(command, err, jsonMode)
		}
		a.OrderHint = order
	}
	a.ClearOrderHint = parsed.Present["clear-order"]
	if parsed.Present["file"] {
		a.FilePath = stringPointer(parsed.Values["file"])
	}
	if parsed.Present["hunk"] {
		n, err := strconv.Atoi(parsed.Values["hunk"])
		if err != nil || n < 1 {
			return renderCommandError(command, usageError("invalid_hunk", "--hunk must be a 1-based number"), jsonMode)
		}
		a.Hunk = &n
	}
	if parsed.Present["lines"] {
		if parsed.Present["clear-lines"] {
			return renderCommandError(command, usageError("conflicting_input", "--lines conflicts with --clear-lines"), jsonMode)
		}
		start, end, err := parseLines(parsed.Values["lines"])
		if err != nil {
			return renderCommandError(command, err, jsonMode)
		}
		a.LineStart, a.LineEnd = start, end
	}
	a.ClearLines = parsed.Present["clear-lines"]
	if a.Text == nil && a.Risk == nil && a.OrderHint == nil && !a.ClearOrderHint && a.FilePath == nil && a.Hunk == nil && a.LineStart == nil && !a.ClearLines {
		return renderCommandError(command, usageError("no_changes", "annotation edit requires at least one field to change"), jsonMode)
	}
	svc, tourID, closeStore, code := selectedReviewService(command, parsed, jsonMode)
	if svc == nil {
		return code
	}
	defer closeStore()
	id, err := cli.ReviewAnnotationEdit(svc, tourID, parsed.Positionals[0], a)
	if err != nil {
		return renderCommandError(command, reviewCommandError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"annotation_id": id}, "", jsonMode)
}
