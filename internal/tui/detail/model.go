package detail

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	TaskID string
}

// OpenDeleteOverlay signals the app to open the delete confirmation for this task.
type OpenDeleteOverlay struct {
	TaskID string
}

// OpenTitleEditOverlay signals the app to open the title edit overlay.
type OpenTitleEditOverlay struct {
	TaskID string
	Title  string
}

// TitleUpdatedMsg signals that the title was updated (sent by app after overlay confirms).
type TitleUpdatedMsg struct {
	Title string
}

// DescriptionEditedMsg carries the result of editing a description in an external editor.
type DescriptionEditedMsg struct {
	Content string
	Err     error
}

// Model is the detail view Bubbletea model.
type Model struct {
	card      *service.CardDetail
	taskID    string
	svc       service.BoardService
	clip      *clipboard.Clipboard
	editor    string
	viewport  viewport.Model
	width     int
	height    int
	loading   bool
	feedback  string
	headerStr string
}

// New creates a detail model with card data already available.
func New(card *service.CardDetail, svc service.BoardService, clip *clipboard.Clipboard, editor string) Model {
	m := Model{
		card:    card,
		svc:     svc,
		clip:    clip,
		editor:  editor,
		loading: card == nil,
	}
	if card != nil {
		m.taskID = card.ID
	}
	return m
}

// NewLoading creates a detail model that will fetch data.
func NewLoading(taskID string, svc service.BoardService, clip *clipboard.Clipboard, editor string) Model {
	return Model{
		taskID:  taskID,
		svc:     svc,
		clip:    clip,
		editor:  editor,
		loading: true,
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
	id := m.taskID
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

	case TitleUpdatedMsg:
		if m.card != nil {
			m.card.Title = msg.Title
			m.renderContent()
		}
		m.feedback = "Title updated"
		return m, nil

	case DescriptionEditedMsg:
		if msg.Err != nil {
			m.feedback = fmt.Sprintf("Edit failed: %v", msg.Err)
			return m, nil
		}
		if m.card != nil {
			m.card.DescriptionMD = msg.Content
			m.renderContent()
		}
		m.feedback = "Description updated"
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
			return m, func() tea.Msg { return OpenMoveOverlay{TaskID: m.card.ID} }
		}
	case "D":
		if m.card != nil {
			id := m.card.ID
			return m, func() tea.Msg { return OpenDeleteOverlay{TaskID: id} }
		}
	case "e":
		return m.editDescription()
	case "t":
		if m.card != nil {
			if m.card.Provider != "" {
				m.feedback = "Cannot edit remote task title"
				return m, nil
			}
			return m, func() tea.Msg {
				return OpenTitleEditOverlay{TaskID: m.card.ID, Title: m.card.Title}
			}
		}
	}
	return m, nil
}

func (m Model) editDescription() (tea.Model, tea.Cmd) {
	if m.card == nil {
		return m, nil
	}
	if m.card.Provider != "" {
		m.feedback = "Cannot edit remote task description"
		return m, nil
	}

	// Write current description to temp file
	tmpFile, err := os.CreateTemp("", "legato-desc-*.md")
	if err != nil {
		m.feedback = fmt.Sprintf("Error: %v", err)
		return m, nil
	}
	if _, err := tmpFile.WriteString(m.card.DescriptionMD); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		m.feedback = fmt.Sprintf("Error: %v", err)
		return m, nil
	}
	tmpFile.Close()

	tmpPath := tmpFile.Name()
	editorCmd := m.editor
	if editorCmd == "" {
		editorCmd = "vi"
	}

	// Split editor command for args like "code --wait"
	parts := strings.Fields(editorCmd)
	parts = append(parts, tmpPath)
	c := exec.Command(parts[0], parts[1:]...)

	svc := m.svc
	taskID := m.card.ID

	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpPath)
		if err != nil {
			return DescriptionEditedMsg{Err: err}
		}
		content, readErr := os.ReadFile(tmpPath)
		if readErr != nil {
			return DescriptionEditedMsg{Err: readErr}
		}
		newDesc := string(content)
		if svc != nil {
			if updateErr := svc.UpdateTaskDescription(context.Background(), taskID, newDesc); updateErr != nil {
				return DescriptionEditedMsg{Err: updateErr}
			}
		}
		return DescriptionEditedMsg{Content: newDesc}
	})
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
	url := ""
	if m.card != nil && m.card.RemoteMeta != nil {
		url = m.card.RemoteMeta["url"]
	}
	if url == "" {
		m.feedback = "No URL available"
		return m, nil
	}
	if err := clipboard.OpenURL(url); err != nil {
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

	title := titleStyle.Render(fmt.Sprintf("%s: %s", m.card.ID, m.card.Title))

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
	if m.card.RemoteMeta != nil {
		addMeta("Type", m.card.RemoteMeta["issue_type"])
		if epicName := m.card.RemoteMeta["epic_name"]; epicName != "" {
			addMeta("Epic", epicName)
		} else {
			addMeta("Epic", m.card.RemoteMeta["epic_key"])
		}
		addMeta("Labels", m.card.RemoteMeta["labels"])
		addMeta("URL", m.card.RemoteMeta["url"])
	}

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
		{"D", "delete"},
		{"o", "open"},
		{"j/k", "scroll"},
	}
	// Show edit hints only for local tasks
	if m.card != nil && m.card.Provider == "" {
		hints = append(hints, struct{ key, label string }{"t", "edit title"})
		if m.editor != "" {
			hints = append(hints, struct{ key, label string }{"e", "edit desc"})
		}
	}

	var parts []string
	for _, h := range hints {
		parts = append(parts,
			theme.KeyHintKey.Render(h.key)+" "+theme.KeyHintLabel.Render(h.label))
	}

	return theme.StatusBar.Width(m.width).Render(strings.Join(parts, "  "))
}

// TaskID returns the current ticket ID.
func (m Model) TaskID() string {
	return m.taskID
}

// SetCard updates the card data (e.g., after a move).
func (m *Model) SetCard(card *service.CardDetail) {
	m.card = card
	m.renderContent()
}
