package agents

import "github.com/cpave3/legato/internal/service"

// SpawnAgentMsg requests spawning an agent for a card.
type SpawnAgentMsg struct {
	TaskID string
	Width  int
	Height int
}

// KillAgentMsg requests killing the selected agent.
type KillAgentMsg struct {
	TaskID string
}

// AttachSessionMsg requests attaching to a tmux session.
type AttachSessionMsg struct {
	TmuxSession string
}

// OpenEphemeralSpawnMsg requests opening the ephemeral spawn overlay.
type OpenEphemeralSpawnMsg struct{}

// ReturnToBoardMsg signals returning to the board view.
type ReturnToBoardMsg struct{}

// CaptureOutputMsg carries captured terminal output.
type CaptureOutputMsg struct {
	Output string
}

// AgentsRefreshedMsg carries refreshed agent list.
type AgentsRefreshedMsg struct {
	Agents       []service.AgentSession
	SelectTask string // optional: select this ticket after refresh
}
