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
			urgent = true
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
