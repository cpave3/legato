export interface BoardResponse {
  columns: BoardColumn[]
  workspaces: Workspace[]
  // The workers this main is paired with; absent from older servers.
  workers?: BoardWorker[]
}

export interface BoardColumn {
  name: string
  cards: BoardCard[]
  // Set on the Away lane the web board adds after the columns: the tasks
  // away on workers, which the server leaves in the columns they left.
  away?: boolean
}

// A worker this main is paired with: a group of the Away lane.
export interface BoardWorker {
  instance_id: string
  name: string
  address: string
  // When this main last heard from the worker on its connection.
  last_pull_at?: string
  // When it went down, once down past the grace; when it stopped
  // accepting this main (pair again).
  unreachable_since?: string
  refused_at?: string
}

export interface BoardCard {
  id: string
  title: string
  priority: string
  issue_type: string
  status: string
  provider: string
  has_warning: boolean
  has_worktree?: boolean
  agent_active: boolean
  agent_state: string
  agent_kind?: string
  // The task's running named slots (a reviewer), shown as chips beside the
  // main slot's state; absent when none.
  slot_agents?: SlotAgent[]
  working_seconds: number
  waiting_seconds: number
  error_seconds: number
  workspace_id: number | null
  workspace_name: string
  workspace_color: string
  // The task's pull requests in stack order, bottom first; absent when none.
  pull_requests?: PRMetaView[]
  review_tour_id?: string
  review_ready?: boolean
  review_unreviewed?: number
  swarm_stats?: SwarmStats
  // On a worker: the main instance that handed this card to it.
  handoff_from?: HandoffFrom
  // On a main: the worker this task is away on.
  away?: AwayOn
}

export interface SlotAgent {
  slot: string
  state: string
}

// A handoff's review passes on its worker, as the worker last said.
export interface AwayReview {
  pass: number
  // reviewing, waiting_for_reviewer, waiting_for_author, clean or stuck
  phase: string
  blocking_open: number
  comments_open: number
  checks_open: number
  // Counted checks not finished on the reviewed heads, and failing ones the
  // author answered by a reply.
  checks_pending?: number
  checks_replied?: number
  skipped_comments: number
  reason?: string
  // Each pull request's latest review by Legato.
  links?: { pr: number; url: string }[]
}

export interface HandoffFrom {
  main: string
  main_task_id: string
}

// A task away on a worker: where it went, how, the column it left, the
// worker's state of it as last heard, and the address that opens it there.
export interface AwayOn {
  worker_instance_id: string
  worker: string
  handoff_id: string
  worker_task_id: string
  // make-change or find-out
  purpose: string
  // new-branch or pushed-branch
  kind: string
  repo: string
  branch: string
  agent_kind?: string
  left_column: string
  handed_off_at: string
  // pending, in_progress, done, failed or returning; absent when nothing
  // was heard.
  state?: string
  failure?: string
  state_at?: string
  url?: string
  // What the worker's live connection says: its agent's activity
  // (working, waiting, idle, ended), the card's column and title there, the
  // branches pushed in stack order.
  activity?: string
  worker_column?: string
  worker_title?: string
  branches?: string[]
  // Why the worker no longer has the task, since when.
  lost?: string
  lost_at?: string
  worker_unreachable_since?: string
  worker_refused_at?: string
  // What the main shows: starting, working, needs_you, idle, ended, done,
  // failed, coming_back, unreachable, lost or not_accepted.
  view?: string
  // Review passes on the worker; absent from a worker without them.
  review?: AwayReview
}

export interface SwarmStats {
  total: number
  done: number
  in_review: number
  building: number
  queued: number
  rejected: number
}

export interface Workspace {
  id: number
  name: string
  color: string
}

export interface TaskWorktree {
  primary_dir: string
  path: string
  branch: string
  base_branch: string
}

export interface CardDetail {
  id: string
  title: string
  description_md: string
  local_description_md: string
  remote_description_md: string
  status: string
  priority: string
  provider: string
  remote_id: string
  remote_meta: Record<string, string>
  workspace_id: number | null
  // The first pull request in stack order, or how discovery last ended.
  pr_meta: PRMetaView | null
  // Every pull request of the task, in stack order.
  pull_requests?: PRMetaView[]
  // The branches the task tracks, in stack order, each with its PR once found.
  tracked_branches?: TrackedBranch[]
  code_context: CodeContextView | null
  worktree?: TaskWorktree
  has_chimera_session: boolean
  resumable_agent_kinds: string[]
  swarm_active_step: number
  swarm_step_names: string[]
  swarm_stats?: SwarmStats
  swarm_working_dir?: string
  created_at: string
  updated_at: string
  notes?: { id: string; task_id: string; body: string; source: string; remote_id?: string; created_at: string }[]
}

export interface CodeContextView {
  repo: string
  branch: string
  head_sha: string
  working_dir: string
  base_branch: string
  repo_source: string
  branch_source: string
}

export interface TrackedBranch {
  repo: string
  branch: string
  // code_context, stack, api, manual or discovered
  source: string
  pr?: PRMetaView
}

// The pull requests of a task's detail, in stack order: from a server that
// lists them, else the single one it names.
export function taskPullRequests(card: Pick<CardDetail, "pr_meta" | "pull_requests">): PRMetaView[] {
  if (card.pull_requests && card.pull_requests.length > 0) return card.pull_requests
  return card.pr_meta && card.pr_meta.pr_number > 0 ? [card.pr_meta] : []
}

export interface PRMetaView {
  repo: string
  branch: string
  pr_number: number
  pr_url: string
  state: string
  is_draft: boolean
  review_decision: string
  check_status: string
  comment_count: number
  pinned: boolean
  match_method: string
  sync_outcome: string
  sync_error: string
}

export interface RemoteSearchResult {
  id: string
  summary: string
  status: string
  priority: string
  issue_type: string
  // Where the ticket is: "jira", "burnlite", ... Absent from older servers.
  provider?: string
}

export interface RepoDetectResponse {
  owner: string
  repo: string
}

export interface PRPreviewResponse {
  number: number
  url: string
  state: string
  is_draft: boolean
  check_status: string
  review_decision: string
  comment_count: number
}

export interface ArchiveDoneResponse {
  archived: number
}
