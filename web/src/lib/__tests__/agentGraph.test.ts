import { describe, expect, it } from "vitest"
import type { AgentInfo, BackgroundJob } from "../../hooks/useWebSocket"
import { layoutAgentGraph, type AgentGraphData, type AgentGraphLayout, type LayoutOptions } from "../agentGraph"

const NOW = Date.parse("2026-11-23T10:00:00Z")

const agent = (taskId: string, values: Partial<AgentInfo> = {}): AgentInfo => ({
  id: 1, task_id: taskId, card_task_id: taskId, slot: "main", task_title: taskId, tmux_session: `legato-${taskId}`, command: "claude",
  agent_kind: "claude-code", status: "running", activity: "working", started_at: "2026-11-23T09:00:00Z",
  working_seconds: 0, waiting_seconds: 0, error_seconds: 0, ...values,
})

const sub = (id: string, parent?: string, state = "working"): BackgroundJob =>
  ({ kind: "subagent", id, label: `sub ${id}`, started_at: "2026-11-23T09:30:00Z", state, ...(parent ? { parent } : {}) })

const data = (values: Partial<AgentGraphData> = {}): AgentGraphData =>
  ({ agents: {}, links: [], peer_window_seconds: 1800, ...values })

const lay = (agents: AgentInfo[], graph: AgentGraphData | null = data(), options: Partial<LayoutOptions> = {}): AgentGraphLayout =>
  layoutAgentGraph(agents, graph, { now: NOW, direction: "LR", ...options })

const node = (layout: AgentGraphLayout, id: string) => {
  const found = layout.nodes.find((n) => n.id === id)
  if (!found) throw new Error(`no node ${id} in ${layout.nodes.map((n) => n.id).join(", ")}`)
  return found
}

const edgeKinds = (layout: AgentGraphLayout) => layout.edges.map((e) => `${e.kind}:${e.source}->${e.target}`).sort()

