import type { StoredBackgroundShell } from "./backgroundShells"
import type { RepositoryCacheStatus } from "./preparingAgents"

// What the server sends over its WebSocket and in its agent list: the
// messages, the agents, their background jobs and the prompt a terminal
// shows. Types only, so lib and hooks alike can name them.
export interface WSMessage {
  type: "agent_output" | "agent_list" | "agents_changed" | "prompt_state" | "error" | "chimera_event" | "chimera_sync_required"
    | "subscribe_agent" | "unsubscribe_agent" | "send_keys" | "resize" | "detect_prompt" | "refresh_pane"
    | "agent_message" | "agent_message_sent" | "agent_interrupt" | "agent_permission" | "agent_question"
    | "plan_proposed" | "plan_verdict" | "plan_changed" | "swarm_changed" | "cards_changed" | "review_changed" | "workflow_changed" | "attention_changed" | "approvals_changed"
    | "answer_terminal_prompt" | "terminal_prompt_answered"
    | "agent_ui_changed" | "agent_context" | "agent_stage_changed" | "pairing_changed" | "repository_cache" | "settings_changed" | "server_theme_changed" | "usage_changed" | "update_changed"
  agent_id?: string
  // How full a native agent's context window is (agent_context).
  context_used?: number
  context_window?: number
  cursor?: string
  queue?: boolean
  content?: string
  full?: boolean
  keys?: string
  cols?: number
  rows?: number
  agents?: AgentInfo[]
  prompt?: PromptState | null
  error?: string
  // Plan proposal / swarm change fields
  plan_path?: string
  reply_socket?: string
  parent_task_id?: string
  status?: string
  notes?: string
  subtask_id?: string
  new_status?: string
  mode?: string
  // Review change fields
  task_id?: string
  tour_id?: string
  step_id?: string
  kind?: string
  plan_id?: string
  revision_id?: string
  workflow_run_id?: string
  workflow_step_id?: string
  item_id?: string
  // agent_ui_changed: where the item is drawn and which revision it is at.
  placement?: "panel" | "inline"
  revision?: number
  producer?: string
  source_key?: string
  request_id?: string
  // Why an error happened, for errors the client acts on (prompt_changed).
  code?: string
  decision?: "allow" | "deny"
  feedback?: string
  // agent_permission: which of the request's options an allow takes.
  option?: string
  // agent_question: what the person answered, per question ID, or that they
  // closed the question unanswered.
  answers?: Record<string, string[]>
  dismissed?: boolean
  // repository_cache: how a repository's copy in the server's cache stands.
  cache?: RepositoryCacheStatus
}

export interface AgentInfo {
  id: number
  // The agent's address: its task's ID for the main slot, <task>_<slot>
  // for a named slot (a reviewer). Every agent route and message takes it.
  task_id: string
  // The task the agent's slot belongs to, and the slot ("main", "reviewer").
  card_task_id?: string
  slot?: string
  execution_id?: string
  attribution_task_id?: string
  execution_kind?: string
  workflow_run_id?: string
  /** The card a workflow run is for, on an agent working on the run's own task (its step's agent, its reviewer). */
  run_card?: { task_id: string; key: string; title: string }
  /** "reeve" when the agent's glance is its wait on the Reeve's answer (working; needs_you once unanswered), since waiting_since. */
  waiting_on?: string
  waiting_since?: string
  task_title: string
  tmux_session: string
  command: string
  agent_kind: string
  account?: string
  account_label?: string
  // The engram the agent runs as; absent for none.
  engram?: string
  // The group the agent's task is in; absent for none.
  group?: string
  status: string
  activity: string
  // The agent's open operator items (for_operator): with any, it needs the
  // operator whatever its activity says.
  open_items?: number
  // Where the agent's work stands (exploring, planning, implementing,
  // reviewing, finished), why, what set it (start, operator, agent,
  // milestone, evidence) and since when; absent when none is recorded.
  stage?: string
  stage_reason?: string
  stage_source?: string
  stage_since?: string
  // The group the server sorts the agent into: needs_you, your_turn,
  // working, finished or ended.
  glance?: string
  // Its task has a plan proposed to the operator (a Legato plan or a swarm
  // plan pending), and how many of its open items are done notes.
  plan_pending?: boolean
  open_done_items?: number
  role?: string
  parent_task_id?: string
  subtask_id?: string
  started_at: string
  ended_at?: string
  working_seconds: number
  waiting_seconds: number
  error_seconds: number
  state_timeline?: string[]
  notify_enabled?: boolean
  has_chimera_endpoint?: boolean
  // The agent runs in Legato's sandbox.
  sandboxed?: boolean
  // The sandbox is on and this agent opted out of it.
  unsandboxed?: boolean
  // Its containers may use the host's network through its filtered Docker
  // socket (agents.sandbox.docker).
  docker_host_network?: boolean
  // The last known fill of a native agent's context window; changes arrive
  // as agent_context messages (useAgentContexts).
  context_used?: number
  context_window?: number
  // The agent of this Legato itself, pinned first.
  reeve?: boolean
  // What the agent started that still runs and will wake it; absent for
  // none. See docs/ai/agent-states.md.
  background_jobs?: BackgroundJob[]
  // The background shells its mod reported, running and ended (Chimera's);
  // absent for none. A Claude Code agent's come from its native endpoint.
  background_shells?: StoredBackgroundShell[]
  // The directory the agent was started in: its execution's working
  // directory (a folder, a worktree or a job clone); absent when none is
  // recorded.
  working_dir?: string
  // The job clone at working_dir, when it is one: its repository and the
  // branch it was made on.
  clone?: { repo: string; branch?: string }
}

export interface BackgroundJob {
  kind: "bash" | "monitor" | "subagent" | "cron" | string
  id: string
  label: string
  // A cron's schedule.
  schedule?: string
  // The subagent that started it; absent for the agent itself, and a parent
  // that is not listed is the agent's too.
  parent?: string
  // A subagent's: working, waiting (on the operator), or background (its
  // turn ended and work it started still runs).
  state?: "working" | "waiting" | "background" | string
  started_at: string
}

export interface PromptState {
  // menu: any other list of options the tool is showing in its terminal.
  type: "tool_approval" | "plan_approval" | "question" | "menu" | "free_text" | "working"
  context?: string
  actions?: PromptAction[]
}

// One answer a terminal prompt offers: the keys that choose it. A row that
// takes text (Claude Code's "Type something.") is answered by typing on it;
// text_submits says Enter then answers with the text, which in a
// multi-select would untick the row instead.
export interface PromptAction {
  label: string
  keys: string
  takes_text?: boolean
  text_submits?: boolean
}
