package theme

// Icons holds the icon glyphs for different providers and indicators.
type Icons struct {
	Jira     string
	GitHub   string
	Local    string
	Terminal string
	Warning  string
}

// NewIcons creates an icon set based on the mode ("nerdfonts" or "unicode").
func NewIcons(mode string) Icons {
	if mode == "nerdfonts" {
		return Icons{
			Jira:     "\ue75c", // nf-dev-jira
			GitHub:   "\uf408", // nf-oct-mark_github
			Local:    "\uf444", // nf-oct-note
			Terminal: "\uf489", // nf-cod-terminal
			Warning:  "\uf071", // nf-fa-exclamation_triangle
		}
	}
	// Unicode (safe for any terminal)
	return Icons{
		Jira:     "◈",
		GitHub:   "◉",
		Local:    "●",
		Terminal: "▶",
		Warning:  "!",
	}
}