describe("the agent graph's boxes", () => {
  it("boxes agents by repository, in name order, with agents outside Git last", () => {
    const layout = lay(
      [agent("a"), agent("b"), agent("c"), agent("d")],
      data({ agents: { a: { repo: "cpave3/staccato" }, b: { repo: "cpave3/legato" }, c: {}, d: { repo: "cpave3/legato" } } }),
    )
    expect(layout.boxes.map((box) => [box.label, box.agents])).toEqual([["cpave3/legato", 2], ["cpave3/staccato", 1], ["No repository", 1]])
    const [legato, staccato] = layout.boxes
    expect(staccato.x >= legato.x + legato.width || staccato.y >= legato.y + legato.height).toBe(true)
    expect(node(layout, "b").box).toBe(legato.id)
    expect(node(layout, "a").box).toBe(staccato.id)
  })

  it("sets boxes side by side on a wide screen until a row is full, and one under another on a phone", () => {
    const repos = ["a/1", "a/2", "a/3", "a/4"]
    const agents = repos.map((_, i) => agent(`t${i}`))
    const graph = data({ agents: Object.fromEntries(repos.map((repo, i) => [`t${i}`, { repo }])) })
    const wide = lay(agents, graph, { maxWidth: 900 }).boxes
    expect(wide[1].y).toBe(wide[0].y)
    expect(wide[1].x).toBeGreaterThanOrEqual(wide[0].x + wide[0].width)
    expect(wide[3].y).toBeGreaterThanOrEqual(Math.max(wide[0].y + wide[0].height, wide[1].y + wide[1].height))
    const phone = lay(agents, graph, { direction: "TB" }).boxes
    expect(phone.map((box) => box.x)).toEqual([0, 0, 0, 0])
    for (let i = 1; i < phone.length; i++) expect(phone[i].y).toBeGreaterThanOrEqual(phone[i - 1].y + phone[i - 1].height)
  })

  it("uses a job clone's repository before the graph has loaded", () => {
    const layout = lay([agent("a", { clone: { repo: "acme/widget" } })], null)
    expect(layout.boxes.map((box) => box.label)).toEqual(["acme/widget"])
  })

  it("orders a box's agents by when they started", () => {
    const layout = lay([agent("late", { started_at: "2026-11-23T09:10:00Z" }), agent("early", { started_at: "2026-11-23T08:00:00Z" })])
    expect(node(layout, "early").y).toBeLessThan(node(layout, "late").y)
  })

  it("keeps every agent in its place when a newer one joins its box", () => {
    const before = lay([agent("a", { started_at: "2026-11-23T08:00:00Z" }), agent("b", { started_at: "2026-11-23T08:10:00Z" })])
    const after = lay([agent("a", { started_at: "2026-11-23T08:00:00Z" }), agent("b", { started_at: "2026-11-23T08:10:00Z" }), agent("c", { started_at: "2026-11-23T09:50:00Z" })])
    for (const id of ["a", "b"]) expect({ x: node(after, id).x, y: node(after, id).y }).toEqual({ x: node(before, id).x, y: node(before, id).y })
  })

  it("collapses a box to its header, sending its links to the box", () => {
    const graph = data({ agents: { a: { repo: "acme/one" }, b: { repo: "acme/two" } }, links: [{ kind: "peer_message", from: "a", to: "b", count: 1, last_at: "2026-11-23T09:59:00.000Z" }] })
    const open = lay([agent("a"), agent("b")], graph)
    const two = open.boxes.find((box) => box.label === "acme/two")!
    const layout = lay([agent("a"), agent("b")], graph, { collapsed: new Set([two.id]) })
    expect(layout.nodes.map((n) => n.id)).toEqual(["a"])
    expect(layout.boxes.find((box) => box.id === two.id)).toMatchObject({ collapsed: true, agents: 1 })
    expect(edgeKinds(layout)).toEqual([`peer_message:a->${two.id}`])
  })

  it("puts the Reeve outside every box", () => {
    const layout = lay([agent("reeve-1", { reeve: true }), agent("a")])
    expect(node(layout, "reeve-1").kind).toBe("reeve")
    expect(node(layout, "reeve-1").box).toBeUndefined()
    expect(layout.boxes).toHaveLength(1)
  })
})

describe("sub-agents on the graph", () => {
  it("branches nested sub-agents off the agent that started them", () => {
    const layout = lay([agent("a", { background_jobs: [sub("s1"), sub("s2", "s1", "waiting"), { kind: "bash", id: "sh", label: "tests", started_at: "x" }] })])
    const s1 = node(layout, "a/s1")
    const s2 = node(layout, "a/s2")
    expect(s1).toMatchObject({ kind: "subagent", depth: 1 })
    expect(s2).toMatchObject({ kind: "subagent", depth: 2, state: "waiting" })
    expect(s1.x).toBeGreaterThan(node(layout, "a").x)
    expect(s2.x).toBeGreaterThan(s1.x)
    expect(edgeKinds(layout)).toEqual(["subagent:a->a/s1", "subagent:a/s1->a/s2"])
    expect(layout.nodes.some((n) => n.id.endsWith("/sh"))).toBe(false)
  })

  it("reads top to bottom at phone width, each sub-agent below and indented", () => {
    const layout = lay([agent("a", { background_jobs: [sub("s1")] }), agent("b", { started_at: "2026-11-23T09:05:00Z" })], data(), { direction: "TB" })
    const [a, s1, b] = [node(layout, "a"), node(layout, "a/s1"), node(layout, "b")]
    expect(s1.y).toBeGreaterThan(a.y)
    expect(s1.x).toBeGreaterThan(a.x)
    expect(b.y).toBeGreaterThan(s1.y)
  })

  it("folds more than three sub-agents into one node, and unfolds them on asking", () => {
    const jobs = [sub("s1"), sub("s2"), sub("s3"), sub("s4"), sub("s5"), sub("s6", "s5")]
    const folded = lay([agent("a", { background_jobs: jobs })])
    expect(folded.nodes.filter((n) => n.kind === "subagent").map((n) => n.id)).toEqual(["a/s1", "a/s2", "a/s3"])
    expect(node(folded, "a/+")).toMatchObject({ kind: "fold", hidden: 3 })
    expect(folded.edges.some((e) => e.target === "a/+")).toBe(true)

    const open = lay([agent("a", { background_jobs: jobs })], data(), { unfolded: new Set(["a"]) })
    expect(open.nodes.filter((n) => n.kind === "subagent")).toHaveLength(6)
    expect(open.nodes.some((n) => n.kind === "fold")).toBe(false)
  })
})

