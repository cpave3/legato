// The agents' graph (LEG-537): every running agent as a node in a box for
// its repository, its sub-agents branching off it, and the links between
// agents that coordinate. This file is the layout alone, with no drawing:
// positions come from the order of the data, never from a simulation, so an
// agent keeps its place while others come and go.
import type { AgentInfo, BackgroundJob } from "../hooks/useWebSocket"

// AgentGraphData is GET /api/agents/graph: what the agent list does not say.
export interface AgentGraphData {
  // Each running agent's place, by agent ID.
  agents: Record<string, AgentPlace>
  links: AgentLinkData[]
  // How long a peer message keeps its link.
  peer_window_seconds: number
}

export interface AgentPlace {
  // owner/name, else the folder name of its worktree; absent outside Git.
  repo?: string
  worktree?: string
  badges?: AgentBadge[]
}

export interface AgentBadge {
  kind: "handoff" | "delegation" | string
  label: string
  detail?: string
}

export interface AgentLinkData {
  kind: "peer_message" | "review_loop" | "reeve_watch" | string
  from: string
  to: string
  count?: number
  last_at?: string
  label?: string
}

export type GraphEdgeKind = "subagent" | "swarm" | "reviewer" | "review_loop" | "reeve_watch" | "peer_message" | "worktree"

// Every kind of link between agents, in the order the legend names them.
export const linkKinds: GraphEdgeKind[] = ["subagent", "swarm", "reviewer", "review_loop", "reeve_watch", "peer_message", "worktree"]

export const linkLabels: Record<GraphEdgeKind, string> = {
  subagent: "sub-agent",
  swarm: "swarm",
  reviewer: "reviewer",
  review_loop: "review run",
  reeve_watch: "Reeve watches",
  peer_message: "messaged",
  worktree: "same worktree",
}

export interface LayoutOptions {
  // The time now, in milliseconds, to age peer messages by.
  now: number
  // LR on wide screens: sub-agents to the right. TB at phone width: each
  // node below the last, sub-agents indented.
  direction: "LR" | "TB"
  // The nodes (an agent's ID, or a sub-agent's node ID) whose folded
  // sub-agents are shown.
  unfolded?: ReadonlySet<string>
  // The boxes (by box ID) shown as their header alone.
  collapsed?: ReadonlySet<string>
  // Left to right, boxes sit side by side in rows this wide at most.
  maxWidth?: number
}

export interface GraphBox {
  id: string
  label: string
  x: number
  y: number
  width: number
  height: number
  // How many agents it holds, not counting sub-agents.
  agents: number
  collapsed: boolean
}

export interface GraphNode {
  id: string
  kind: "agent" | "subagent" | "fold" | "reeve"
  // The agent the node is, or whose sub-agent it is.
  agentId: string
  box?: string
  x: number
  y: number
  width: number
  height: number
  depth: number
  agent?: AgentInfo
  job?: BackgroundJob
  // A sub-agent's state: working, waiting, background.
  state?: string
  // A fold's count of the sub-agents it hides.
  hidden?: number
  badges?: AgentBadge[]
}

export interface GraphEdge {
  id: string
  kind: GraphEdgeKind
  source: string
  target: string
  label?: string
  count?: number
  // 1 for a live link; a peer message fades as it ages.
  opacity: number
}

export interface AgentGraphLayout {
  boxes: GraphBox[]
  nodes: GraphNode[]
  edges: GraphEdge[]
}

// Sizes are fixed, so the layout never waits for the nodes to be measured.
export const sizes = {
  agent: { width: 232, height: 62 },
  sub: { width: 176, height: 34 },
  reeve: { width: 132, height: 44 },
} as const

const BOX_PAD = 16
const BOX_HEAD = 32
const BOX_GAP = 28
const COL_GAP = 44
const AGENT_ROW = sizes.agent.height + 14
const SUB_ROW = sizes.sub.height + 10
const TB_INDENT = 26
// A sub-agent sits level with the middle of its agent's row.
const SUB_OFFSET = (sizes.agent.height - sizes.sub.height) / 2
const REEVE_GAP = 72
// How wide a row of boxes may be, left to right.
const ROW_WIDTH = 1500
// How many sub-agents of one node are shown before the rest fold.
export const FOLD_AFTER = 3

