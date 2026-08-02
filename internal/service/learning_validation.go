package service

import (
	"errors"
	"fmt"
)

// ErrLearningValidation identifies errors caused by invalid learning API input.
// Callers should use errors.Is rather than matching error strings.
var ErrLearningValidation = errors.New("invalid learning input")

// LearningValidationError marks a user-correctable learning input error.
func LearningValidationError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrLearningValidation, fmt.Sprintf(format, args...))
}

// IsLearningValidation reports whether err is safe to classify as bad input.
func IsLearningValidation(err error) bool {
	return errors.Is(err, ErrLearningValidation) ||
		errors.Is(err, ErrInvalidReflection) ||
		errors.Is(err, ErrInvalidLearningPacket) ||
		errors.Is(err, ErrInvalidCuratorOperations)
}
