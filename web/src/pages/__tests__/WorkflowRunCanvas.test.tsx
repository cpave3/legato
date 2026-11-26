import { QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient, queryKeys } from "../../lib/query"
import type { WorkflowRunView } from "../../lib/workflow"
import { mockReactFlow } from "../../test/reactFlow"
import { WorkflowRunPage } from "../WorkflowRunPage"

// LEG-651: on a wide screen the run's graph is the page, and the run's
// details are cards over it.
mockReactFlow()

const { fetchRun } = vi.hoisted(() => ({ fetchRun: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: fetchRun }))

const issue = "When a run fails partway and someone presses Retry in Slack, the bot's replies stop landing in the original thread. The first message of the retried run goes to the channel itself, so the people who reported it never see it."
const steps = [
  { id: "read", title: "Read the thread", type: "chat.thread.read" },
  { id: "write", title: "Write the ticket", type: "agent.run" },
  { id: "post", title: "Post the PR link", type: "chat.message.send" },
]
const graph = {
  version: 1,
  nodes: [
    { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
    ...steps.map(step => ({ id: `step:${step.id}`, kind: "step", step: { ...step, start: "automatic", key: {}, definition: step } })),
  ],
  edges: [
    { id: "start:trigger:manual>step:read", kind: "start", source: "trigger:manual", target: "step:read" },
    { id: "order:step:read>step:write", kind: "order", source: "step:read", target: "step:write" },
    { id: "order:step:write>step:post", kind: "order", source: "step:write", target: "step:post" },
  ],
}
const stepRun = (index: number, status: string, extra = {}) => ({ run_id: "wr-1", step_id: steps[index].id, step_order: index, title: steps[index].title, executor_type: steps[index].type, definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: status === "pending" ? 0 : 1, started_at: status === "pending" ? undefined : "2026-11-21T10:00:00Z", ...extra })
const data = {
  run: { id: "wr-1", workflow_name: "autopilot-fix", workflow_title: "Autopilot fix from Slack", definition_source: "/w/autopilot-fix.workflow.yaml", definition_digest: "d", definition_json: JSON.stringify({ title: "Fix an autopilot issue from Slack" }), inputs_json: JSON.stringify({ channel: "#autopilot-issues", issue_text: issue }), status: "running", current_step_id: "write", created_at: "2026-11-21T10:00:00Z", started_at: "2026-11-21T10:00:00Z" },
  steps: [stepRun(0, "succeeded", { outputs_json: JSON.stringify({ transcript: "11 messages" }) }), stepRun(1, "running"), stepRun(2, "pending")],
  attempts: [],
  artifacts: [],
  details: null,
  trigger_kind: "manual",
  graph,
}

function show() {
  fetchRun.mockResolvedValue(data)
  const client = createQueryClient()
  client.setQueryData(queryKeys.workflowRun("", "wr-1"), data as unknown as WorkflowRunView)
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
}

// screen is a wide screen; tall makes it a large one too.
function screenSize(tall: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("min-width") && (tall || !query.includes("min-height")), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
}
const card = (name: string) => screen.getByRole("region", { name })
const fold = (name: string) => within(card(name)).getAllByRole("button")[0]

beforeEach(() => {
  cleanup()
  localStorage.clear()
  fetchRun.mockReset()
  screenSize(true)
})
afterEach(() => { vi.unstubAllGlobals() })

describe("the run page as a canvas", () => {
  it("draws the graph as the page, with the run's details as cards over it", async () => {
    show()
    expect(await screen.findByRole("button", { name: "Step: Write the ticket, Running" })).toBeTruthy()
    const canvas = screen.getByRole("region", { name: "Run graph" })
    for (const name of ["Now", "Inputs", "Steps"]) expect(within(canvas).getByRole("region", { name })).toBeTruthy()
  })

  it("puts the run's id and file in the header", async () => {
    show()
    const header = (await screen.findByRole("heading", { level: 1, name: "Autopilot fix from Slack" })).closest("header")!
    expect(header.textContent).toContain("wr-1")
    expect(header.textContent).toContain("autopilot-fix")
    expect(within(header).getByRole("button", { name: "Cancel run" })).toBeTruthy()
  })

  it("folds a card to its title, and opens it again", async () => {
    show()
    await screen.findByRole("region", { name: "Now" })
    expect(fold("Now").getAttribute("aria-expanded")).toBe("true")
    fireEvent.click(fold("Now"))
    expect(fold("Now").getAttribute("aria-expanded")).toBe("false")
    expect(card("Now").textContent).toContain("Write the ticket")
    fireEvent.click(fold("Now"))
    expect(fold("Now").getAttribute("aria-expanded")).toBe("true")
  })

  it("remembers a card folded, the next time the page opens", async () => {
    show()
    fireEvent.click(fold("Inputs"))
    cleanup()
    show()
    expect(fold("Inputs").getAttribute("aria-expanded")).toBe("false")
  })

  it("opens Inputs on a large screen and folds it on a smaller one", () => {
    show()
    expect(fold("Inputs").getAttribute("aria-expanded")).toBe("true")
    cleanup()
    screenSize(false)
    show()
    expect(fold("Inputs").getAttribute("aria-expanded")).toBe("false")
  })

  it("cuts a long input to three lines, with Show all", () => {
    show()
    const inputs = card("Inputs")
    expect(inputs.textContent).toContain("#autopilot-issues")
    fireEvent.click(within(inputs).getByRole("button", { name: "Show all" }))
    expect(within(inputs).getByRole("button", { name: "Show less" })).toBeTruthy()
  })

  it("lists the steps in Steps; choosing one shows its details beside the graph", async () => {
    show()
    expect(fold("Steps").getAttribute("aria-expanded")).toBe("false")
    fireEvent.click(fold("Steps"))
    fireEvent.click(within(card("Steps")).getByRole("button", { name: /Read the thread/ }))
    expect((await screen.findByRole("complementary", { name: "Read the thread" })).textContent).toContain("11 messages")
  })

  it("closes a step's details with Close or Escape", async () => {
    show()
    fireEvent.click(await screen.findByRole("button", { name: "Step: Write the ticket, Running" }))
    fireEvent.click(within(screen.getByRole("complementary", { name: "Write the ticket" })).getByRole("button", { name: "Close" }))
    expect(screen.queryByRole("complementary", { name: "Write the ticket" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Step: Write the ticket, Running" }))
    fireEvent.keyDown(document, { key: "Escape" })
    expect(screen.queryByRole("complementary", { name: "Write the ticket" })).toBeNull()
  })

  it("hides every card with Panels or the . key, and brings them back", async () => {
    show()
    fireEvent.click(await screen.findByRole("button", { name: "Step: Write the ticket, Running" }))
    const panels = screen.getByRole("button", { name: "Panels" })
    expect(panels.getAttribute("aria-pressed")).toBe("true")
    fireEvent.click(panels)
    expect(screen.queryByRole("region", { name: "Now" })).toBeNull()
    expect(screen.queryByRole("region", { name: "Inputs" })).toBeNull()
    expect(screen.queryByRole("complementary", { name: "Write the ticket" })).toBeNull()
    fireEvent.keyDown(document, { key: "." })
    expect(screen.getByRole("region", { name: "Now" })).toBeTruthy()
    expect(screen.getByRole("complementary", { name: "Write the ticket" })).toBeTruthy()
  })

  it("links a step that waits on its child run to it, under its node", async () => {
    const waiting = { ...data, steps: [stepRun(0, "succeeded"), stepRun(1, "waiting", { executor_type: "workflow.run", child_run_id: "wr-child" }), stepRun(2, "pending")], details: { write: { run_id: "wr-child", workflow: "autopilot-one-off", title: "Autopilot one-off", status: "running" } } }
    fetchRun.mockResolvedValue(waiting)
    const client = createQueryClient()
    client.setQueryData(queryKeys.workflowRun("", "wr-1"), waiting as unknown as WorkflowRunView)
    render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
    const chip = await screen.findByRole("link", { name: "Child run: Autopilot one-off, running" })
    expect(chip.getAttribute("href")).toBe("/workflows/runs/wr-child")
  })

  it("lets Enter open the child run from its link, without selecting the step", async () => {
    const waiting = { ...data, steps: [stepRun(0, "succeeded"), stepRun(1, "waiting", { executor_type: "workflow.run", child_run_id: "wr-child" }), stepRun(2, "pending")], details: { write: { run_id: "wr-child", workflow: "autopilot-one-off", title: "Autopilot one-off", status: "running" } } }
    fetchRun.mockResolvedValue(waiting)
    const client = createQueryClient()
    client.setQueryData(queryKeys.workflowRun("", "wr-1"), waiting as unknown as WorkflowRunView)
    render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
    const chip = await screen.findByRole("link", { name: "Child run: Autopilot one-off, running" })
    // fireEvent returns false when a handler cancelled the key's default.
    expect(fireEvent.keyDown(chip, { key: "Enter" })).toBe(true)
    expect(screen.queryByRole("complementary", { name: "Write the ticket" })).toBeNull()
  })

  it("opens on a step inside nested loops, centred in the free area", async () => {
    const loop = (path: string, title: string, parent?: string) => ({ id: `step:${path}`, kind: "step", ...(parent ? { parent } : {}), step: { id: path.split("/").pop(), path, title, type: "loop", group: "loop", start: "automatic", key: {}, loop: { form: "over", max: 5 }, definition: { id: path.split("/").pop(), title } } })
    const step = (path: string, title: string, parent?: string) => ({ id: `step:${path}`, kind: "step", ...(parent ? { parent } : {}), step: { id: path.split("/").pop(), path, title, type: "task.note", start: "automatic", key: {}, definition: { id: path.split("/").pop(), title } } })
    const before = ["one", "two", "three", "four", "five"]
    const nested = {
      version: 2,
      nodes: [
        { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
        ...before.map(id => step(id, `Step ${id}`)),
        loop("outer", "Each milestone"),
        loop("outer/inner", "Each ticket", "step:outer"),
        step("outer/inner/prep", "Prepare", "step:outer/inner"),
        step("outer/inner/build", "Build it", "step:outer/inner"),
      ],
      edges: [
        { id: "start:trigger:manual>step:one", kind: "start", source: "trigger:manual", target: "step:one" },
        ...before.slice(1).map((id, i) => ({ id: `order:${before[i]}>${id}`, kind: "order", source: `step:${before[i]}`, target: `step:${id}` })),
        { id: "order:five>outer", kind: "order", source: "step:five", target: "step:outer" },
        { id: "order:prep>build", kind: "order", source: "step:outer/inner/prep", target: "step:outer/inner/build" },
      ],
    }
    const view = { ...data, run: { ...data.run, current_step_id: "outer/0/inner/1/build" }, steps: [], graph: nested }
    fetchRun.mockResolvedValue(view)
    const client = createQueryClient()
    client.setQueryData(queryKeys.workflowRun("", "wr-1"), view as unknown as WorkflowRunView)
    render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
    const node = (await screen.findByRole("button", { name: /^Step: Build it/ })).closest<HTMLElement>(".react-flow__node")!
    const viewport = document.querySelector<HTMLElement>(".react-flow__viewport")!
    await waitFor(() => expect(viewport.style.transform).toContain("scale(0.8)"))
    const [tx, ty] = [...viewport.style.transform.matchAll(/(-?[\d.]+)px/g)].map(match => Number(match[1]))
    const [ax, ay] = [...node.style.transform.matchAll(/(-?[\d.]+)px/g)].map(match => Number(match[1]))
    // jsdom measures no frame, so the free area is one pixel wide, just past
    // the cards' column and its padding (392 + 24).
    expect(tx + (ax + parseFloat(node.style.width) / 2) * 0.8).toBeCloseTo(416.5, 0)
    expect(ty + (ay + parseFloat(node.style.height) / 2) * 0.8).toBeCloseTo(24.5, 0)
  })

  it("offers Fit and Where it is under the graph", async () => {
    show()
    await screen.findByRole("button", { name: "Step: Write the ticket, Running" })
    expect(screen.getByRole("button", { name: "Fit the whole flow" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Where it is" })).toBeTruthy()
  })
})
