// The agents' graph (LEG-537): every running agent as a node in a box for
// its repository, its sub-agents branching off it, and the links between
// agents that coordinate. This file is the layout alone, with no drawing:
// positions come from the order of the data, never from a simulation, and
// every line's path is worked out here too, so it runs around boxes rather
// than through them (LEG-615). With the memory one layout leaves the next,
// an agent keeps its place while others come and go, and a change of state
// moves nothing.
import type { AgentInfo, BackgroundJob } from "../hooks/useWebSocket"
import { subagentTree, subagentWaitsOn } from "./agentGlance"

// AgentGraphData is GET /api/agents/graph: what the agent list does not say.
export interface AgentGraphData {
  // Each running agent's place, by agent ID.
  agents: Record<string, AgentPlace>
  links: AgentLinkData[]
  // How long a peer message keeps its link.
  peer_window_seconds: number
  // Each active workflow run an agent works for, by run ID.
  runs?: Record<string, GraphRun>
}

// GraphRun is an active workflow run as the graph shows it, in the shape of
// the runs list's runs.
export interface GraphRun {
  id: string
  workflow_name: string
  workflow_title: string
  status: string
  // The top-level step the run is at.
  current_step_id: string
  needs_attention?: boolean
  attention_reason?: string
  steps: { step_id: string; title: string; status: string; trigger?: string }[]
  // The review loop's pass, while the run is at its loop.
  pass?: number
  created_at: string
}

// AgentWorkflowPlace is an agent's place in a run: its top-level step (node
// "step:<step_id>" of the run's workflow graph) and its role there.
export interface AgentWorkflowPlace {
  run_id: string
  step_id: string
  role: "agent" | "interview" | "reviewer" | "author" | "awaited" | string
}

// NodeWorkflow is a place of a node's agent with its run.
export interface NodeWorkflow {
  place: AgentWorkflowPlace
  run: GraphRun
}

