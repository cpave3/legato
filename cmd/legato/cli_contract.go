package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	exitOK          = 0
	exitInternal    = 1
	exitUsage       = 2
	exitNotFound    = 3
	exitForbidden   = 4
	exitEnvironment = 5
	exitDependency  = 6
)

type commandError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	Exit    int            `json:"-"`
}

func (e *commandError) Error() string { return e.Message }

type parsedArgs struct {
	Positionals []string
	Values      map[string]string
	Present     map[string]bool
}

type flagSpec struct {
	Boolean bool
}

func parseCommandArgs(args []string, specs map[string]flagSpec) (parsedArgs, *commandError) {
	out := parsedArgs{Values: map[string]string{}, Present: map[string]bool{}}
	flagsEnded := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if flagsEnded || !strings.HasPrefix(arg, "--") || arg == "-" {
			out.Positionals = append(out.Positionals, arg)
			continue
		}
		if arg == "--" {
			flagsEnded = true
			continue
		}
		nameValue := strings.TrimPrefix(arg, "--")
		name, inline, hasInline := strings.Cut(nameValue, "=")
		spec, ok := specs[name]
		if !ok {
			return out, usageError("unknown_flag", fmt.Sprintf("unknown flag --%s", name))
		}
		if out.Present[name] {
			return out, usageError("duplicate_flag", fmt.Sprintf("flag --%s may only be specified once", name))
		}
		out.Present[name] = true
		if spec.Boolean {
			if hasInline {
				return out, usageError("invalid_flag_value", fmt.Sprintf("flag --%s does not take a value", name))
			}
			out.Values[name] = "true"
			continue
		}
		if hasInline {
			out.Values[name] = inline
			continue
		}
		if i+1 >= len(args) {
			return out, usageError("missing_argument", fmt.Sprintf("missing value for --%s", name))
		}
		i++
		out.Values[name] = args[i]
	}
	return out, nil
}

func usageError(code, message string) *commandError {
	return &commandError{Code: code, Message: message, Exit: exitUsage}
}

func renderCommandError(command string, err *commandError, jsonMode bool) int {
	if err.Exit == 0 {
		err.Exit = exitInternal
	}
	if jsonMode {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{
			"ok": false, "command": command, "error": err,
		})
	} else {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Message)
	}
	return err.Exit
}

func writeCommandSuccess(command string, data any, text string, jsonMode bool) int {
	if jsonMode {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"ok": true, "command": command, "data": data,
		})
		return exitOK
	}
	if text != "" {
		fmt.Fprint(os.Stdout, text)
		if !strings.HasSuffix(text, "\n") {
			fmt.Fprintln(os.Stdout)
		}
	}
	return exitOK
}

func resolveTaskID(positionals []string) (string, []string, *commandError) {
	if len(positionals) > 0 && strings.TrimSpace(positionals[0]) != "" {
		return positionals[0], positionals[1:], nil
	}
	if taskID := strings.TrimSpace(os.Getenv("LEGATO_TASK_ID")); taskID != "" {
		return taskID, positionals, nil
	}
	return "", positionals, &commandError{
		Code: "agent_context_missing", Message: "no task ID provided and LEGATO_TASK_ID is not set",
		Exit: exitEnvironment,
	}
}

func readTextInput(inline string, hasInline bool, path string, hasPath bool, stdin io.Reader) (string, *commandError) {
	if hasInline && hasPath {
		return "", usageError("conflicting_input", "use only one of --description and --description-file")
	}
	if hasInline {
		return inline, nil
	}
	if !hasPath {
		return "", nil
	}
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", &commandError{Code: "input_unavailable", Message: fmt.Sprintf("read description: %v", err), Exit: exitUsage}
	}
	if !utf8.Valid(data) {
		return "", usageError("invalid_text", "description must be valid UTF-8 text")
	}
	return string(data), nil
}
