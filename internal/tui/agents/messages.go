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
	Agents     []service.AgentSession
	SelectTask string // optional: select this ticket after refresh
}

// OpenMacroPickerMsg is sent when the user presses 'm' in the agents view.
type OpenMacroPickerMsg struct{}

// OpenAgentActionMsg is sent when the user presses Shift+M on a swarm agent.
type OpenAgentActionMsg struct {
	TaskID       string
	ParentTaskID string
	Role         string
}

// StateTimelinesRefreshedMsg carries sparkline data per agent task ID.
type StateTimelinesRefreshedMsg struct {
	Timelines map[string][]string // taskID -> bucket labels
}
