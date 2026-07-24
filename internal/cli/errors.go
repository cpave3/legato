package cli

import "errors"

// ErrInvalidInput marks a command value rejected before any mutation.
var ErrInvalidInput = errors.New("invalid CLI input")