// boxID is the ID of the box for a repository ("" for agents outside Git).
export function boxID(repo: string): string {
  return `box:${repo}`
}

// A sub-agent as a tree, before it is placed.
interface SubTree {
  id: string
  job?: BackgroundJob
  fold?: number
  children: SubTree[]
}

// layoutAgentGraph places agents (already filtered to those shown) with what
// the server said of them (null while it loads).
export function layoutAgentGraph(agents: AgentInfo[], data: AgentGraphData | null, options: LayoutOptions): AgentGraphLayout {
  const places = data?.agents ?? {}
  const collapsed = options.collapsed ?? new Set<string>()
  const unfolded = options.unfolded ?? new Set<string>()
  const repoOf = (agent: AgentInfo) => places[agent.task_id]?.repo || agent.clone?.repo || ""

  const ordered = [...agents].sort(byStart)
  const reeves = ordered.filter((agent) => agent.reeve)
  const groups = new Map<string, AgentInfo[]>()
  for (const agent of ordered) {
    if (agent.reeve) continue
    const repo = repoOf(agent)
    groups.set(repo, [...(groups.get(repo) ?? []), agent])
  }
  const repos = [...groups.keys()].sort((a, b) => (a === "") === (b === "") ? a.localeCompare(b) : a === "" ? 1 : -1)

  const boxes: GraphBox[] = []
  const nodes: GraphNode[] = []
  const edges: GraphEdge[] = []
  // Where each agent is drawn: its own node, or its collapsed box.
  const shownAs = new Map<string, string>()

  // The Reeve stands apart, before the boxes, where it never moves them.
  reeves.forEach((reeve, i) => {
    const at = options.direction === "LR"
      ? { x: -(sizes.reeve.width + REEVE_GAP), y: i * (sizes.reeve.height + 16) }
      : { x: i * (sizes.reeve.width + 16), y: -(sizes.reeve.height + REEVE_GAP / 2) }
    nodes.push({ id: reeve.task_id, kind: "reeve", agentId: reeve.task_id, ...at, ...sizes.reeve, depth: 0, agent: reeve, badges: places[reeve.task_id]?.badges })
    shownAs.set(reeve.task_id, reeve.task_id)
  })

  // Left to right, boxes fill rows as words fill lines; top to bottom,
  // each is under the last.
  const wrap = options.direction === "LR" ? options.maxWidth ?? ROW_WIDTH : 0
  let shelfX = 0, shelfY = 0, shelfHeight = 0
  const place = (box: GraphBox, first: number) => {
    if (wrap && shelfX > 0 && shelfX + box.width > wrap) {
      shelfX = 0
      shelfY += shelfHeight + BOX_GAP
      shelfHeight = 0
    }
    box.x = shelfX
    box.y = shelfY
    for (let i = first; i < nodes.length; i++) {
      nodes[i].x += box.x
      nodes[i].y += box.y
    }
    if (wrap) {
      shelfX += box.width + BOX_GAP
      shelfHeight = Math.max(shelfHeight, box.height)
    } else {
      shelfY += box.height + BOX_GAP
    }
  }
  for (const repo of repos) {
    // A box is laid out at the origin, then moved to its place.
    const top = 0
    const first = nodes.length
    const members = groups.get(repo)!
    const id = boxID(repo)
    const box: GraphBox = { id, label: repo || "No repository", x: 0, y: top, width: sizes.agent.width + 2 * BOX_PAD, height: BOX_HEAD, agents: members.length, collapsed: collapsed.has(id) }
    boxes.push(box)
    if (box.collapsed) {
      box.width = Math.max(box.width, 220)
      for (const agent of members) shownAs.set(agent.task_id, id)
      place(box, first)
      continue
    }
    let y = top + BOX_HEAD
    let right = 0
    for (const agent of members) {
      shownAs.set(agent.task_id, agent.task_id)
      const tree = subTrees(agent, unfolded)
      const x = BOX_PAD
      nodes.push({ id: agent.task_id, kind: "agent", agentId: agent.task_id, box: id, x, y, ...sizes.agent, depth: 0, agent, badges: places[agent.task_id]?.badges })
      right = Math.max(right, x + sizes.agent.width)
      if (options.direction === "LR") {
        // Each leaf of the agent's tree takes a row; the agent sits at the
        // top of its rows.
        let row = y
        const place = (parent: string, trees: SubTree[], depth: number) => {
          for (const tree of trees) {
            const nodeX = x + sizes.agent.width + COL_GAP + (depth - 1) * (sizes.sub.width + COL_GAP)
            nodes.push(subNode(agent, tree, id, nodeX, row + SUB_OFFSET, depth))
            edges.push(treeEdge(parent, tree.id))
            right = Math.max(right, nodeX + sizes.sub.width)
            if (tree.children.length > 0) place(tree.id, tree.children, depth + 1)
            else row += SUB_ROW
          }
        }
        place(agent.task_id, tree, 1)
        // The next agent starts below this one and below its last sub-agent.
        y = Math.max(y + AGENT_ROW, row + AGENT_ROW - SUB_ROW)
      } else {
        y += AGENT_ROW
        const place = (parent: string, trees: SubTree[], depth: number) => {
          for (const tree of trees) {
            const nodeX = x + depth * TB_INDENT
            nodes.push(subNode(agent, tree, id, nodeX, y, depth))
            edges.push(treeEdge(parent, tree.id))
            right = Math.max(right, nodeX + sizes.sub.width)
            y += SUB_ROW
            place(tree.id, tree.children, depth + 1)
          }
        }
        place(agent.task_id, tree, 1)
      }
    }
    box.width = Math.max(box.width, right + BOX_PAD)
    box.height = y - top + BOX_PAD - 14
    place(box, first)
  }

  // Links between agents, each drawn between what shows them.
  const seen = new Set(edges.map((edge) => edge.id))
  const linked = new Set<string>()
  const link = (kind: GraphEdgeKind, from: string, to: string, extra: Partial<GraphEdge> = {}) => {
    const source = shownAs.get(from)
    const target = shownAs.get(to)
    if (!source || !target || source === target) return
    linked.add(pairKey(from, to))
    const id = `${kind}:${source}->${target}`
    if (seen.has(id)) return
    seen.add(id)
    edges.push({ id, kind, source, target, opacity: 1, ...extra })
  }

  const byID = new Map(agents.map((agent) => [agent.task_id, agent]))
  for (const agent of ordered) {
    if (agent.parent_task_id && agent.parent_task_id !== agent.task_id && byID.get(agent.parent_task_id)?.role === "conductor") {
      link("swarm", agent.parent_task_id, agent.task_id)
    }
    if (agent.slot && agent.slot !== "main" && agent.card_task_id) link("reviewer", agent.task_id, agent.card_task_id)
  }
  const window = (data?.peer_window_seconds ?? 1800) * 1000
  for (const raw of data?.links ?? []) {
    if (raw.kind === "peer_message") {
      const age = options.now - Date.parse(raw.last_at ?? "")
      if (!(age < window)) continue
      link("peer_message", raw.from, raw.to, { count: raw.count, opacity: 1 - 0.75 * Math.max(0, age) / window })
    } else if (raw.kind === "review_loop" || raw.kind === "reeve_watch") {
      link(raw.kind, raw.from, raw.to, raw.label ? { label: raw.label } : {})
    }
  }
  // Agents in one worktree can message each other: a faint chain between
  // those nothing else links.
  const worktrees = new Map<string, string[]>()
  for (const agent of ordered) {
    const worktree = places[agent.task_id]?.worktree
    if (worktree && !agent.reeve) worktrees.set(worktree, [...(worktrees.get(worktree) ?? []), agent.task_id])
  }
  for (const members of worktrees.values()) {
    for (let i = 1; i < members.length; i++) {
      if (!linked.has(pairKey(members[i - 1], members[i]))) link("worktree", members[i - 1], members[i], { opacity: 0.6 })
    }
  }
  return { boxes, nodes, edges }
}

