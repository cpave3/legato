package detail

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/clipboard"
	"github.com/cpave3/legato/internal/tui/theme"
)

// Messages

// CardLoadedMsg carries the loaded card detail.
type CardLoadedMsg struct {
	Card *service.CardDetail
	Err  error
}

// FeedbackMsg displays a temporary message in the status bar.
type FeedbackMsg struct {
	Text string
}

// ClearFeedbackMsg clears the feedback message.
type ClearFeedbackMsg struct{}

// BackToBoard signals the app to return to the board view.
type BackToBoard struct{}

// OpenMoveOverlay signals the app to open the move overlay for this ticket.
type OpenMoveOverlay struct {
	TicketID string
}

// Model is the detail view Bubbletea model.
type Model struct {
	card      *service.CardDetail
	ticketID  string
	svc       service.BoardService
	clip      *clipboard.Clipboard
	viewport  viewport.Model
	width     int
	height    int
	loading   bool
	feedback  string
	headerStr string
}

// New creates a detail model with card data already available.
func New(card *service.CardDetail, svc service.BoardService, clip *clipboard.Clipboard) Model {
	m := Model{
		card:    card,
		svc:     svc,
		clip:    clip,
		loading: card == nil,
	}
	if card != nil {
		m.ticketID = card.ID
	}
	return m
}

// NewLoading creates a detail model that will fetch data.
func NewLoading(ticketID string, svc service.BoardService, clip *clipboard.Clipboard) Model {
	return Model{
		ticketID: ticketID,
		svc:      svc,
		clip:     clip,
		loading:  true,
	}
}

// Init returns the initial command.
func (m Model) Init() tea.Cmd {
	if m.loading && m.svc != nil {
		return m.fetchCard()
	}
	return nil
}

func (m Model) fetchCard() tea.Cmd {
	svc := m.svc
	id := m.ticketID
	return func() tea.Msg {
		card, err := svc.GetCard(context.Background(), id)
		return CardLoadedMsg{Card: card, Err: err}
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case CardLoadedMsg:
		if msg.Err != nil {
			m.feedback = fmt.Sprintf("Error: %v", msg.Err)
			m.loading = false
			return m, nil
		}
		m.card = msg.Card
		m.loading = false
		m.renderContent()
		return m, nil

	case FeedbackMsg:
		m.feedback = msg.Text
		return m, nil

	case ClearFeedbackMsg:
		m.feedback = ""
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.renderContent()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward to viewport
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m, func() tea.Msg { return BackToBoard{} }
	case "j":
		m.viewport.LineDown(1)
	case "k":
		m.viewport.LineUp(1)
	case "d":
		m.viewport.HalfViewDown()
	case "u":
		m.viewport.HalfViewUp()
	case "g":
		m.viewport.GotoTop()
	case "G":
		m.viewport.GotoBottom()
	case "y":
		return m.copyContext(service.ExportFormatDescription, "Copied description!")
	case "Y":
		return m.copyContext(service.ExportFormatFull, "Copied full context!")
	case "o":
		return m.openURL()
	case "m":
		if m.card != nil {
			return m, func() tea.Msg { return OpenMoveOverlay{TicketID: m.card.ID} }
		}
	}
	return m, nil
}

func (m Model) copyContext(format service.ExportFormat, successMsg string) (tea.Model, tea.Cmd) {
	if m.clip == nil || !m.clip.Available() {
		m.feedback = "Clipboard unavailable"
		return m, nil
	}
	if m.svc == nil || m.card == nil {
		return m, nil
	}
	text, err := m.svc.ExportCardContext(context.Background(), m.card.ID, format)
	if err != nil {
		m.feedback = fmt.Sprintf("Error: %v", err)
		return m, nil
	}
	if err := m.clip.Copy(text); err != nil {
		m.feedback = fmt.Sprintf("Copy failed: %v", err)
		return m, nil
	}
	m.feedback = successMsg
	return m, nil
}

