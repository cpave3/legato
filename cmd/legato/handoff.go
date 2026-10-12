package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func handoffCommands(selectors []commandFlag) *commandDef {
	flags := func(extra ...commandFlag) []commandFlag {
		return append(append([]commandFlag{}, selectors...), extra...)
	}
	return group("handoff", "Author and export revision-bound handoff context", []string{"legato review handoff show --json"},
		leaf("template", "Print an editable JSON template bound to the ready review; replace all instructional text and source paths", func(a []string) int { return runHandoff("template", a) }, nil, flags(), "legato review handoff template > /tmp/handoff.json"),
		leaf("submit", "Submit a strict JSON packet for a clean ready review", func(a []string) int { return runHandoff("submit", a) }, nil, flags(valueFlag("file", "path|-", "Packet JSON file or stdin (maximum 256 KiB)")), "legato review handoff submit --file packet.json"),
		leaf("show", "Read persisted handoff history without syncing Git", func(a []string) int { return runHandoff("show", a) }, nil, flags(), "legato review handoff show --json"),
		leaf("export", "Preview selected context as Markdown; never publish", func(a []string) int { return runHandoff("export", a) }, nil, flags(valueFlag("packet", "id", "Exact packet ID"), valueFlag("format", "github|maintenance", "Brief format"), commandFlag{Name: "concept", Value: "id", Description: "Select a concept (repeatable; default all)", Repeatable: true}, boolFlag("include-sources", "Include pinned source excerpts"), boolFlag("include-notes", "Include local human notes")), "legato review handoff export --packet <id> --format github"))
}

func handoffCommandError(err error) *commandError {
	var validation *service.HandoffValidationError
	if errors.As(err, &validation) {
		return usageError("invalid_handoff", err.Error())
	}
	if errors.Is(err, service.ErrHandoffConflict) || errors.Is(err, store.ErrConflict) {
		return &commandError{Code: "handoff_conflict", Message: err.Error(), Exit: exitUsage}
	}
	if errors.Is(err, store.ErrNotFound) {
		return &commandError{Code: "handoff_not_found", Message: "handoff or selected review was not found", Exit: exitNotFound}
	}
	return &commandError{Code: "internal_error", Message: err.Error(), Exit: exitInternal}
}

func runHandoff(verb string, args []string) int {
	command := "review.handoff." + verb
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("review", "handoff", verb))
	asJSON := parsed.Present["json"]
	fail := func(err error) int { return renderCommandError(command, handoffCommandError(err), asJSON) }
	if parseErr != nil {
		return renderCommandError(command, parseErr, asJSON)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "handoff takes flags only"), asJSON)
	}
	if verb == "submit" && parsed.Values["file"] == "" {
		return renderCommandError(command, usageError("missing_file", "submit requires --file path|-"), asJSON)
	}
	if verb == "export" && (parsed.Values["packet"] == "" || (parsed.Values["format"] != "github" && parsed.Values["format"] != "maintenance")) {
		return renderCommandError(command, usageError("invalid_export", "export requires --packet ID --format github|maintenance"), asJSON)
	}
	taskID, name := parsed.Values["task"], parsed.Values["name"]
	if taskID == "" {
		taskID = os.Getenv("LEGATO_TASK_ID")
	}
	if name == "" {
		name = os.Getenv("LEGATO_REVIEW_NAME")
	}
	if taskID == "" {
		return renderCommandError(command, &commandError{Code: "agent_context_missing", Message: "pass --task or set LEGATO_TASK_ID", Exit: exitEnvironment}, asJSON)
	}
	cfg, db, code := openCommandStoreWithConfig(command, asJSON)
	if db == nil {
		return code
	}
	defer db.Close()
	svc := newReviewService(cfg, db, nil, nil)
	tour, err := svc.ResolveReviewTour(ctx(), taskID, name)
	if err != nil {
		return fail(err)
	}
	switch verb {
	case "template":
		if err := cli.ReviewHandoffTemplate(svc, tour.ID, os.Stdout); err != nil {
			return fail(err)
		}
	case "submit":
		var input io.Reader = os.Stdin
		if parsed.Values["file"] != "-" {
			f, err := os.Open(parsed.Values["file"])
			if err != nil {
				return fail(err)
			}
			defer f.Close()
			input = f
		}
		packet, err := cli.ReviewHandoffSubmit(svc, tour.ID, input)
		if err != nil {
			return fail(err)
		}
		if asJSON {
			return writeCommandSuccess(command, packet, "", true)
		}
		fmt.Fprintf(os.Stdout, "Handoff %s revision %d\n", packet.ID, packet.Revision)
	case "show":
		if err := cli.ReviewHandoffShow(svc, tour.ID, asJSON, os.Stdout); err != nil {
			return fail(err)
		}
	case "export":
		input := service.HandoffExportInput{PacketID: parsed.Values["packet"], Format: parsed.Values["format"], IncludeSources: parsed.Present["include-sources"], IncludeNotes: parsed.Present["include-notes"]}
		if parsed.Present["concept"] {
			input.ConceptIDs = strings.Split(parsed.Values["concept"], "\x00")
		}
		if err := cli.ReviewHandoffExport(svc, tour.ID, input, asJSON, os.Stdout); err != nil {
			return fail(err)
		}
	}
	return exitOK
}