// subTrees is an agent's sub-agents as trees, each node showing at most
// FOLD_AFTER sub-agents until it is unfolded; the rest are one fold.
function subTrees(agent: AgentInfo, unfolded: ReadonlySet<string>): SubTree[] {
  const jobs = (agent.background_jobs ?? []).filter((job) => job.kind === "subagent")
  const ids = new Set(jobs.map((job) => job.id))
  const parentOf = (job: BackgroundJob) => job.parent && ids.has(job.parent) && job.parent !== job.id ? job.parent : ""
  const seen = new Set<string>()
  const build = (parentJob: string, parentNode: string): SubTree[] => {
    const children = jobs.filter((job) => parentOf(job) === parentJob && !seen.has(job.id))
    children.forEach((job) => seen.add(job.id))
    const trees = children.map((job) => {
      const id = `${agent.task_id}/${job.id}`
      return { id, job, children: build(job.id, id) }
    })
    if (trees.length <= FOLD_AFTER || unfolded.has(parentNode)) return trees
    const hidden = trees.slice(FOLD_AFTER).reduce((n, tree) => n + count(tree), 0)
    return [...trees.slice(0, FOLD_AFTER), { id: `${parentNode}/+`, fold: hidden, children: [] }]
  }
  return build("", agent.task_id)
}

