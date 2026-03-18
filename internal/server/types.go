package server

// HealthResponse is the JSON response for GET /health.
type HealthResponse struct {
	Status   string           `json:"status"`
	Columns  []ColumnResponse `json:"columns"`
	SyncedAt *string          `json:"synced_at"`
}

// ColumnResponse represents a column with its cards in the health response.
type ColumnResponse struct {
	Name  string         `json:"name"`
	Cards []CardResponse `json:"cards"`
}

// CardResponse represents a card summary in the health response.
type CardResponse struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
}
