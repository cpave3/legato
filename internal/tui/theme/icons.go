package theme

// Icons holds the icon glyphs for different providers and indicators.
type Icons struct {
	Jira             string
	GitHub           string
	Local            string
	Terminal         string
	Warning          string
	AgentWorking     string
	AgentWaiting     string
	CIPass           string
	CIFail           string
	CIPending        string
	PRApproved       string
	PRChanges        string
	PRComments       string
	PRDraft          string
}

// NewIcons creates an icon set based on the mode ("nerdfonts" or "unicode").
func NewIcons(mode string) Icons {
	if mode == "nerdfonts" {
		return Icons{
			Jira:         "\ue75c", // nf-dev-jira
			GitHub:       "\uf408", // nf-oct-mark_github
			Local:        "\uf444", // nf-oct-note
			Terminal:     "\uf489", // nf-cod-terminal
			Warning:      "\uf071", // nf-fa-exclamation_triangle
			AgentWorking: "\uf46a", // nf-oct-sync (spinning arrows)
			AgentWaiting: "\uf4a5", // nf-oct-bell
			CIPass:       "\uf00c", // nf-fa-check
			CIFail:       "\uf00d", // nf-fa-times
			CIPending:    "\uf110", // nf-fa-spinner
			PRApproved:   "\uf00c", // nf-fa-check
			PRChanges:    "\uf071", // nf-fa-exclamation_triangle
			PRComments:   "\uf075", // nf-fa-comment
			PRDraft:      "\uf040", // nf-fa-pencil
		}
	}
	// Unicode (safe for any terminal)
	return Icons{
		Jira:         "◈",
		GitHub:       "◉",
		Local:        "●",
		Terminal:     "▶",
		Warning:      "!",
		AgentWorking: "⟳",
		AgentWaiting: "◆",
		CIPass:       "✓",
		CIFail:       "✗",
		CIPending:    "○",
		PRApproved:   "✓",
		PRChanges:    "⚑",
		PRComments:   "▪",
		PRDraft:      "~",
	}
}
