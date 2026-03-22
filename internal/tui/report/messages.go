package report

import "github.com/cpave3/legato/internal/service"

// ReportLoadedMsg carries a completed report.
type ReportLoadedMsg struct {
	Report *service.Report
	Err    error
}

// ReturnToBoardMsg signals returning to the board view.
type ReturnToBoardMsg struct{}

// CopyReportMsg signals that the report markdown should be copied.
type CopyReportMsg struct {
	Markdown string
}
