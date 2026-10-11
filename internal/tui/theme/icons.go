package theme

// Icons holds the icon glyphs for different providers and indicators.
type Icons struct {
	Jira         string
	GitHub       string
	Local        string
	Terminal     string
	Worktree     string
	Warning      string
	AgentWorking string
	AgentWaiting string
	// AgentWorked marks time an agent spent working. Unlike AgentWorking,
	// which a view turns while the agent runs, it never moves.
	AgentWorked string
	// AgentBackground marks an agent whose turn ended while work it started
	// still runs, AgentIdle one with nothing to do, AgentDone one that handed
	// its work over, and AgentMixed a swarm whose agents differ.
	AgentBackground string
	AgentIdle       string
	AgentDone       string
	AgentMixed      string
	CIPass          string
	CIFail          string
	CIPending       string
	PRApproved      string
	PRChanges       string
	PRComments      string
	PRDraft         string
}

// NewIcons creates an icon set based on the mode ("nerdfonts" or "unicode").
func NewIcons(mode string) Icons {
	if mode == "nerdfonts" {
		return Icons{
			Jira:            "\ue75c", // nf-dev-jira
			GitHub:          "\uf408", // nf-oct-mark_github
			Local:           "\uf444", // nf-oct-note
			Terminal:        "\uf489", // nf-cod-terminal
			Worktree:        "\uf1bb", // nf-fa-tree
			Warning:         "\uf071", // nf-fa-exclamation_triangle
			AgentWorking:    "\uf46a", // nf-oct-sync (spinning arrows)
			AgentWaiting:    "\uf4a5", // nf-oct-bell
			AgentWorked:     "\uf46a", // nf-oct-sync
			AgentBackground: "\uf489", // nf-cod-terminal
			AgentIdle:       "\uf10c", // nf-fa-circle_o
			AgentDone:       "\uf00c", // nf-fa-check
			AgentMixed:      "\uf0c0", // nf-fa-users
			CIPass:          "\uf00c", // nf-fa-check
			CIFail:          "\uf00d", // nf-fa-times
			CIPending:       "\uf110", // nf-fa-spinner
			PRApproved:      "\uf00c", // nf-fa-check
			PRChanges:       "\uf071", // nf-fa-exclamation_triangle
			PRComments:      "\uf075", // nf-fa-comment
			PRDraft:         "\uf040", // nf-fa-pencil
		}
	}
	// Unicode (safe for any terminal)
	return Icons{
		Jira:            "◈",
		GitHub:          "◉",
		Local:           "●",
		Terminal:        "▶",
		Worktree:        "⑂",
		Warning:         "!",
		AgentWorking:    "⠹",
		AgentWaiting:    "◆",
		AgentWorked:     "⠹",
		AgentBackground: "◐",
		AgentIdle:       "○",
		AgentDone:       "✓",
		AgentMixed:      "◇",
		CIPass:          "✓",
		CIFail:          "✗",
		CIPending:       "○",
		PRApproved:      "✓",
		PRChanges:       "⚑",
		PRComments:      "▪",
		PRDraft:         "~",
	}
}
