package cli

import (
	"fmt"
	"strings"
)

// noteReadWords are words that ask to read. On its own, in place of the
// task ID or as the whole message, one means the caller wanted to read
// notes, not write one: `legato task note <id> list` would otherwise add
// a note saying "list".
var noteReadWords = map[string]bool{
	"list": true, "ls": true, "show": true, "get": true, "read": true, "view": true, "notes": true,
}

// CheckTaskNoteArgs refuses `legato task note` arguments (the task ID and
// the message) when one is only a word that asks to read, and points to
// `legato task notes`.
func CheckTaskNoteArgs(args ...string) error {
	for _, arg := range args {
		word := strings.ToLower(strings.TrimSpace(arg))
		if noteReadWords[word] {
			return fmt.Errorf("%w: legato task note writes: it appends a line to a task's description, and %q reads like a request to read. To read a task's notes: legato task notes <task-id>. To write a note, give the task ID and a message of more than that word", ErrInvalidInput, strings.TrimSpace(arg))
		}
	}
	return nil
}
