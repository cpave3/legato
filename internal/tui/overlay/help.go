package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpave3/legato/internal/tui/hotkey"
	"github.com/cpave3/legato/internal/tui/theme"
)

// HelpClosedMsg is sent when the help overlay is dismissed.
type HelpClosedMsg struct{}

// HelpMode is retained as the overlay's public page selector; bindings and
// labels come from the hotkey registry.
type HelpMode = hotkey.Page

const (
	HelpModeBoard     = hotkey.PageBoard
	HelpModeDetail    = hotkey.PageDetail
	HelpModeAgents    = hotkey.PageAgents
	HelpModeReport    = hotkey.PageReport
	HelpModeReview    = hotkey.PageReview
	HelpModePlans     = hotkey.PagePlans
	HelpModeAttention = hotkey.PageAttention
)

// HelpOverlay displays registry-derived keyboard reference data.
type HelpOverlay struct {
	width    int
	height   int
	registry *hotkey.Registry
	context  hotkey.Context
}

func NewHelp(width, height int) HelpOverlay {
	return NewHelpWithMode(width, height, HelpModeBoard)
}

func NewHelpWithMode(width, height int, mode HelpMode) HelpOverlay {
	ctx := hotkey.Context{Page: mode, Mode: defaultHelpMode(mode), Capabilities: allHelpCapabilities(), Predicates: hotkey.NewPredicates(hotkey.PredicateLocalTask, hotkey.PredicateEditor)}
	return HelpOverlay{width: width, height: height, registry: hotkey.DefaultRegistry(), context: ctx}
}

func NewHelpWithContext(width, height int, registry *hotkey.Registry, ctx hotkey.Context) HelpOverlay {
	return HelpOverlay{width: width, height: height, registry: registry, context: ctx}
}

func defaultHelpMode(page hotkey.Page) hotkey.Mode {
	if page == hotkey.PageReview {
		return hotkey.ModeTour
	}
	if page == hotkey.PagePlans {
		return hotkey.ModeQueue
	}
	return ""
}

func allHelpCapabilities() hotkey.Capabilities {
	return hotkey.NewCapabilities(
		hotkey.CapabilitySwarm, hotkey.CapabilityVoice, hotkey.CapabilityNotifications,
		hotkey.CapabilityReview, hotkey.CapabilityPlans, hotkey.CapabilityAttention,
		hotkey.CapabilitySync, hotkey.CapabilityPR,
	)
}

func (m HelpOverlay) WithWorktrees(enabled bool) HelpOverlay {
	if m.context.Capabilities == nil {
		m.context.Capabilities = allHelpCapabilities()
	}
	m.context.Capabilities[hotkey.CapabilityWorktrees] = enabled
	return m
}

func (m HelpOverlay) Init() tea.Cmd { return nil }

func (m HelpOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.String() == "esc" || msg.String() == "?" || msg.String() == "q" {
			return m, func() tea.Msg { return HelpClosedMsg{} }
		}
	}
	return m, nil
}

func (m HelpOverlay) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.TextPrimary)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.AccentPurple).MarginTop(1)
	keyStyle := lipgloss.NewStyle().Foreground(theme.AccentPurple).Bold(true).Width(12)
	descStyle := lipgloss.NewStyle().Foreground(theme.TextSecondary)
	shortlistKeyStyle := lipgloss.NewStyle().Foreground(theme.AccentPurpleAlt).Bold(true)

	lines := []string{titleStyle.Render("Legato -- Keyboard Reference")}
	shortlist := m.registry.Bindings(m.context, hotkey.Shortlist)
	if len(shortlist) > 0 {
		lines = append(lines, sectionStyle.Render(hotkey.PageTitle(m.context.Page)+" — Quick Reference"))
		parts := make([]string, 0, len(shortlist))
		for _, binding := range shortlist {
			parts = append(parts, shortlistKeyStyle.Render(hotkey.DisplayKey(binding))+" "+descStyle.Render(binding.Label))
		}
		lines = append(lines, strings.Join(parts, "  "))
	}

	fullContext := m.context
	fullContext.Predicates = hotkey.NewPredicates(hotkey.PredicateLocalTask, hotkey.PredicateEditor, hotkey.PredicateSwarmNext)
	for _, section := range m.registry.Sections(fullContext, hotkey.FullHelp) {
		lines = append(lines, sectionStyle.Render(section.Name))
		for _, binding := range section.Bindings {
			lines = append(lines, fmt.Sprintf("  %s%s", keyStyle.Render(hotkey.DisplayKey(binding)), descStyle.Render(binding.Help)))
		}
	}
	return RenderPanel(lipgloss.JoinVertical(lipgloss.Left, lines...), m.width, m.height)
}