func (m Model) openURL() (tea.Model, tea.Cmd) {
	if m.card == nil || m.card.URL == "" {
		m.feedback = "No URL available"
		return m, nil
	}
	if err := clipboard.OpenURL(m.card.URL); err != nil {
		m.feedback = fmt.Sprintf("Open failed: %v", err)
		return m, nil
	}
	m.feedback = "Opened in browser"
	return m, nil
}

// renderContent rebuilds the header and viewport content.
func (m *Model) renderContent() {
	if m.card == nil || m.width == 0 || m.height == 0 {
		return
	}

	m.headerStr = m.renderHeader()
	headerHeight := lipgloss.Height(m.headerStr)
	statusBarHeight := 1

	vpHeight := m.height - headerHeight - statusBarHeight
	if vpHeight < 1 {
		vpHeight = 1
	}

	contentWidth := m.width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	m.viewport = viewport.New(m.width, vpHeight)
	m.viewport.YPosition = headerHeight

	// Render markdown with Glamour (use DarkStyleConfig — AutoStyle blocks
	// because it probes terminal background via stdin/stdout, which conflicts
	// with bubbletea's alt-screen)
	rendered := m.card.DescriptionMD
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(styles.DarkStyleConfig),
		glamour.WithWordWrap(contentWidth),
	)
	if err == nil {
		if out, err := r.Render(m.card.DescriptionMD); err == nil {
			rendered = out
		}
	}
	m.viewport.SetContent(rendered)
}

func (m Model) renderHeader() string {
	if m.card == nil {
		return ""
	}

	contentWidth := m.width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TextPrimary).
		Width(contentWidth).
		Padding(0, 1)

	title := titleStyle.Render(fmt.Sprintf("%s: %s", m.card.ID, m.card.Summary))

	// Metadata grid
	var metaParts []string
	addMeta := func(label, value string) {
		if value != "" {
			labelStyle := lipgloss.NewStyle().Foreground(theme.TextTertiary)
			valueStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary)
			metaParts = append(metaParts, labelStyle.Render(label+":")+valueStyle.Render(" "+value))
		}
	}

	addMeta("Status", m.card.Status)
	addMeta("Priority", m.card.Priority)
	addMeta("Type", m.card.IssueType)
	if m.card.EpicName != "" {
		addMeta("Epic", m.card.EpicName)
	} else if m.card.EpicKey != "" {
		addMeta("Epic", m.card.EpicKey)
	}
	addMeta("Labels", m.card.Labels)
	addMeta("URL", m.card.URL)

	metaLine := lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(metaParts, "  "))
	separator := lipgloss.NewStyle().
		Foreground(theme.TextTertiary).
		Padding(0, 1).
		Render(strings.Repeat("─", contentWidth-2))

	return lipgloss.JoinVertical(lipgloss.Left, title, metaLine, separator)
}

// View renders the detail view.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.loading {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, "Loading...")
	}

	if m.card == nil {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, "No data")
	}

	header := m.headerStr
	body := m.viewport.View()
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, statusBar)
}

func (m Model) renderStatusBar() string {
	if m.feedback != "" {
		return theme.StatusBar.Width(m.width).Render(m.feedback)
	}

	hints := []struct{ key, label string }{
		{"esc", "back"},
		{"y", "copy desc"},
		{"Y", "copy full"},
		{"m", "move"},
		{"o", "open"},
		{"j/k", "scroll"},
	}

	var parts []string
	for _, h := range hints {
		parts = append(parts,
			theme.KeyHintKey.Render(h.key)+" "+theme.KeyHintLabel.Render(h.label))
	}

	return theme.StatusBar.Width(m.width).Render(strings.Join(parts, "  "))
}

// TicketID returns the current ticket ID.
func (m Model) TicketID() string {
	return m.ticketID
}

// SetCard updates the card data (e.g., after a move).
func (m *Model) SetCard(card *service.CardDetail) {
	m.card = card
	m.renderContent()
}
