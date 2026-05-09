package agents

import "github.com/cpave3/legato/internal/service"

// KillAgentMsg requests killing the selected agent.
type KillAgentMsg struct {
	TaskID string
}

// AttachSessionMsg requests attaching to a tmux session.
type AttachSessionMsg struct {
	TmuxSession string
}

// OpenAgentSpawnMsg requests opening the agent spawn overlay.
type OpenAgentSpawnMsg struct {
	TaskID string // empty for ephemeral agents
	Title  string // context title for task-bound agents
}

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
