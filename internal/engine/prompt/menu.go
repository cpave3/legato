package prompt

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Menu is a list of numbered options a tool is showing in its terminal with
// one of them under the cursor: an update notice, a trust question, a picker.
// It is read from the screen alone, whichever tool drew it, so a prompt
// Legato has no dedicated card for can still be shown and answered.
const Menu PromptType = "menu"

const (
	// menuContextLines bounds how much of what stands above a menu is kept
	// as the question it asks.
	menuContextLines = 6
	// menuTrailingLines is how many lines of hints a menu may have under it
	// and still be what the screen is showing now.
	menuTrailingLines = 4
)

// menuOptionRe matches one option: an optional cursor, its number, its label.
var menuOptionRe = regexp.MustCompile(`^(\s*)([❯›>]?)(\s*)(\d+)\.\s+(\S.*?)\s*$`)

// menuRuleRe matches a line that only draws a border or a rule.
var menuRuleRe = regexp.MustCompile(`^[\s─━═╌╍┄┈\-_│┃╭╮╰╯┌┐└┘├┤]+$`)

// transcriptMarkers open a line of the conversation, which is where a
// menu's own text stops.
const transcriptMarkers = "•●⏺"

// unboxed drops the side borders a dialog is drawn in, keeping the indent.
func unboxed(line string) string {
	line = strings.TrimRight(line, " \t│┃")
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "│") || strings.HasPrefix(trimmed, "┃") {
		_, size := utf8.DecodeRuneInString(trimmed)
		return strings.Repeat(" ", len(line)-len(trimmed)+1) + trimmed[size:]
	}
	return line
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// detectMenu reads the menu the screen ends with, if it ends with one.
func detectMenu(cleaned string) (PromptState, bool) {
	raw := strings.Split(strings.TrimRight(cleaned, "\n \t"), "\n")
	lines := make([]string, len(raw))
	for index, line := range raw {
		lines[index] = unboxed(line)
	}

	// The last option numbered 1 is where the newest menu would begin.
	start := -1
	for index := len(lines) - 1; index >= 0; index-- {
		if match := menuOptionRe.FindStringSubmatch(lines[index]); match != nil && match[4] == "1" {
			start = index
			break
		}
	}
	if start < 0 {
		return PromptState{}, false
	}

	type option struct {
		label  string
		column int
	}
	var options []option
	selected, end := -1, start
	for index := start; index < len(lines); index++ {
		line := lines[index]
		if match := menuOptionRe.FindStringSubmatch(line); match != nil && match[4] == strconv.Itoa(len(options)+1) {
			if match[2] != "" {
				if selected >= 0 {
					return PromptState{}, false
				}
				selected = len(options)
			}
			options = append(options, option{label: match[5], column: len(match[1]) + len(match[2]) + len(match[3])})
			end = index
			continue
		}
		followedByOption := false
		if index+1 < len(lines) {
			next := menuOptionRe.FindStringSubmatch(lines[index+1])
			followedByOption = next != nil && next[4] == strconv.Itoa(len(options)+1)
		}
		switch {
		case strings.TrimSpace(line) == "" || menuRuleRe.MatchString(line):
			// A gap or a rule inside the list, when the list goes on after it.
			if !followedByOption {
				index = len(lines)
			}
		case indentOf(line) > options[len(options)-1].column:
			// An option wrapped onto another line, or its description.
			options[len(options)-1].label += " " + strings.TrimSpace(line)
			end = index
		default:
			if !followedByOption {
				index = len(lines)
			}
		}
	}
	if len(options) < 2 || selected < 0 {
		return PromptState{}, false
	}

	// A menu still on screen above newer output is not what is being asked.
	var trailing []string
	for _, line := range lines[end+1:] {
		if strings.TrimSpace(line) != "" && !menuRuleRe.MatchString(line) {
			trailing = append(trailing, line)
		}
	}
	if len(trailing) > menuTrailingLines {
		return PromptState{}, false
	}

	state := PromptState{Type: Menu, Context: menuContext(lines[:start])}
	canCancel := false
	for index, one := range options {
		keys := make([]string, 0, 4)
		for step := selected; step < index; step++ {
			keys = append(keys, "Down")
		}
		for step := selected; step > index; step-- {
			keys = append(keys, "Up")
		}
		state.Actions = append(state.Actions, Action{Label: one.label, Keys: strings.Join(append(keys, "Enter"), " ")})
		canCancel = canCancel || strings.Contains(strings.ToLower(one.label), "(esc)")
	}
	for _, hint := range trailing {
		canCancel = canCancel || strings.Contains(strings.ToLower(hint), "esc")
	}
	if canCancel {
		state.Actions = append(state.Actions, Action{Label: "Cancel (Esc)", Keys: "Escape"})
	}
	return state, true
}

// menuContext is what stands directly above a menu: the question it asks.
func menuContext(above []string) string {
	var kept []string
	for index := len(above) - 1; index >= 0 && len(kept) < menuContextLines; index-- {
		line := strings.TrimSpace(above[index])
		if line == "" {
			continue
		}
		if menuRuleRe.MatchString(above[index]) {
			break
		}
		if marker, _ := utf8.DecodeRuneInString(line); strings.ContainsRune(transcriptMarkers, marker) {
			break
		}
		kept = append([]string{line}, kept...)
	}
	return strings.Join(kept, "\n")
}
