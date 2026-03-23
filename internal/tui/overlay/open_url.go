package overlay

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/theme"
)

// OpenURLSelectedMsg is sent when the user picks a URL to open.
type OpenURLSelectedMsg struct {
	URL string
}

// OpenURLCancelledMsg is sent when the user dismisses the picker.
type OpenURLCancelledMsg struct{}

// OpenURLOverlay lets the user choose between provider and PR URLs.
type OpenURLOverlay struct {
	providerURL string
	prURL       string
	width       int
	height      int
}

// NewOpenURL creates a URL picker overlay.
func NewOpenURL(providerURL, prURL string, width, height int) OpenURLOverlay {
	return OpenURLOverlay{
		providerURL: providerURL,
		prURL:       prURL,
		width:       width,
		height:      height,
	}
}

// Init returns no command.
func (m OpenURLOverlay) Init() tea.Cmd { return nil }

// Update handles messages.
func (m OpenURLOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "j":
			url := m.providerURL
			return m, func() tea.Msg { return OpenURLSelectedMsg{URL: url} }
		case "g":
			url := m.prURL
			return m, func() tea.Msg { return OpenURLSelectedMsg{URL: url} }
		case "esc", "q":
			return m, func() tea.Msg { return OpenURLCancelledMsg{} }
		}
	}
	return m, nil
}

// View renders the overlay.
func (m OpenURLOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary).
		Padding(0, 1)

	hintStyle := lipgloss.NewStyle().
		Foreground(theme.TextSecondary).
		Padding(0, 1)

	footerStyle := lipgloss.NewStyle().
		Foreground(theme.TextTertiary).
		Padding(0, 1)

	lines := []string{
		titleStyle.Render("Open Link"),
		"",
		hintStyle.Render("j = Jira"),
		hintStyle.Render("g = GitHub PR"),
		"",
		footerStyle.Render("esc = cancel"),
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return RenderPanel(content, m.width, m.height)
}
