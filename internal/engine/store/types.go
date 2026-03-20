package store

type Task struct {
	ID            string  `db:"id"`
	Title         string  `db:"title"`
	Description   string  `db:"description"`
	DescriptionMD string  `db:"description_md"`
	Status        string  `db:"status"`
	Priority      string  `db:"priority"`
	SortOrder     int     `db:"sort_order"`
	Provider      *string `db:"provider"`
	RemoteID      *string `db:"remote_id"`
	RemoteMeta    *string `db:"remote_meta"`
	CreatedAt     string  `db:"created_at"`
	UpdatedAt     string  `db:"updated_at"`
}


type ColumnMapping struct {
	ID               int    `db:"id"`
	ColumnName       string `db:"column_name"`
	RemoteStatuses   string `db:"remote_statuses"`
	RemoteTransition string `db:"remote_transition"`
	SortOrder        int    `db:"sort_order"`
}

type SyncLogEntry struct {
	ID        int    `db:"id"`
	TaskID    string `db:"task_id"`
	Action    string `db:"action"`
	Detail    string `db:"detail"`
	CreatedAt string `db:"created_at"`
}

type AgentSession struct {
	ID          int     `db:"id"`
	TaskID      string  `db:"task_id"`
	TmuxSession string  `db:"tmux_session"`
	Command     string  `db:"command"`
	Status      string  `db:"status"`
	StartedAt   string  `db:"started_at"`
	EndedAt     *string `db:"ended_at"`
}