describe("links between agents", () => {
  it("links a swarm's conductor to its workers", () => {
    const layout = lay([
      agent("p", { role: "conductor", parent_task_id: "p" }),
      agent("st-1", { role: "parser", parent_task_id: "p", subtask_id: "st-1" }),
    ])
    expect(edgeKinds(layout)).toEqual(["swarm:p->st-1"])
  })

  it("links a task's reviewer slot to its author", () => {
    const layout = lay([agent("t"), agent("t_reviewer", { card_task_id: "t", slot: "reviewer" })])
    expect(edgeKinds(layout)).toEqual(["reviewer:t_reviewer->t"])
  })

  it("draws the server's links, with a peer message fading as it ages and gone after the window", () => {
    const links = [
      { kind: "review_loop", from: "r_reviewer", to: "a", label: "pass 2" },
      { kind: "reeve_watch", from: "reeve-1", to: "a" },
      { kind: "peer_message", from: "a", to: "b", count: 3, last_at: "2026-11-23T09:59:00.000Z" },
      { kind: "peer_message", from: "b", to: "a", count: 1, last_at: "2026-11-23T09:35:00.000Z" },
      { kind: "peer_message", from: "b", to: "reeve-1", count: 1, last_at: "2026-11-23T09:00:00.000Z" },
    ]
    const layout = lay(
      [agent("a"), agent("b"), agent("r_reviewer", { card_task_id: "r", slot: "reviewer" }), agent("reeve-1", { reeve: true })],
      data({ links }),
    )
    expect(edgeKinds(layout)).toEqual(["peer_message:a->b", "peer_message:b->a", "reeve_watch:reeve-1->a", "review_loop:r_reviewer->a"])
    const fresh = layout.edges.find((e) => e.source === "a" && e.target === "b")!
    const older = layout.edges.find((e) => e.source === "b" && e.target === "a")!
    expect(fresh).toMatchObject({ count: 3 })
    expect(fresh.opacity).toBeGreaterThan(older.opacity)
    expect(older.opacity).toBeGreaterThan(0)
    expect(layout.edges.find((e) => e.kind === "review_loop")).toMatchObject({ label: "pass 2", opacity: 1 })
  })

  it("links agents in one worktree that nothing else links", () => {
    const graph = data({ agents: { a: { repo: "x/y", worktree: "/w" }, b: { repo: "x/y", worktree: "/w" }, c: { repo: "x/y", worktree: "/w" }, d: { repo: "x/y", worktree: "/other" } } })
    const layout = lay([
      agent("a", { started_at: "2026-11-23T08:00:00Z" }), agent("b", { started_at: "2026-11-23T08:01:00Z" }),
      agent("c", { started_at: "2026-11-23T08:02:00Z" }), agent("d"),
    ], graph)
    expect(edgeKinds(layout)).toEqual(["worktree:a->b", "worktree:b->c"])
  })

  it("leaves out links to agents that are not shown", () => {
    const layout = lay([agent("a")], data({ links: [{ kind: "reeve_watch", from: "reeve-1", to: "a" }] }))
    expect(layout.edges).toEqual([])
  })
})
