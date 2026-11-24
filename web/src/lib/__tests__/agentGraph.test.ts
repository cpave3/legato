import { describe, expect, it } from "vitest"
import type { AgentInfo, BackgroundJob } from "../../hooks/useWebSocket"
import { compactGraphMemory, GONE_GRACE_MS, layoutAgentGraph, linkLines, type AgentGraphData, type AgentGraphLayout, type GraphMemory, type GraphRun, type LayoutOptions } from "../agentGraph"
import { busyFleet, screenshotFleet } from "../../test/agentGraphFleet"
import { boxesCrossed, crossings, geometryOf, inside, nodesCrossed, orthogonal } from "./graphGeometry"

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
  // Under their agent, not to its right (LEG-615): a sub-agent never widens
  // its box, so it never moves the boxes beside it.
  it("hangs nested sub-agents under the agent that started them, each indented", () => {
    const layout = lay([agent("a", { background_jobs: [sub("s1"), sub("s2", "s1", "waiting"), { kind: "bash", id: "sh", label: "tests", started_at: "x" }] })])
    const a = node(layout, "a")
    const s1 = node(layout, "a/s1")
    const s2 = node(layout, "a/s2")
    expect(s1).toMatchObject({ kind: "subagent", depth: 1 })
    expect(s2).toMatchObject({ kind: "subagent", depth: 2, state: "waiting" })
    expect(s1.x).toBeGreaterThan(a.x)
    expect(s1.y).toBeGreaterThan(a.y)
    expect(s2.x).toBeGreaterThan(s1.x)
    expect(s2.y).toBeGreaterThan(s1.y)
    expect(s2.x + s2.width).toBeLessThanOrEqual(a.x + a.width)
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

  it("draws one line between a reviewer and its author when a review loop joins them", () => {
    const layout = lay([agent("t"), agent("t_reviewer", { card_task_id: "t", slot: "reviewer" })], data({ links: [{ kind: "review_loop", from: "t_reviewer", to: "t", label: "pass 4" }] }))
    expect(edgeKinds(layout)).toEqual(["review_loop:t_reviewer->t"])
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

const run = (id: string, values: Partial<GraphRun> = {}): GraphRun => ({
  id, workflow_name: "flow", workflow_title: `Run ${id}`, status: "running", current_step_id: "implement", created_at: "2026-11-23 09:00:00",
  steps: [
    { step_id: "triage", title: "Triage", status: "succeeded" },
    { step_id: "implement", title: "Implement", status: "waiting_agent" },
    { step_id: "review", title: "Review", status: "pending" },
  ],
  ...values,
})

describe("workflow runs on the graph", () => {
  it("joins the shown agents of one run in step order, then by when they started", () => {
    const graph = data({
      agents: {
        tri: { workflows: [{ run_id: "wr-1", step_id: "triage", role: "agent" }] },
        impl: { workflows: [{ run_id: "wr-1", step_id: "implement", role: "agent" }] },
        q: { workflows: [{ run_id: "wr-1", step_id: "implement", role: "interview" }] },
      },
      runs: { "wr-1": run("wr-1") },
    })
    const layout = lay([
      agent("q", { started_at: "2026-11-23T08:30:00Z" }), agent("tri", { started_at: "2026-11-23T09:00:00Z" }), agent("impl", { started_at: "2026-11-23T08:00:00Z" }),
    ], graph)
    expect(edgeKinds(layout)).toEqual(["workflow:impl->q", "workflow:tri->impl"])
    expect(layout.edges.find((e) => e.kind === "workflow")).toMatchObject({ label: "Run wr-1" })
  })

  it("draws no run link for an agent alone in its run, nor for one in none", () => {
    const graph = data({ agents: { a: { workflows: [{ run_id: "wr-1", step_id: "implement", role: "agent" }] } }, runs: { "wr-1": run("wr-1") } })
    const layout = lay([agent("a"), agent("b")], graph)
    expect(layout.edges).toEqual([])
    expect(node(layout, "a").workflows).toEqual([{ place: { run_id: "wr-1", step_id: "implement", role: "agent" }, run: run("wr-1") }])
    expect(node(layout, "b").workflows).toBeUndefined()
  })

  it("puts an agent's runs on its node in the server's order, leaving out a run it does not list", () => {
    const graph = data({
      agents: { a: { workflows: [
        { run_id: "wr-2", step_id: "review", role: "author" },
        { run_id: "gone", step_id: "x", role: "agent" },
        { run_id: "wr-1", step_id: "implement", role: "awaited" },
      ] } },
      runs: { "wr-1": run("wr-1"), "wr-2": run("wr-2", { current_step_id: "review" }) },
    })
    expect(node(lay([agent("a")], graph), "a").workflows?.map((w) => `${w.run.id}:${w.place.role}`)).toEqual(["wr-2:author", "wr-1:awaited"])
  })

  it("sends a collapsed box's run link to the box", () => {
    const graph = data({
      agents: {
        a: { repo: "x/1", workflows: [{ run_id: "wr-1", step_id: "triage", role: "agent" }] },
        b: { repo: "x/2", workflows: [{ run_id: "wr-1", step_id: "implement", role: "agent" }] },
      },
      runs: { "wr-1": run("wr-1") },
    })
    const layout = lay([agent("a"), agent("b")], graph, { collapsed: new Set(["box:x/1"]) })
    expect(edgeKinds(layout)).toEqual(["workflow:box:x/1->b"])
  })

  it("adds no run link between agents another link joins", () => {
    const graph = data({
      agents: {
        t: { workflows: [{ run_id: "wr-1", step_id: "review", role: "author" }] },
        t_reviewer: { workflows: [{ run_id: "wr-1", step_id: "review", role: "reviewer" }] },
      },
      runs: { "wr-1": run("wr-1", { current_step_id: "review" }) },
    })
    const layout = lay([agent("t"), agent("t_reviewer", { card_task_id: "t", slot: "reviewer" })], graph)
    expect(edgeKinds(layout)).toEqual(["reviewer:t_reviewer->t"])
  })

  it("says which run an agent shares with another", () => {
    const graph = data({
      agents: { a: { workflows: [{ run_id: "wr-1", step_id: "triage", role: "agent" }] }, b: { workflows: [{ run_id: "wr-1", step_id: "implement", role: "agent" }] } },
      runs: { "wr-1": run("wr-1") },
    })
    const agents = [agent("a"), agent("b")]
    const layout = lay(agents, graph)
    expect(linkLines("a", layout.edges, agents)).toEqual(["in run “Run wr-1” with b"])
    expect(linkLines("b", layout.edges, agents)).toEqual(["in run “Run wr-1” with a"])
  })
})

// LEG-615: the Reeve above the fleet, lines that go around boxes, and a
// layout that stays put.
describe("where the graph's lines run", () => {
  const screenshot = (options: Partial<LayoutOptions> = {}) => {
    const fleet = screenshotFleet()
    return lay(fleet.agents, fleet.data, options)
  }

  it("stands the Reeve above the fleet, centred over its first row", () => {
    for (const direction of ["LR", "TB"] as const) {
      const layout = screenshot({ direction })
      const reeve = node(layout, "reeve-1")
      expect(reeve.y + reeve.height).toBeLessThan(Math.min(...layout.boxes.map((box) => box.y)))
      const first = layout.boxes.filter((box) => box.y === 0)
      const left = Math.min(...first.map((box) => box.x)), right = Math.max(...first.map((box) => box.x + box.width))
      expect(Math.abs(reeve.x + reeve.width / 2 - (left + right) / 2)).toBeLessThanOrEqual(1)
    }
  })

  it("draws every link as straight runs across and down", () => {
    expect(orthogonal(screenshot())).toBe(true)
    expect(orthogonal(lay(busyFleet(NOW).agents, busyFleet(NOW).data))).toBe(true)
  })

  it("starts and ends each link on what it joins", () => {
    const layout = lay(busyFleet(NOW).agents, busyFleet(NOW).data)
    const of = (id: string) => layout.nodes.find((n) => n.id === id) ?? layout.boxes.find((box) => box.id === id)!
    for (const edge of layout.edges) {
      expect(inside(edge.points[0], of(edge.source))).toBe(true)
      expect(inside(edge.points[edge.points.length - 1], of(edge.target))).toBe(true)
    }
  })

  it("runs no line through a box or a node it does not end in", () => {
    const fleet = screenshotFleet()
    expect(boxesCrossed(lay(fleet.agents, fleet.data))).toEqual([])
    expect(nodesCrossed(lay(fleet.agents, fleet.data))).toEqual([])
    const busy = busyFleet(NOW)
    for (const options of [{}, { maxWidth: 700 }, { direction: "TB" as const }, { collapsed: new Set(["box:cpave3/chimera"]) }]) {
      const layout = lay(busy.agents, busy.data, options)
      expect(boxesCrossed(layout)).toEqual([])
      expect(nodesCrossed(layout)).toEqual([])
    }
  })

  it("leaves no crossing in the screenshot's fleet", () => {
    expect(crossings(screenshot())).toEqual([])
    expect(crossings(screenshot({ maxWidth: 700 }))).toEqual([])
    expect(crossings(screenshot({ direction: "TB" }))).toEqual([])
  })

  it("keeps the crossings of a busier fleet to the ones its links force", () => {
    const busy = busyFleet(NOW)
    expect(crossings(lay(busy.agents, busy.data))).toHaveLength(BUSY_CROSSINGS)
  })

  it("keeps a review loop inside its box", () => {
    const layout = screenshot()
    const chimera = layout.boxes.find((box) => box.id === "box:cpave3/chimera")!
    const loop = layout.edges.find((e) => e.kind === "review_loop")!
    for (const point of loop.points) expect(inside(point, chimera)).toBe(true)
    expect(inside(loop.labelAt, chimera)).toBe(true)
  })

  it("bundles the Reeve's lines to one box into one trunk down into it", () => {
    const layout = screenshot()
    const legato = layout.boxes.find((box) => box.id === "box:cpave3/legato")!
    const watch = layout.edges.filter((e) => e.kind === "reeve_watch" && (e.target === "LEG-612" || e.target === "LEG-613"))
    // Each enters the box through its top edge at one place.
    const entry = watch.map((e) => e.points.find((p) => p.y === legato.y && p.x > legato.x && p.x < legato.x + legato.width))
    expect(entry[0]).toBeDefined()
    expect(entry[1]).toEqual(entry[0])
  })
})

// The busy fleet's one crossing: the peer message from autopilot to legato
// crosses the Reeve's trunk down into chimera, the box between them. Run
// above the Reeve's lines instead, it would cross the Reeve's own trunk.
const BUSY_CROSSINGS = 1

describe("a graph that stays put", () => {
  // step lays out agents with the memory the last layout left.
  const stepper = (graph: AgentGraphData, options: Partial<LayoutOptions> = {}) => {
    let memory: GraphMemory | undefined
    return (agents: AgentInfo[], extra: Partial<LayoutOptions> = {}, data = graph) => {
      const layout = lay(agents, data, { ...options, memory, ...extra })
      memory = layout.memory
      return layout
    }
  }
  const at = (layout: AgentGraphLayout, id: string) => ({ x: node(layout, id).x, y: node(layout, id).y })

  it("moves nothing on twenty updates that change only state", () => {
    const fleet = busyFleet(NOW)
    const step = stepper(fleet.data)
    const first = geometryOf(step(fleet.agents))
    const activities = ["working", "waiting", "idle", "error"]
    for (let i = 0; i < 20; i++) {
      const agents = fleet.agents.map((a, j) => ({
        ...a, activity: activities[(i + j) % 4], stage: i % 2 ? "reviewing" : "implementing", stage_reason: `reason ${i}`,
        glance: i % 3 ? "working" : "needs_you", working_seconds: i * 60, waiting_seconds: i * 7,
      }))
      const run = fleet.data.runs!["wr-9"]
      const data: AgentGraphData = {
        ...fleet.data,
        runs: { "wr-9": { ...run, current_step_id: i % 2 ? "triage" : "implement", pass: i, needs_attention: i % 2 === 0 } },
        links: fleet.data.links.map((link) => link.kind === "peer_message" ? { ...link, last_at: new Date(NOW - 60_000 * (i + 1)).toISOString() } : link),
      }
      expect(geometryOf(step(agents, { now: NOW + i * 30_000 }, data))).toEqual(first)
    }
  })

  it("moves only the agents below a new sub-agent, by one row and down, and nothing back up when it goes", () => {
    const fleet = screenshotFleet()
    // chimera, the tallest box of the first row, has legato under it.
    const step = stepper(fleet.data, { maxWidth: 700 })
    const before = step(fleet.agents)
    const withSub = fleet.agents.map((a) => a.task_id === "CHIM-97" ? { ...a, background_jobs: [sub("s1")] } : a)
    const after = step(withSub)
    for (const n of before.nodes) {
      const moved = { x: node(after, n.id).x - n.x, y: node(after, n.id).y - n.y }
      const below = (n.box === "box:cpave3/chimera" && n.y > node(before, "CHIM-97").y) || n.box === "box:cpave3/legato"
      expect(moved).toEqual({ x: 0, y: below ? 44 : 0 })
    }
    // When it goes, its own box closes up, and the row below stays put.
    const gone = step(fleet.agents)
    expect(at(gone, "CHIM-101")).toEqual(at(before, "CHIM-101"))
    expect(at(gone, "LEG-612")).toEqual(at(after, "LEG-612"))
  })

  it("keeps a sub-agent in a short box from moving the row below", () => {
    const fleet = screenshotFleet()
    const step = stepper(fleet.data, { maxWidth: 700 })
    const before = step(fleet.agents)
    const after = step(fleet.agents.map((a) => a.task_id === "AG-12" ? { ...a, background_jobs: [sub("s1")] } : a))
    expect(at(after, "LEG-612")).toEqual(at(before, "LEG-612"))
    expect(at(after, "CHIM-101")).toEqual(at(before, "CHIM-101"))
  })

  it("keeps a restarted agent in its place", () => {
    const fleet = screenshotFleet()
    const step = stepper(fleet.data)
    const before = step(fleet.agents)
    const after = step(fleet.agents.map((a) => a.task_id === "CHIM-97" ? { ...a, started_at: "2026-11-23T09:55:00Z" } : a))
    expect(geometryOf(after)).toEqual(geometryOf(before))
  })

  it("holds a departed agent's place for a while, then closes up", () => {
    const fleet = screenshotFleet()
    const step = stepper(fleet.data)
    const before = step(fleet.agents)
    const without = fleet.agents.filter((a) => a.task_id !== "CHIM-97")
    const held = step(without, { now: NOW + 1000 })
    expect(at(held, "CHIM-101")).toEqual(at(before, "CHIM-101"))
    expect(at(step(without, { now: NOW + GONE_GRACE_MS - 1000 }), "CHIM-101")).toEqual(at(before, "CHIM-101"))
    expect(at(step(without, { now: NOW + GONE_GRACE_MS + 2000 }), "CHIM-101").y).toBeLessThan(at(before, "CHIM-101").y)
  })

  it("closes up at once when the memory is compacted", () => {
    const fleet = screenshotFleet()
    const before = lay(fleet.agents, fleet.data)
    const without = fleet.agents.filter((a) => a.task_id !== "CHIM-97")
    const held = lay(without, fleet.data, { memory: before.memory, now: NOW + 1000 })
    expect(at(held, "CHIM-101")).toEqual(at(before, "CHIM-101"))
    const compact = lay(without, fleet.data, { memory: compactGraphMemory(held.memory), now: NOW + 1000 })
    expect(at(compact, "CHIM-101").y).toBeLessThan(at(before, "CHIM-101").y)
  })

  it("brings a reviewer that comes back for its next pass back under its author, moving nothing else", () => {
    const fleet = screenshotFleet()
    const step = stepper(fleet.data)
    const before = step(fleet.agents)
    const reviewer = fleet.agents.find((a) => a.task_id === "CHIM-97_reviewer")!
    const others = fleet.agents.filter((a) => a !== reviewer)
    step(others, { now: NOW + 5 * 60_000 })
    const back = step([...others, { ...reviewer, started_at: "2026-11-23T10:05:00Z" }], { now: NOW + 6 * 60_000 })
    expect(node(back, "CHIM-97_reviewer").y).toBe(node(back, "CHIM-97").y + 84)
    for (const id of ["reeve-1", "AG-12", "CHIM-97", "LEG-612", "LEG-613"]) expect(at(back, id)).toEqual(at(before, id))
  })

  it("puts a new box after the boxes already drawn, with No repository last", () => {
    const fleet = screenshotFleet()
    const step = stepper(fleet.data)
    step(fleet.agents)
    const data: AgentGraphData = { ...fleet.data, agents: { ...fleet.data.agents, "X-1": { repo: "aaa/first" }, "Y-1": {} } }
    const after = step([...fleet.agents, agent("X-1"), agent("Y-1")], {}, data)
    expect(after.boxes.map((box) => box.label)).toEqual(["cpave3/autopilot", "cpave3/chimera", "cpave3/legato", "aaa/first", "No repository"])
  })

  it("leaves out an agent the graph has not read yet, rather than flash it in No repository", () => {
    const fleet = screenshotFleet()
    const layout = lay([...fleet.agents, agent("LEG-620")], fleet.data, { pending: new Set(["LEG-620"]) })
    expect(layout.nodes.some((n) => n.id === "LEG-620")).toBe(false)
    expect(layout.boxes.some((box) => box.label === "No repository")).toBe(false)
  })

  it("keeps an agent in its box when a later read loses its place", () => {
    const fleet = screenshotFleet()
    const step = stepper(fleet.data)
    const before = step(fleet.agents)
    const rest = Object.fromEntries(Object.entries(fleet.data.agents).filter(([id]) => id !== "LEG-613"))
    const after = step(fleet.agents, {}, { ...fleet.data, agents: rest })
    expect(node(after, "LEG-613").box).toBe("box:cpave3/legato")
    expect(at(after, "LEG-613")).toEqual(at(before, "LEG-613"))
  })

  it("gives the same layout when laid out again with the memory it left", () => {
    const fleet = busyFleet(NOW)
    const once = lay(fleet.agents, fleet.data)
    const twice = lay(fleet.agents, fleet.data, { memory: once.memory })
    expect(geometryOf(twice)).toEqual(geometryOf(once))
    expect(twice.memory).toEqual(once.memory)
  })
})

describe("the order of a box's agents", () => {
  it("puts a reviewer under its author, and agents of one run together, on a first draw", () => {
    const layout = lay(screenshotFleet().agents, screenshotFleet().data)
    expect(node(layout, "CHIM-97_reviewer").y).toBe(node(layout, "CHIM-97").y + 84)
    expect(node(layout, "CHIM-101").y).toBeGreaterThan(node(layout, "CHIM-97_reviewer").y)

    const graph = data({
      agents: {
        tri: { workflows: [{ run_id: "wr-1", step_id: "triage", role: "agent" }] },
        impl: { workflows: [{ run_id: "wr-1", step_id: "implement", role: "agent" }] },
      },
      runs: { "wr-1": run("wr-1") },
    })
    const runLayout = lay([
      agent("tri", { started_at: "2026-11-23T08:00:00Z" }), agent("other", { started_at: "2026-11-23T08:10:00Z" }), agent("impl", { started_at: "2026-11-23T08:20:00Z" }),
    ], graph)
    expect(node(runLayout, "impl").y).toBe(node(runLayout, "tri").y + 84)
  })

  it("puts a swarm's workers under their conductor", () => {
    const layout = lay([
      agent("p", { role: "conductor", parent_task_id: "p", started_at: "2026-11-23T08:00:00Z" }),
      agent("lone", { started_at: "2026-11-23T08:05:00Z" }),
      agent("st-1", { parent_task_id: "p", subtask_id: "st-1", started_at: "2026-11-23T08:10:00Z" }),
    ])
    expect(node(layout, "st-1").y).toBe(node(layout, "p").y + 84)
  })
})
