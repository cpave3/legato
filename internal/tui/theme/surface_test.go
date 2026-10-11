package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// withColor renders with a terminal that has colors for one test. Tests
// otherwise run without one, and styles then add no escape sequences.
func withColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// backgroundAt reports the background in force at each visible cell of one
// line, as the last background sequence that preceded it ("" for none).
func backgroundsOf(line string) []string {
	var cells []string
	current := ""
	for i := 0; i < len(line); {
		if line[i] == '\x1b' && i+1 < len(line) && line[i+1] == '[' {
			end := strings.IndexByte(line[i:], 'm')
			if end < 0 {
				break
			}
			params := line[i+2 : i+end]
			switch {
			case params == "" || params == "0":
				current = ""
			case strings.Contains(params, "48;2;"):
				current = params[strings.Index(params, "48;2;"):]
			}
			i += end + 1
			continue
		}
		cells = append(cells, current)
		i++
	}
	return cells
}

func TestFillMakesEveryLineTheSameWidth(t *testing.T) {
	out := Fill("one\nlonger line\n", 14, Surface)

	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 14 {
			t.Errorf("line %d is %d wide, want 14: %q", i, w, line)
		}
	}
}

func TestFillCutsALineThatIsTooWide(t *testing.T) {
	out := Fill("a line that does not fit", 10, Surface)

	if w := lipgloss.Width(out); w != 10 {
		t.Fatalf("width = %d, want 10: %q", w, out)
	}
	if !strings.HasPrefix(ansi.Strip(out), "a line tha") {
		t.Fatalf("kept the wrong end of the line: %q", out)
	}
}

func TestFillKeepsItsBackgroundAfterAStyledSpanEnds(t *testing.T) {
	withColor(t)
	styled := lipgloss.NewStyle().Foreground(Accent).Render("id")

	out := Fill(styled+" title", 12, Surface)

	for i, bg := range backgroundsOf(out) {
		if bg != "48;2;27;26;34" {
			t.Fatalf("cell %d has background %q, want the surface's: %q", i, bg, out)
		}
	}
}

func TestFillLeavesASpanItsOwnBackground(t *testing.T) {
	withColor(t)
	chip := lipgloss.NewStyle().Background(lipgloss.Color("#000000")).Render("chip")

	cells := backgroundsOf(Fill(chip+" x", 8, Surface))

	if cells[0] != "48;2;0;0;0" {
		t.Fatalf("the span lost its own background: %v", cells)
	}
	if cells[5] != "48;2;27;26;34" {
		t.Fatalf("the surface did not resume after the span: %v", cells)
	}
}

func TestFillKeepsAnInnerSurfaceInsideAnOuterOne(t *testing.T) {
	withColor(t)
	inner := Fill(lipgloss.NewStyle().Foreground(Accent).Render("a")+"b", 4, Surface)

	cells := backgroundsOf(Fill(inner+"cd", 6, Background))

	for i := 0; i < 4; i++ {
		if cells[i] != "48;2;27;26;34" {
			t.Fatalf("cell %d of the inner surface has background %q: %v", i, cells[i], cells)
		}
	}
	for i := 4; i < 6; i++ {
		if cells[i] != "48;2;31;29;39" {
			t.Fatalf("cell %d of the outer surface has background %q: %v", i, cells[i], cells)
		}
	}
}

func TestFillResumesAfterContentThatClearsOnlyItsBackground(t *testing.T) {
	withColor(t)
	// A captured terminal ends a highlighted run this way.
	captured := "\x1b[48;2;9;9;9mhi\x1b[49m there"

	cells := backgroundsOf(Fill(captured, 10, Background))

	if cells[3] != "48;2;31;29;39" {
		t.Fatalf("the surface did not resume after the run: %v", cells)
	}
}

func TestFillAddsNoEscapeSequencesWithoutColors(t *testing.T) {
	if out := Fill("plain", 8, Surface); out != "plain   " {
		t.Fatalf("got %q", out)
	}
}

func TestCanvasIsExactlyTheSizeAsked(t *testing.T) {
	out := Canvas("one\ntwo\nthree\nfour", 6, 3, Background)

	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), out)
	}
	if lines[2] != "three " {
		t.Fatalf("third line = %q", lines[2])
	}

	taller := strings.Split(Canvas("one", 6, 3, Background), "\n")
	if len(taller) != 3 || taller[2] != "      " {
		t.Fatalf("a short block was not padded to the height: %q", taller)
	}
}

func TestSpreadPutsTheTwoEndsAtTheEdges(t *testing.T) {
	if got := Spread("left", "right", 12); got != "left   right" {
		t.Fatalf("got %q", got)
	}
}

func TestSpreadGivesUpTheRightEndWhenThereIsNoRoom(t *testing.T) {
	if got := Spread("left side", "right", 12); got != "left side" {
		t.Fatalf("got %q", got)
	}
}
