// Package macros defines the shared macro type and config-related helpers.
// Macros are persisted in legato's YAML config as a top-level list.
package macros

// Macro is a named send-keys payload stored in config.
type Macro struct {
	Name string `yaml:"name"`
	Keys string `yaml:"keys"`
}

// ListResult is the JSON shape returned by GET /api/macros.
type ListResult struct {
	Macros []Macro `json:"macros"`
}
