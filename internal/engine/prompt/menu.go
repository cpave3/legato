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
	// maxMenuRows bounds the options a menu may number and the rows its keys
	// may cross: terminal text is anyone's, and a menu-shaped block with a
	// huge option number must not become a huge list of keys.
	maxMenuRows = 50
)

// menuOptionRe matches one option: an optional cursor or scroll mark, its
// number, its label. A list too long for the screen marks its first and last
// visible options with ↑ and ↓ where the cursor would stand.
var menuOptionRe = regexp.MustCompile(`^(\s*)([❯›>↑↓]?)(\s*)(\d+)\.\s+(\S.*?)\s*$`)

// menuRowRe matches a row with no number of its own: the Submit row Claude
// Code draws under a multi-select's options.
var menuRowRe = regexp.MustCompile(`^(\s*)([❯›>]?)(\s*)(Submit|Next)\s*$`)

// menuRuleRe matches a line that only draws a border or a rule.
var menuRuleRe = regexp.MustCompile(`^[\s─━═╌╍┄┈\-_│┃╭╮╰╯┌┐└┘├┤]+$`)

// menuDashedRe matches a dashed rule, which frames part of a dialog (the
// command a permission prompt asks about) rather than ending it.
var menuDashedRe = regexp.MustCompile(`^[\s╌╍┄┈]+$`)

// menuTextRowRe matches a row that takes typed text: typing on it replaces
// its label with the text.
var menuTextRowRe = regexp.MustCompile(`(?i)^(\[.\]\s*)?Type something\.?$`)

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

func isCursor(mark string) bool {
	return mark != "" && mark != "↑" && mark != "↓"
}

// detectMenu reads the menu the screen ends with, if it ends with one.
func detectMenu(cleaned string) (PromptState, bool) {
	raw := strings.Split(strings.TrimRight(cleaned, "\n \t"), "\n")
	lines := make([]string, len(raw))
	for index, line := range raw {
		lines[index] = unboxed(line)
	}

	// The last option numbered 1, or the first one a scrolled list shows,
	// is where the newest menu would begin.
	start := -1
	for index := len(lines) - 1; index >= 0; index-- {
		if match := menuOptionRe.FindStringSubmatch(lines[index]); match != nil && (match[4] == "1" || match[2] == "↑") {
			start = index
			break
		}
	}
	if start < 0 {
		return PromptState{}, false
	}

	// A row is a line the cursor can stand on. Rows a scrolled list hides
	// are counted by their numbers, so keys reach past them.
	type row struct {
		label    string
		column   int
		position int
	}
	var rows []row
	number, position, scrollsOn := 0, 0, false
	// nextOption reads line as the next numbered option, if it is one.
	nextOption := func(line string) ([]string, int, bool) {
		match := menuOptionRe.FindStringSubmatch(line)
		if match == nil {
			return nil, 0, false
		}
		n, err := strconv.Atoi(match[4])
		if err != nil || n < 1 || n > maxMenuRows {
			return nil, 0, false
		}
		if len(rows) == 0 || n == number+1 || (scrollsOn && n > number+1) {
			return match, n, true
		}
		return nil, 0, false
	}
	selected, end := -1, start
	for index := start; index < len(lines); index++ {
		line := lines[index]
		if match, n, ok := nextOption(line); ok {
			if len(rows) > 0 {
				position += n - number
			}
			if isCursor(match[2]) {
				if selected >= 0 {
					return PromptState{}, false
				}
				selected = len(rows)
			}
			rows = append(rows, row{label: match[5], column: len(match[1]) + len(match[2]) + len(match[3]), position: position})
			number, scrollsOn, end = n, match[2] == "↓", index
			continue
		}
		if len(rows) == 0 {
			// The line it starts on is no option it can read.
			return PromptState{}, false
		}
		if match := menuRowRe.FindStringSubmatch(line); match != nil && strings.HasPrefix(rows[len(rows)-1].label, "[") {
			position++
			if isCursor(match[2]) {
				if selected >= 0 {
					return PromptState{}, false
				}
				selected = len(rows)
			}
			// Numbering goes on after it as though it were not there.
			rows = append(rows, row{label: match[4], column: len(match[1]) + len(match[2]) + len(match[3]), position: position})
			end = index
			continue
		}
		followedByOption := false
		if index+1 < len(lines) {
			_, _, followedByOption = nextOption(lines[index+1])
		}
		switch {
		case strings.TrimSpace(line) == "" || menuRuleRe.MatchString(line):
			// A gap or a rule inside the list, when the list goes on after it.
			if !followedByOption {
				index = len(lines)
			}
		case indentOf(line) > rows[len(rows)-1].column:
			// An option wrapped onto another line, or its description.
			rows[len(rows)-1].label += " " + strings.TrimSpace(line)
			end = index
		default:
			if !followedByOption {
				index = len(lines)
			}
		}
	}
	if len(rows) < 2 || selected < 0 || rows[len(rows)-1].position > maxMenuRows {
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
	from := rows[selected].position
	for _, one := range rows {
		keys := make([]string, 0, 4)
		for step := from; step < one.position; step++ {
			keys = append(keys, "Down")
		}
		for step := from; step > one.position; step-- {
			keys = append(keys, "Up")
		}
		action := Action{Label: one.label, Keys: strings.Join(append(keys, "Enter"), " ")}
		if menuTextRowRe.MatchString(one.label) {
			// In a multi-select, typing ticks the row and Enter would
			// untick it; elsewhere Enter answers with the text.
			action.TakesText = true
			action.TextSubmits = !strings.HasPrefix(one.label, "[")
		}
		state.Actions = append(state.Actions, action)
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
		if menuDashedRe.MatchString(above[index]) {
			continue
		}
		if menuRuleRe.MatchString(above[index]) {
			break
		}
		// The conversation's lines start at the margin; a dialog indents
		// its own bullets, such as the answers a form's review lists.
		if marker, _ := utf8.DecodeRuneInString(above[index]); strings.ContainsRune(transcriptMarkers, marker) {
			break
		}
		kept = append([]string{line}, kept...)
	}
	return strings.Join(kept, "\n")
}
