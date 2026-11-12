package config

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxReeveNameChars is the longest reeve.name, in characters.
const MaxReeveNameChars = 40

// CheckReeveName says whether name, trimmed, can be the Reeve's name: one
// line of plain text (letters, digits, spaces, and ' ’ - .), at most
// MaxReeveNameChars characters. It goes into the Reeve's prompt and into
// UI text, so it carries no Markdown, markup or control characters.
func CheckReeveName(name string) error {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxReeveNameChars {
		return errors.New("at most 40 characters")
	}
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsMark(r), unicode.IsDigit(r):
		case r == ' ', r == '\'', r == '’', r == '-', r == '.':
		default:
			return errors.New("letters, digits, spaces, apostrophes, hyphens and dots only, on one line")
		}
	}
	return nil
}

// DisplayName is the Reeve's name, trimmed, or "" when it has none or the
// one set is not a valid name.
func (c ReeveConfig) DisplayName() string {
	if CheckReeveName(c.Name) != nil {
		return ""
	}
	return strings.TrimSpace(c.Name)
}