export interface AgentPlace {
  // owner/name, else the folder name of its worktree; absent outside Git.
  repo?: string
  worktree?: string
  badges?: AgentBadge[]
  // The active runs it works for, the one that needs the operator first.
  workflows?: AgentWorkflowPlace[]
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

export type GraphEdgeKind = "subagent" | "swarm" | "reviewer" | "slot" | "review_loop" | "workflow" | "reeve_watch" | "peer_message" | "worktree"

// Every kind of link between agents, in the order the legend names them.
export const linkKinds: GraphEdgeKind[] = ["subagent", "swarm", "reviewer", "slot", "review_loop", "workflow", "reeve_watch", "peer_message", "worktree"]

export const linkLabels: Record<GraphEdgeKind, string> = {
  subagent: "sub-agent",
  swarm: "swarm",
  reviewer: "reviewer",
  slot: "card slot",
  review_loop: "review run",
  workflow: "same run",
  reeve_watch: "Reeve watches",
  peer_message: "messaged",
  worktree: "same worktree",
}

export interface LayoutOptions {
  // The time now, in milliseconds, to age peer messages and departed agents
  // by.
  now: number
  // LR on wide screens: boxes side by side in rows. TB at phone width: one
  // box under another.
  direction: "LR" | "TB"
  // The nodes (an agent's ID, or a sub-agent's node ID) whose folded
  // sub-agents are shown.
  unfolded?: ReadonlySet<string>
  // The boxes (by box ID) shown as their header alone.
  collapsed?: ReadonlySet<string>
  // Left to right, boxes sit side by side in rows this wide at most.
  maxWidth?: number
  // What the last layout left (its memory), so this one keeps its order:
  // boxes and agents stay where they were and newcomers slot in.
  memory?: GraphMemory
  // Agents the graph's last read did not know yet, left out until the next
  // read says where they work.
  pending?: ReadonlySet<string>
}

export interface Point {
  x: number
  y: number
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
  // What a sub-agent in the background waits on: "waiting on 3 shells".
  waitsOn?: string
  // A fold's count of the sub-agents it hides.
  hidden?: number
  badges?: AgentBadge[]
  // An agent's workflow runs, the one to show first first.
  workflows?: NodeWorkflow[]
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
  // The line from source to target, in straight runs across and down.
  points: Point[]
  // Where its label goes: the middle of its longest run.
  labelAt: Point
}

// GraphMemory is what one layout leaves for the next (LEG-615), so the graph
// changes only when its structure does: the order of the boxes and of each
// box's agents, the place of an agent that left (held for GONE_GRACE_MS),
// and the tallest each row of boxes has been.
export interface GraphMemory {
  boxes: string[]
  // Each box's agents in order, those that left and are still held among
  // them.
  slots: Record<string, string[]>
  // The box each agent was last in.
  boxOf: Record<string, string>
  // When each held agent was first missed, in milliseconds.
  gone: Record<string, number>
  rows: number[]
}

export interface AgentGraphLayout {
  boxes: GraphBox[]
  nodes: GraphNode[]
  edges: GraphEdge[]
  memory: GraphMemory
}

// How long the place of an agent that left is kept empty before its box
// closes up.
export const GONE_GRACE_MS = 60_000

// compactGraphMemory is a memory with the places of agents that left let
// go and the rows as tall as they are now: what Fit the graph tidies.
export function compactGraphMemory(memory: GraphMemory): GraphMemory {
  const gone = new Set(Object.keys(memory.gone))
  const slots = Object.fromEntries(Object.entries(memory.slots).map(([box, ids]) => [box, ids.filter((id) => !gone.has(id))]).filter(([, ids]) => ids.length > 0))
  const boxOf = Object.fromEntries(Object.entries(memory.boxOf).filter(([id]) => !gone.has(id)))
  return { boxes: memory.boxes.filter((box) => box in slots), slots, boxOf, gone: {}, rows: [] }
}

// Sizes are fixed, so the layout never waits for the nodes to be measured.
export const sizes = {
  // Tall enough for a run's step strip along the foot.
  agent: { width: 232, height: 70 },
  sub: { width: 176, height: 34 },
  reeve: { width: 132, height: 44 },
} as const

// A box is a column of agents with a lane each side: the left one for links
// between its agents, the right one for lines from outside, which come in
// through its top edge. Lines between boxes run in the band above each row
// of boxes (the first under the Reeve) and in the gaps between columns.
const AGENT_X = 36
const RIGHT_LANE = 32
const BOX_WIDTH = AGENT_X + sizes.agent.width + RIGHT_LANE
const BOX_PAD = 16
const BOX_HEAD = 32
const COL_GAP = 40
const ROW_GAP = 56
const REEVE_BAND = 64
const REEVE_GAP = 16
// How far apart parallel lines run.
const TRACK = 6
const AGENT_ROW = sizes.agent.height + 14
const SUB_ROW = sizes.sub.height + 10
const INDENT = 26
// How wide a row of boxes may be, left to right.
const ROW_WIDTH = 1500
// How many sub-agents of one node are shown before the rest fold.
export const FOLD_AFTER = 3

// boxID is the ID of the box for a repository ("" for agents outside Git).
export function boxID(repo: string): string {
  return `box:${repo}`
}

const repoOfBox = (box: string) => box.slice("box:".length)

// A sub-agent as a tree, before it is placed.
interface SubTree {
  id: string
  job?: BackgroundJob
  fold?: number
  children: SubTree[]
}

// A link between two agents, before it is drawn between what shows them.
interface Relation {
  kind: GraphEdgeKind
  from: string
  to: string
  extra: Partial<GraphEdge>
}

// layoutAgentGraph places agents with what the server said of them (null
// while it loads).
export function layoutAgentGraph(agents: AgentInfo[], data: AgentGraphData | null, options: LayoutOptions): AgentGraphLayout {
  const places = data?.agents ?? {}
  const runs = data?.runs ?? {}
  const workflowsOf = (id: string): NodeWorkflow[] | undefined => {
    const found = (places[id]?.workflows ?? []).flatMap((place) => runs[place.run_id] ? [{ place, run: runs[place.run_id] }] : [])
    return found.length > 0 ? found : undefined
  }
  const collapsed = options.collapsed ?? new Set<string>()
  const unfolded = options.unfolded ?? new Set<string>()
  const pending = options.pending ?? new Set<string>()
  const memory = options.memory ?? { boxes: [], slots: {}, boxOf: {}, gone: {}, rows: [] }

  const ordered = agents.filter((agent) => !pending.has(agent.task_id)).sort(byStart)
  const byID = new Map(ordered.map((agent) => [agent.task_id, agent]))
  const reeves = ordered.filter((agent) => agent.reeve)
  // An agent the read no longer places stays in the box it was in.
  const boxOfAgent = new Map<string, string>()
  for (const agent of ordered) {
    if (agent.reeve) continue
    const place = places[agent.task_id]
    const remembered = !place && !agent.clone?.repo ? memory.boxOf[agent.task_id] : undefined
    boxOfAgent.set(agent.task_id, remembered ?? boxID(place?.repo || agent.clone?.repo || ""))
  }

  const relations = relationsOf(ordered, data, places, runs, options.now)
  const order = orderer(relations, ordered, boxOfAgent)

  // Each box's agents: those it had keep their places, an agent that left
  // keeps its place empty for a while, and a newcomer goes after the agent
  // it works with, else last.
  const gone: Record<string, number> = {}
  const live = new Map<string, string[]>()
  for (const [id, box] of boxOfAgent) live.set(box, [...(live.get(box) ?? []), id])
  const slots = new Map<string, string[]>()
  for (const box of new Set([...memory.boxes, ...Object.keys(memory.slots), ...live.keys()])) {
    const kept: string[] = []
    for (const id of memory.slots[box] ?? []) {
      if (boxOfAgent.get(id) === box) kept.push(id)
      else if (!byID.has(id)) {
        const since = memory.gone[id] ?? options.now
        if (options.now - since < GONE_GRACE_MS) {
          kept.push(id)
          gone[id] = since
        }
      }
    }
    const members = order.members(live.get(box) ?? [])
    const list = kept.length === 0 ? members : kept
    if (kept.length > 0) for (const id of members) if (!kept.includes(id)) order.insert(list, id)
    if (list.length > 0) slots.set(box, list)
  }
  const kept = memory.boxes.filter((box) => slots.has(box))
  const boxOrder = [...kept, ...[...slots.keys()].filter((box) => !kept.includes(box)).sort(byBoxName)]
  const outside = boxOrder.indexOf(boxID(""))
  if (outside >= 0) boxOrder.push(...boxOrder.splice(outside, 1))

  const boxes: GraphBox[] = []
  const nodes: GraphNode[] = []
  const edges: GraphEdge[] = []
  // Where each agent is drawn: its own node, or its collapsed box.
  const shownAs = new Map<string, string>()
  const reeveNodes = reeves.map((reeve): GraphNode => {
    shownAs.set(reeve.task_id, reeve.task_id)
    return { id: reeve.task_id, kind: "reeve", agentId: reeve.task_id, x: 0, y: 0, ...sizes.reeve, depth: 0, agent: reeve, badges: places[reeve.task_id]?.badges }
  })
  nodes.push(...reeveNodes)

  // Each box is laid out at the origin, then moved to its place.
  const treeLinks: [string, string][] = []
  const spans: { box: GraphBox; first: number }[] = []
  for (const id of boxOrder) {
    const list = slots.get(id)!
    const first = nodes.length
    const box: GraphBox = { id, label: repoOfBox(id) || "No repository", x: 0, y: 0, width: BOX_WIDTH, height: BOX_HEAD, agents: list.filter((agent) => byID.has(agent)).length, collapsed: collapsed.has(id) }
    boxes.push(box)
    spans.push({ box, first })
    if (box.collapsed) {
      for (const agent of list) if (byID.has(agent)) shownAs.set(agent, id)
      continue
    }
    let y = BOX_HEAD
    for (const agentID of list) {
      const agent = byID.get(agentID)
      if (!agent) {
        y += AGENT_ROW
        continue
      }
      shownAs.set(agentID, agentID)
      nodes.push({ id: agentID, kind: "agent", agentId: agentID, box: id, x: AGENT_X, y, ...sizes.agent, depth: 0, agent, badges: places[agentID]?.badges, workflows: workflowsOf(agentID) })
      y += AGENT_ROW
      // Sub-agents hang under their agent, each indented under its parent.
      const place = (parent: string, trees: SubTree[], depth: number) => {
        for (const tree of trees) {
          nodes.push(subNode(agent, tree, id, AGENT_X + depth * INDENT, y, depth))
          treeLinks.push([parent, tree.id])
          y += SUB_ROW
          place(tree.id, tree.children, depth + 1)
        }
      }
      place(agentID, subTrees(agent, unfolded), 1)
    }
    box.height = y + BOX_PAD - 14
  }

  // Boxes fill rows as words fill lines; top to bottom there is one column.
  const columns = options.direction === "LR" ? Math.max(1, Math.floor(((options.maxWidth ?? ROW_WIDTH) + COL_GAP) / (BOX_WIDTH + COL_GAP))) : 1
  const rowCount = Math.ceil(boxes.length / columns)
  const rows: number[] = []
  for (let row = 0; row < rowCount; row++) {
    const tallest = Math.max(...boxes.slice(row * columns, (row + 1) * columns).map((box) => box.height))
    rows.push(Math.max(memory.rows[row] ?? 0, tallest))
  }
  const rowTop: number[] = []
  rows.forEach((_, row) => rowTop.push(row === 0 ? 0 : rowTop[row - 1] + rows[row - 1] + ROW_GAP))
  spans.forEach(({ box, first }, i) => {
    box.x = (i % columns) * (BOX_WIDTH + COL_GAP)
    box.y = rowTop[Math.floor(i / columns)]
    const end = i + 1 < spans.length ? spans[i + 1].first : nodes.length
    for (let n = first; n < end; n++) {
      nodes[n].x += box.x
      nodes[n].y += box.y
    }
  })

  // The Reeve stands above the fleet, centred over its first row.
  const firstRow = Math.min(columns, boxes.length)
  const span = firstRow > 0 ? firstRow * BOX_WIDTH + (firstRow - 1) * COL_GAP : 0
  const reeveSpan = reeveNodes.length * sizes.reeve.width + Math.max(0, reeveNodes.length - 1) * REEVE_GAP
  reeveNodes.forEach((node, i) => {
    node.x = span / 2 - reeveSpan / 2 + i * (sizes.reeve.width + REEVE_GAP)
    node.y = -REEVE_BAND - sizes.reeve.height
  })

  // Sub-agent links, as a file tree: down from the parent, across to the
  // child.
  const nodeByID = new Map(nodes.map((node) => [node.id, node]))
  for (const [parentID, childID] of treeLinks) {
    const parent = nodeByID.get(parentID)!, node = nodeByID.get(childID)!
    const x = parent.x + 14
    const points = [{ x, y: parent.y + parent.height }, { x, y: node.y + node.height / 2 }, { x: node.x, y: node.y + node.height / 2 }]
    edges.push({ id: `subagent:${parent.id}->${node.id}`, kind: "subagent", source: parent.id, target: node.id, opacity: 1, points, labelAt: middle(points) })
  }

  // Links between agents, each drawn between what shows them.
  const links: GraphEdge[] = []
  const seen = new Set<string>()
  for (const relation of relations) {
    const source = shownAs.get(relation.from)
    const target = shownAs.get(relation.to)
    if (!source || !target || source === target) continue
    const id = `${relation.kind}:${source}->${target}`
    if (seen.has(id)) continue
    seen.add(id)
    links.push({ id, kind: relation.kind, source, target, opacity: 1, points: [], labelAt: { x: 0, y: 0 }, ...relation.extra })
  }
  route(links, { nodes: nodeByID, boxes, columns, rowTop })
  edges.push(...links)

  const boxOf: Record<string, string> = {}
  for (const [id, box] of boxOfAgent) boxOf[id] = box
  for (const id of Object.keys(gone)) if (memory.boxOf[id]) boxOf[id] = memory.boxOf[id]
  return {
    boxes, nodes, edges,
    memory: { boxes: boxOrder, slots: Object.fromEntries(slots), boxOf, gone, rows },
  }
}

// relationsOf is every link between the agents, before it is drawn.
function relationsOf(ordered: AgentInfo[], data: AgentGraphData | null, places: Record<string, AgentPlace>, runs: Record<string, GraphRun>, now: number): Relation[] {
  const present = new Set(ordered.map((agent) => agent.task_id))
  const byID = new Map(ordered.map((agent) => [agent.task_id, agent]))
  const out: Relation[] = []
  const linked = new Set<string>()
  const link = (kind: GraphEdgeKind, from: string, to: string, extra: Partial<GraphEdge> = {}) => {
    if (!present.has(from) || !present.has(to) || from === to) return
    linked.add(pairKey(from, to))
    out.push({ kind, from, to, extra })
  }
  for (const agent of ordered) {
    if (agent.parent_task_id && agent.parent_task_id !== agent.task_id && byID.get(agent.parent_task_id)?.role === "conductor") {
      link("swarm", agent.parent_task_id, agent.task_id)
    }
    // A named slot runs beside its card's own agent: the reviewer reviews
    // it, any other (a workflow's triage agent) works for the card. A
    // review loop between a reviewer and its card says more, and is drawn
    // instead.
    const looped = (data?.links ?? []).some((raw) => raw.kind === "review_loop" && raw.from === agent.task_id && raw.to === agent.card_task_id)
    if (agent.slot === "reviewer" && agent.card_task_id) {
      if (!looped) link("reviewer", agent.task_id, agent.card_task_id)
    } else if (agent.slot && agent.slot !== "main" && agent.card_task_id) link("slot", agent.task_id, agent.card_task_id, { label: agent.slot })
  }
  const window = (data?.peer_window_seconds ?? 1800) * 1000
  for (const raw of data?.links ?? []) {
    if (raw.kind === "peer_message") {
      const age = now - Date.parse(raw.last_at ?? "")
      if (!(age < window)) continue
      link("peer_message", raw.from, raw.to, { count: raw.count, opacity: 1 - 0.75 * Math.max(0, age) / window })
    } else if (raw.kind === "review_loop" || raw.kind === "reeve_watch") {
      link(raw.kind, raw.from, raw.to, raw.label ? { label: raw.label } : {})
    }
  }
  // Agents of one run are one piece of work: a chain in the order of their
  // steps, then of when they started, between those nothing else links.
  const members = new Map<string, { id: string; step: number; at: number }[]>()
  ordered.forEach((agent, at) => {
    if (agent.reeve) return
    for (const place of places[agent.task_id]?.workflows ?? []) {
      const run = runs[place.run_id]
      if (!run) continue
      const step = run.steps.findIndex((item) => item.step_id === place.step_id)
      members.set(run.id, [...(members.get(run.id) ?? []), { id: agent.task_id, step: step < 0 ? run.steps.length : step, at }])
    }
  })
  for (const [runID, list] of members) {
    list.sort((a, b) => a.step - b.step || a.at - b.at)
    for (let i = 1; i < list.length; i++) {
      if (!linked.has(pairKey(list[i - 1].id, list[i].id))) link("workflow", list[i - 1].id, list[i].id, { label: runs[runID].workflow_title, opacity: 0.8 })
    }
  }
  // Agents in one worktree can message each other: a faint chain between
  // those nothing else links.
  const worktrees = new Map<string, string[]>()
  for (const agent of ordered) {
    const worktree = places[agent.task_id]?.worktree
    if (worktree && !agent.reeve) worktrees.set(worktree, [...(worktrees.get(worktree) ?? []), agent.task_id])
  }
  for (const list of worktrees.values()) {
    for (let i = 1; i < list.length; i++) {
      if (!linked.has(pairKey(list[i - 1], list[i]))) link("worktree", list[i - 1], list[i], { opacity: 0.6 })
    }
  }
  return out
}

// orderer orders a box's agents so those that work together sit together:
// a reviewer under its author, a run's agents in the order of its steps, a
// conductor's workers under it, each group by when it started.
function orderer(relations: Relation[], ordered: AgentInfo[], boxOf: Map<string, string>) {
  const rank = new Map(ordered.map((agent, i) => [agent.task_id, i]))
  // The agent each agent goes after, when they share a box.
  const partner = new Map<string, string>()
  const follow = (id: string, after: string) => {
    if (!partner.has(id) && boxOf.get(id) !== undefined && boxOf.get(id) === boxOf.get(after)) partner.set(id, after)
  }
  for (const { kind, from, to } of relations) {
    if (kind === "reviewer" || kind === "review_loop") follow(from, to)
    else if (kind === "swarm" || kind === "workflow") follow(to, from)
  }
  const byRank = (a: string, b: string) => (rank.get(a) ?? 0) - (rank.get(b) ?? 0)
  return {
    // members is a box's agents in order, for its first draw.
    members(ids: string[]): string[] {
      const inBox = new Set(ids)
      const sorted = [...ids].sort(byRank)
      const group = new Map(sorted.map((id) => [id, id]))
      const find = (id: string): string => group.get(id) === id ? id : find(group.get(id)!)
      for (const { from, to } of relations) {
        if (!inBox.has(from) || !inBox.has(to)) continue
        const [a, b] = [find(from), find(to)].sort(byRank)
        group.set(b, a)
      }
      const out: string[] = []
      const placed = new Set<string>()
      const visit = (id: string) => {
        if (placed.has(id)) return
        placed.add(id)
        out.push(id)
        for (const child of sorted) if (partner.get(child) === id) visit(child)
      }
      const groups = [...new Set(sorted.map(find))]
      for (const root of groups) {
        const list = sorted.filter((id) => find(id) === root)
        for (const id of list) if (!partner.has(id)) visit(id)
        for (const id of list) visit(id)
      }
      return out
    },
    // insert puts a newcomer after the agent it goes after, and after that
    // agent's other followers; else last.
    insert(list: string[], id: string) {
      const after = partner.get(id)
      const at = after ? list.indexOf(after) : -1
      if (at < 0) {
        list.push(id)
        return
      }
      let i = at + 1
      while (i < list.length && partner.get(list[i]) === after) i++
      list.splice(i, 0, id)
    },
  }
}

interface RouteFrame {
  nodes: Map<string, GraphNode>
  boxes: GraphBox[]
  columns: number
  rowTop: number[]
}

// An end of a line drawn between boxes: an agent, a closed box, or the
// Reeve.
type End =
  | { kind: "agent"; node: GraphNode; box: GraphBox; row: number }
  | { kind: "box"; box: GraphBox; row: number }
  | { kind: "reeve"; node: GraphNode }

// A line's run along one band, under a key that lines
// drawn as one share.
interface Run {
  key: string
  lo: number
  hi: number
  // A watch line, and how far it runs from where it comes into the band.
  watch: boolean
  reach: number
}

// route gives each link its points: inside a box down its left lane, else
// out through the top of its box (or down from the Reeve), along the band
// above the row, down a gap between columns to another row's band, and in
// through the top of the other box. Parallel lines each get a track, the
// Reeve's lines to one box share one.
function route(links: GraphEdge[], frame: RouteFrame) {
  const boxByID = new Map(frame.boxes.map((box, i) => [box.id, { box, row: Math.floor(i / frame.columns) }]))
  const endOf = (id: string): End => {
    const node = frame.nodes.get(id)
    if (node?.kind === "reeve") return { kind: "reeve", node }
    if (node) return { kind: "agent", node, ...boxByID.get(node.box!)! }
    return { kind: "box", ...boxByID.get(id)! }
  }
  const laneStart = (box: GraphBox) => box.x + AGENT_X + sizes.agent.width + 8
  const xOf = (end: End) => end.kind === "reeve" ? end.node.x + end.node.width / 2 : laneStart(end.box) + TRACK
  const bandOf = (end: End) => end.kind === "reeve" ? 0 : end.row
  const gapX = (gap: number) => gap * (BOX_WIDTH + COL_GAP) - COL_GAP / 2
  const midY = (node: GraphNode) => node.y + node.height / 2

  // Links inside one box take tracks in its left lane, the shorter nearer
  // the agents.
  const inner = new Map<string, GraphEdge[]>()
  const outer: { edge: GraphEdge; source: End; target: End; key: string; gap: number }[] = []
  for (const edge of links) {
    const source = endOf(edge.source)
    const target = endOf(edge.target)
    if (source.kind === "agent" && target.kind === "agent" && source.box === target.box) {
      inner.set(source.box.id, [...(inner.get(source.box.id) ?? []), edge])
      continue
    }
    const key = edge.kind === "reeve_watch" && target.kind !== "reeve" ? `watch:${edge.source}->${target.box.id}` : edge.id
    const gap = bandOf(source) === bandOf(target) ? -1 : nearestGap(xOf(source), xOf(target), frame.columns, gapX)
    outer.push({ edge, source, target, key, gap })
  }
  for (const [boxID, list] of inner) {
    const box = boxByID.get(boxID)!.box
    const span = (edge: GraphEdge) => {
      const a = midY(frame.nodes.get(edge.source)!), b = midY(frame.nodes.get(edge.target)!)
      return { lo: Math.min(a, b), hi: Math.max(a, b) }
    }
    const levels = assign(list.map((edge) => ({ key: edge.id, ...span(edge), watch: false, reach: 0 })), false)
    const most = Math.max(...levels.values())
    const step = Math.min(TRACK, (AGENT_X - 12) / Math.max(1, most))
    for (const edge of list) {
      const x = box.x + AGENT_X - 8 - levels.get(edge.id)! * step
      const source = frame.nodes.get(edge.source)!, target = frame.nodes.get(edge.target)!
      edge.points = [{ x: source.x, y: midY(source) }, { x, y: midY(source) }, { x, y: midY(target) }, { x: target.x, y: midY(target) }]
      edge.labelAt = middle(edge.points)
    }
  }

  // The lines down each gap between columns. One that turns off sooner
  // runs on the side it turns to, so it never crosses one that goes on.
  const gapRuns = new Map<number, { key: string; watch: boolean; band: number; left: boolean }[]>()
  for (const { source, target, key, gap, edge } of outer) {
    if (gap < 0) continue
    const lower = bandOf(source) > bandOf(target) ? source : target
    const list = gapRuns.get(gap) ?? []
    const same = list.find((item) => item.key === key)
    if (same) same.band = Math.max(same.band, bandOf(lower))
    else list.push({ key, watch: edge.kind === "reeve_watch", band: bandOf(lower), left: xOf(lower) < gapX(gap) })
    gapRuns.set(gap, list)
  }
  const gapOffset = new Map<string, number>()
  for (const [gap, list] of gapRuns) {
    const step = Math.min(TRACK, (COL_GAP - 10) / Math.max(1, list.length - 1))
    const sorted = [...list].sort((a, b) => Number(b.left) - Number(a.left) || (a.left ? a.band - b.band : b.band - a.band) || a.key.localeCompare(b.key))
    sorted.forEach((run, i) => gapOffset.set(`${gap}|${run.key}`, gapX(gap) + (i - (sorted.length - 1) / 2) * step))
  }

  // The runs along each band.
  const bands = new Map<number, Run[]>()
  const add = (band: number, run: Run) => {
    const list = bands.get(band) ?? []
    const same = list.find((item) => item.key === run.key)
    if (same) {
      same.lo = Math.min(same.lo, run.lo)
      same.hi = Math.max(same.hi, run.hi)
      same.reach = Math.max(same.reach, run.reach)
    } else list.push({ ...run })
    bands.set(band, list)
  }
  for (const { edge, source, target, key, gap } of outer) {
    const watch = edge.kind === "reeve_watch"
    const [a, b] = [bandOf(source), bandOf(target)]
    const [xs, xt] = [xOf(source), xOf(target)]
    const along = (band: number, from: number, to: number) => add(band, { key, lo: Math.min(from, to), hi: Math.max(from, to), watch, reach: Math.abs(to - from) })
    if (gap < 0) along(a, xs, xt)
    else {
      const x = gapOffset.get(`${gap}|${key}`)!
      along(a, xs, x)
      along(b, x, xt)
    }
  }
  const trackY = new Map<string, number>()
  for (const [band, list] of bands) {
    const bottom = frame.rowTop[band] ?? 0
    const height = band === 0 ? REEVE_BAND : ROW_GAP
    // The Reeve's lines take the tracks nearest the top, the farthest
    // highest, so they fan out without crossing; the rest the tracks below,
    // the shortest nearest the boxes.
    const watched = list.filter((run) => run.watch).sort((a, b) => b.reach - a.reach || a.key.localeCompare(b.key))
    const others = assign(list.filter((run) => !run.watch), true)
    const levels = others.size > 0 ? Math.max(...others.values()) + 1 : 0
    const step = Math.min(TRACK, (height - 20) / Math.max(1, watched.length + levels - 1))
    watched.forEach((run, i) => trackY.set(`${band}|${run.key}`, bottom - height + 10 + i * step))
    for (const [key, level] of others) trackY.set(`${band}|${key}`, bottom - 10 - level * step)
  }
  // Each box's right lane: a line to a lower agent runs farther out, so a
  // line to an upper one never crosses it.
  const lanes = new Map<string, { key: string; y: number; left: boolean; track: number }[]>()
  for (const { source, target, key, gap } of outer) {
    for (const [end, other] of [[source, target], [target, source]] as const) {
      if (end.kind === "reeve") continue
      const band = bandOf(end)
      const towards = gap < 0 ? xOf(other) : gapX(gap)
      const y = end.kind === "agent" ? midY(end.node) : end.box.y
      const list = lanes.get(end.box.id) ?? []
      const same = list.find((item) => item.key === key)
      if (same) same.y = Math.max(same.y, y)
      else list.push({ key, y, left: towards < xOf(end), track: trackY.get(`${band}|${key}`)! })
      lanes.set(end.box.id, list)
    }
  }
  const laneX = new Map<string, number>()
  for (const [boxID, list] of lanes) {
    list.sort((a, b) => a.y - b.y || Number(b.left) - Number(a.left) || (a.left ? b.track - a.track : a.track - b.track) || a.key.localeCompare(b.key))
    const step = Math.min(TRACK, (RIGHT_LANE - 12) / Math.max(1, list.length - 1))
    const start = laneStart(boxByID.get(boxID)!.box)
    list.forEach((item, i) => laneX.set(`${boxID}|${item.key}`, start + i * step))
  }

  for (const { edge, source, target, key, gap } of outer) {
    // out is the line from an end up to its band, ending at its track.
    const out = (end: End): Point[] => {
      const y = trackY.get(`${bandOf(end)}|${key}`)!
      if (end.kind === "reeve") {
        const x = xOf(end)
        return [{ x, y: end.node.y + end.node.height }, { x, y }]
      }
      const x = laneX.get(`${end.box.id}|${key}`)!
      const top = { x, y: end.box.y }
      if (end.kind === "box") return [top, { x, y }]
      return [{ x: end.node.x + end.node.width, y: midY(end.node) }, { x, y: midY(end.node) }, top, { x, y }]
    }
    const from = out(source)
    const to = out(target).reverse()
    const between: Point[] = []
    if (gap >= 0) {
      const x = gapOffset.get(`${gap}|${key}`)!
      between.push({ x, y: from[from.length - 1].y }, { x, y: to[0].y })
    }
    edge.points = dedupe([...from, ...between, ...to])
    edge.labelAt = middle(edge.points)
  }
}

// nearestGap is the gap between columns a line goes down to reach another
// row: the one nearest both its ends.
function nearestGap(a: number, b: number, columns: number, gapX: (gap: number) => number): number {
  let best = 0
  for (let gap = 1; gap <= columns; gap++) {
    if (Math.abs(gapX(gap) - a) + Math.abs(gapX(gap) - b) < Math.abs(gapX(best) - a) + Math.abs(gapX(best) - b)) best = gap
  }
  return best
}

// assign gives runs levels so that no two that overlap share one, the
// shortest first, so a run nests inside the runs that span it.
function assign(runs: Run[], strict: boolean): Map<string, number> {
  const done: { run: Run; level: number }[] = []
  const margin = strict ? 2 : 0
  for (const run of [...runs].sort((a, b) => (a.hi - a.lo) - (b.hi - b.lo) || a.key.localeCompare(b.key))) {
    const overlapping = done.filter(({ run: other }) => run.lo <= other.hi + margin && other.lo <= run.hi + margin)
    let level = 0
    if (strict) {
      while (overlapping.some((item) => item.level === level)) level++
    } else if (overlapping.length > 0) {
      level = Math.max(...overlapping.map((item) => item.level)) + 1
    }
    done.push({ run, level })
  }
  return new Map(done.map(({ run, level }) => [run.key, level]))
}

function dedupe(points: Point[]): Point[] {
  return points.filter((point, i) => i === 0 || point.x !== points[i - 1].x || point.y !== points[i - 1].y)
}

// middle is the middle of a line's longest run.
function middle(points: Point[]): Point {
  let best = { x: points[0].x, y: points[0].y }, length = -1
  for (let i = 1; i < points.length; i++) {
    const [a, b] = [points[i - 1], points[i]]
    const d = Math.abs(a.x - b.x) + Math.abs(a.y - b.y)
    if (d > length) {
      length = d
      best = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
    }
  }
  return best
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

// subNode is a sub-agent (or a fold) under its agent, never wider than the
// agent reaches, so the box's right lane stays clear.
function subNode(agent: AgentInfo, tree: SubTree, box: string, x: number, y: number, depth: number): GraphNode {
  const width = Math.min(sizes.sub.width, AGENT_X + sizes.agent.width - x)
  if (tree.fold !== undefined) return { id: tree.id, kind: "fold", agentId: agent.task_id, box, x, y, width, height: sizes.sub.height, depth, hidden: tree.fold }
  const waiting = tree.job?.state === "background" ? subagentTree(agent.background_jobs).find((each) => each.job.id === tree.job?.id) : undefined
  return {
    id: tree.id, kind: "subagent", agentId: agent.task_id, box, x, y, width, height: sizes.sub.height, depth, job: tree.job, state: tree.job?.state,
    ...(waiting ? { waitsOn: subagentWaitsOn(waiting) } : {}),
  }
}

function pairKey(a: string, b: string): string {
  return a < b ? `${a}|${b}` : `${b}|${a}`
}

// byBoxName orders boxes by repository, No repository last.
function byBoxName(a: string, b: string): number {
  const [x, y] = [repoOfBox(a), repoOfBox(b)]
  return (x === "") === (y === "") ? x.localeCompare(y) : x === "" ? 1 : -1
}

// byStart orders agents by when they started, then by ID.
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
      case "slot":
        out.push(outgoing ? `${edge.label} for ${other}` : `has ${edge.label} agent ${other}`)
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
      case "workflow":
        out.push(`in run “${edge.label ?? ""}” with ${other}`)
        break
    }
  }
  return out
}

// workflowWords is what the graph says of an agent's place in a run: its
// step's number and title, its role there in words, and where the run is
// when that is another step.
export function workflowWords({ place, run }: NodeWorkflow): { number: number; total: number; title: string; role: string; now: string } {
  const index = run.steps.findIndex((step) => step.step_id === place.step_id)
  const title = index >= 0 ? run.steps[index].title : place.step_id
  const step = `“${title}”`
  const pass = run.pass && place.step_id === run.current_step_id && (place.role === "reviewer" || place.role === "author") ? ` · pass ${run.pass}` : ""
  const role = {
    agent: `Runs ${step}`,
    interview: `Interviewed in ${step}`,
    reviewer: `Reviewer in ${step}`,
    author: `Author in ${step}`,
    awaited: `${step} starts when it ends`,
  }[place.role] ?? `In ${step}`
  const current = run.steps.find((item) => item.step_id === run.current_step_id)
  const now = current && run.current_step_id !== place.step_id ? `The run is at “${current.title}”` : ""
  return { number: index + 1, total: run.steps.length, title, role: role + pass, now }
}
