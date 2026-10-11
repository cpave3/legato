package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Fill gives a rendered block a solid background and an exact width: every
// line is padded, or cut, to width, and has the background behind all of it.
//
// A styled span ends by resetting every attribute, the background included.
// Fill puts the background back after each such reset, so the spans inside
// the block need not carry it themselves. A span that sets a background of
// its own keeps it, and a block filled earlier keeps its own inside a larger
// one that is filled afterwards.
func Fill(block string, width int, bg lipgloss.Color) string {
	if width < 1 {
		return ""
	}
	return fillLines(strings.Split(strings.TrimSuffix(block, "\n"), "\n"), width, bg)
}

// Canvas is Fill for a whole area: the block is also padded, or cut, to
// height lines.
func Canvas(block string, width, height int, bg lipgloss.Color) string {
	if width < 1 || height < 1 {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return fillLines(lines, width, bg)
}

func fillLines(lines []string, width int, bg lipgloss.Color) string {
	open := backgroundSequence(bg)
	filled := make([]string, len(lines))
	for i, line := range lines {
		filled[i] = fillLine(line, width, open)
	}
	return strings.Join(filled, "\n")
}

// Spread lays two pieces of one line at its two ends. When both do not fit
// with a space between them, the right one is left out.
func Spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if right == "" || gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func fillLine(line string, width int, open string) string {
	lineWidth := ansi.StringWidth(line)
	if lineWidth > width {
		line = ansi.Truncate(line, width, "")
		lineWidth = ansi.StringWidth(line)
	}
	padding := strings.Repeat(" ", width-lineWidth)
	if open == "" {
		return line + padding
	}
	return open + resumeBackground(line, open) + padding + "\x1b[0m"
}

// backgroundSequence is the escape sequence that sets a background in the
// terminal's color profile, or "" when the terminal has no colors.
func backgroundSequence(bg lipgloss.Color) string {
	params := lipgloss.ColorProfile().Color(string(bg)).Sequence(true)
	if params == "" {
		return ""
	}
	return "\x1b[" + params + "m"
}

// resumeBackground writes open after every escape sequence in line that
// takes the background away.
func resumeBackground(line, open string) string {
	if !strings.Contains(line, "\x1b[") {
		return line
	}
	var out strings.Builder
	out.Grow(len(line) + len(open)*4)
	for i := 0; i < len(line); {
		if line[i] != '\x1b' || i+1 >= len(line) || line[i+1] != '[' {
			out.WriteByte(line[i])
			i++
			continue
		}
		end := i + 2
		for end < len(line) && (line[end] < 0x40 || line[end] > 0x7e) {
			end++
		}
		if end >= len(line) {
			out.WriteString(line[i:])
			break
		}
		out.WriteString(line[i : end+1])
		if line[end] == 'm' && clearsBackground(line[i+2:end]) {
			out.WriteString(open)
		}
		i = end + 1
	}
	return out.String()
}

// clearsBackground reports whether the parameters of a color sequence reset
// every attribute or the background alone.
func clearsBackground(params string) bool {
	if params == "" {
		return true
	}
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "", "0", "49":
			return true
		case "38", "48", "58":
			// An extended color takes the fields that follow it; none of
			// them is an attribute.
			if i+1 < len(fields) {
				switch fields[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
		}
	}
	return false
}
