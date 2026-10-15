package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/service"
)

func resourceCommandError(err error) *commandError {
	if errors.Is(err, service.ErrResourceInvalid) {
		return &commandError{Code: "invalid_resource", Message: err.Error(), Exit: exitUsage}
	}
	return &commandError{Code: "internal_error", Message: err.Error(), Exit: exitInternal}
}

func runResourceContract(verb string, args []string) int {
	command := "resource." + verb
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("resource", verb))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	resources := configuredResourceService(cfg, db)
	ctx := context.Background()

	switch verb {
	case "list":
		if len(parsed.Positionals) != 0 {
			return renderCommandError(command, usageError("unexpected_argument", "resource list accepts no positional arguments"), jsonMode)
		}
		all, err := resources.List(ctx)
		if err != nil {
			return renderCommandError(command, resourceCommandError(err), jsonMode)
		}
		if all == nil {
			all = []service.Resource{}
		}
		return writeCommandSuccess(command, map[string]any{"resources": all}, resourceListText(all), jsonMode)
	case "add":
		if len(parsed.Positionals) < 2 {
			return renderCommandError(command, usageError("missing_argument", "resource add needs a name and at least one path"), jsonMode)
		}
		access := "read"
		if parsed.Present["write"] {
			access = "write"
		}
		var paths []service.ResourcePath
		for _, path := range parsed.Positionals[1:] {
			paths = append(paths, service.ResourcePath{Path: path, Access: access})
		}
		saved, err := resources.AddPaths(ctx, parsed.Positionals[0], paths)
		if err != nil {
			return renderCommandError(command, resourceCommandError(err), jsonMode)
		}
		return writeCommandSuccess(command, saved, resourceListText([]service.Resource{saved}), jsonMode)
	case "remove":
		if len(parsed.Positionals) != 1 {
			return renderCommandError(command, usageError("missing_argument", "resource remove needs the name of one resource"), jsonMode)
		}
		name := parsed.Positionals[0]
		if err := resources.RemoveNamed(ctx, name); err != nil {
			return renderCommandError(command, resourceCommandError(err), jsonMode)
		}
		return writeCommandSuccess(command, map[string]any{"removed": name}, fmt.Sprintf("Removed %s.\n", name), jsonMode)
	}
	return renderCommandError(command, usageError("unknown_command", fmt.Sprintf("unknown resource command %q", verb)), jsonMode)
}

// resourceListText writes each resource with its paths under it.
func resourceListText(resources []service.Resource) string {
	if len(resources) == 0 {
		return "No resources. Add one with: legato resource add <name> <path>...\n"
	}
	var b strings.Builder
	for _, resource := range resources {
		b.WriteString(resource.Name)
		if resource.Configured {
			b.WriteString(" (config.yaml)")
		}
		b.WriteString("\n")
		for _, path := range resource.Paths {
			fmt.Fprintf(&b, "  %-5s  %s\n", path.Access, path.Path)
		}
	}
	return b.String()
}
