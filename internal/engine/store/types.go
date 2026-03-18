package store

type Ticket struct {
	ID            string `db:"id"`
	Summary       string `db:"summary"`
	Description   string `db:"description"`
	DescriptionMD string `db:"description_md"`
	Status        string `db:"status"`
	JiraStatus    string `db:"jira_status"`
	Priority      string `db:"priority"`
	IssueType     string `db:"issue_type"`
	Assignee      string `db:"assignee"`
	Labels        string `db:"labels"`
	EpicKey       string `db:"epic_key"`
	EpicName      string `db:"epic_name"`
	URL           string `db:"url"`
	CreatedAt     string `db:"created_at"`
	UpdatedAt     string `db:"updated_at"`
	JiraUpdatedAt string `db:"jira_updated_at"`
	SortOrder     int    `db:"sort_order"`
}

type ColumnMapping struct {
	ID             int    `db:"id"`
	ColumnName     string `db:"column_name"`
	JiraStatuses   string `db:"jira_statuses"`
	JiraTransition string `db:"jira_transition"`
	SortOrder      int    `db:"sort_order"`
}

type SyncLogEntry struct {
	ID        int    `db:"id"`
	TicketID  string `db:"ticket_id"`
	Action    string `db:"action"`
	Detail    string `db:"detail"`
	CreatedAt string `db:"created_at"`
}
