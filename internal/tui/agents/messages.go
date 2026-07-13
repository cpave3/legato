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

// OpenGroupMsg is sent when the user presses 'g' for the selected agent.
type OpenGroupMsg struct {
	TaskID string
	Group  string
}

// OpenAgentActionMsg is sent when the user presses Shift+M on a swarm agent.
type OpenAgentActionMsg struct {
	TaskID       string
	ParentTaskID string
	Role         string
}

// ToggleNotifyMsg is sent when the user presses 'n' to toggle push
// notifications for the selected agent.
type ToggleNotifyMsg struct {
	TaskID string
}

// NotifyToggledMsg carries the result of a notify toggle action.
type NotifyToggledMsg struct {
	TaskID  string
	Enabled bool
	Err     string
}

// StateTimelinesRefreshedMsg carries sparkline data per agent task ID.
type StateTimelinesRefreshedMsg struct {
	Timelines map[string][]string // taskID -> bucket labels
}

// VoiceToggleMsg is sent when the user presses 'v' in the agents view. The
// app-level handler starts or stops recording based on the current state.
type VoiceToggleMsg struct {
	TaskID      string
	TmuxSession string
	AgentKind   string
}

// VoiceRecordingMsg signals that recording state changed.
type VoiceRecordingMsg struct {
	Recording bool
}

// VoiceLevelMsg carries amplitude levels for the waveform display.
type VoiceLevelMsg struct {
	Levels []float64
}

// VoiceTranscriptionMsg carries the transcription result (or error).
type VoiceTranscriptionMsg struct {
	Text string
	Err  string
}

// VoiceTranscribingMsg signals that transcription is in progress.
type VoiceTranscribingMsg struct{}
