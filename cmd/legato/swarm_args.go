package main

import (
	"errors"
	"strings"
)

// parseMessageArgs extracts subtask/parent ID, message text, and --urgent flag
// from a swarm message or broadcast argument slice. The flag can appear in any
// position; the remaining positional arguments are joined with spaces to form
// the text.
func parseMessageArgs(args []string) (id, text string, urgent bool, err error) {
	if len(args) < 2 {
		return "", "", false, errors.New("at least ID and message text required")
	}

	var pos []string
	for _, a := range args {
		if a == "--urgent" {
			if urgent {
				return "", "", false, errors.New("--urgent may only be specified once")
			}
			urgent = true
		} else if strings.HasPrefix(a, "--") {
			return "", "", false, errors.New("unknown flag " + a)
		} else {
			pos = append(pos, a)
		}
	}
	if len(pos) < 2 {
		return "", "", false, errors.New("at least ID and message text required")
	}
	id = pos[0]
	text = strings.Join(pos[1:], " ")
	return id, text, urgent, nil
}

// parseSwarmCreateArgs extracts the goal and optional --working-dir flag from
// `legato swarm create`. The flag can appear before or after goal words; all
// remaining positional args are joined as the goal.
func parseSwarmCreateArgs(args []string) (goal, workingDir string, err error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--working-dir":
			if workingDir != "" {
				return "", "", errors.New("--working-dir may only be specified once")
			}
			if i+1 >= len(args) {
				return "", "", errors.New("--working-dir requires a value")
			}
			workingDir = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "--") {
				return "", "", errors.New("unknown flag " + args[i])
			}
			pos = append(pos, args[i])
		}
	}
	if len(pos) == 0 {
		return "", "", errors.New("goal is required")
	}
	return strings.Join(pos, " "), workingDir, nil
}
