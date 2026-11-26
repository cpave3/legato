import { QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
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

  it("offers Fit and Where it is under the graph", async () => {
    show()
    await screen.findByRole("button", { name: "Step: Write the ticket, Running" })
    expect(screen.getByRole("button", { name: "Fit the whole flow" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Where it is" })).toBeTruthy()
  })
})