function count(tree: SubTree): number {
  return 1 + tree.children.reduce((n, child) => n + count(child), 0)
}

function subNode(agent: AgentInfo, tree: SubTree, box: string, x: number, y: number, depth: number): GraphNode {
  if (tree.fold !== undefined) return { id: tree.id, kind: "fold", agentId: agent.task_id, box, x, y, ...sizes.sub, depth, hidden: tree.fold }
  return { id: tree.id, kind: "subagent", agentId: agent.task_id, box, x, y, ...sizes.sub, depth, job: tree.job, state: tree.job?.state }
}

function treeEdge(source: string, target: string): GraphEdge {
  return { id: `subagent:${source}->${target}`, kind: "subagent", source, target, opacity: 1 }
}

function pairKey(a: string, b: string): string {
  return a < b ? `${a}|${b}` : `${b}|${a}`
}

// byStart orders agents by when they started, then by ID, so the order
// never changes while they run.
function byStart(a: AgentInfo, b: AgentInfo): number {
  const at = Date.parse(a.started_at)
  const bt = Date.parse(b.started_at)
  if (!Number.isNaN(at) && !Number.isNaN(bt) && at !== bt) return at - bt
  return a.task_id.localeCompare(b.task_id)
}

// linkLines says, in words, how an agent is linked to the others.
export function linkLines(id: string, edges: GraphEdge[], agents: AgentInfo[]): string[] {
  const name = (other: string) => agents.find((agent) => agent.task_id === other)?.reeve ? "the Reeve" : other
  const out: string[] = []
  const workers = edges.filter((edge) => edge.kind === "swarm" && edge.source === id).length
  if (workers > 0) out.push(`conducts ${workers} ${workers === 1 ? "worker" : "workers"}`)
  for (const edge of edges) {
    if (edge.kind === "subagent" || (edge.source !== id && edge.target !== id)) continue
    const outgoing = edge.source === id
    const other = name(outgoing ? edge.target : edge.source)
    switch (edge.kind) {
      case "swarm":
        if (!outgoing) out.push(`works for ${other}`)
        break
      case "reviewer":
        out.push(outgoing ? `reviews ${other}` : `reviewed by ${other}`)
        break
      case "review_loop":
        out.push(`${outgoing ? `reviews ${other}'s pull request` : `${other} reviews its pull request`}${edge.label ? ` (${edge.label})` : ""}`)
        break
      case "reeve_watch":
        out.push(outgoing ? `watches ${other}` : "watched by the Reeve")
        break
      case "peer_message":
        out.push(outgoing ? `messaged ${other} ${edge.count ?? 1}×` : `${other} messaged it ${edge.count ?? 1}×`)
        break
      case "worktree":
        out.push(`shares a worktree with ${other}`)
        break
    }
  }
  return out
}
