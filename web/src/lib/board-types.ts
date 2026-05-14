export interface BoardResponse {
  columns: BoardColumn[]
  workspaces: Workspace[]
}

export interface BoardColumn {
  name: string
  cards: BoardCard[]
}

export interface BoardCard {
  id: string
  title: string
  priority: string
  issue_type: string
  status: string
  provider: string
  has_warning: boolean
  agent_active: boolean
  agent_state: string
  working_seconds: number
  waiting_seconds: number
  workspace_name: string
  workspace_color: string
  pr_check_status: string
  pr_review_decision: string
  pr_comment_count: number
  pr_is_draft: boolean
  pr_number: number
  swarm_stats?: SwarmStats
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

export interface CardDetail {
  id: string
  title: string
  description_md: string
  status: string
  priority: string
  provider: string
  remote_id: string
  remote_meta: Record<string, string>
  workspace_id: number | null
  pr_meta: PRMetaView | null
  swarm_active_step: number
  swarm_step_names: string[]
  created_at: string
  updated_at: string
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
}

export interface RemoteSearchResult {
  id: string
  summary: string
  status: string
  priority: string
  issue_type: string
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
