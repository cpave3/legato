import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { PlanApprovalModal } from "../PlanApprovalModal"
import type { PendingPlanData } from "../../lib/swarm"

type Listener = (msg: Record<string, unknown>) => void
const mocks = vi.hoisted(() => ({
  listeners: [] as ((msg: Record<string, unknown>) => void)[],
  send: vi.fn(),
  addToast: vi.fn(),
  plans: vi.fn(),
  connected: { current: true },
}))
vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    subscribe: (listener: Listener) => { mocks.listeners.push(listener); return () => { mocks.listeners = mocks.listeners.filter((one) => one !== listener) } },
    send: mocks.send,
    connected: mocks.connected.current,
  }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: mocks.addToast }) }))
vi.mock("../../lib/swarm", () => ({ getAllPendingPlans: () => mocks.plans() }))

afterEach(() => {
  cleanup()
  mocks.listeners = []
  mocks.send.mockReset()
  mocks.addToast.mockReset()
  mocks.plans.mockReset()
  mocks.connected.current = true
})

const full: PendingPlanData = {
  parent_task_id: "T1", plan_path: "/plan.yaml", reply_socket: "/reply",
  plan: {
    header: { parent_task_id: "T1", working_dir: "/repo", summary: "Build it" },
    subtasks: [
      { title: "Schema", role: "builder", agent: "claude-code", tier: "fast", scope: ["db/**", "store/**"], prompt: "Write the migration" },
      { title: "Docs", scope: [] },
    ],
  },
}
const emit = (msg: Record<string, unknown>) => act(() => { for (const listener of mocks.listeners) listener(msg) })

describe("PlanApprovalModal verdicts", () => {
  it("shows the plan's directory, summary and sub-tasks", async () => {
    mocks.plans.mockResolvedValue([full])
    render(<PlanApprovalModal />)
    const dialog = await screen.findByRole("dialog", { name: "Plan Proposed" })
    expect(within(dialog).getByText("T1")).toBeTruthy()
    expect(within(dialog).getByText("/repo")).toBeTruthy()
    expect(within(dialog).getByText("Build it")).toBeTruthy()
    expect(within(dialog).getByText("Sub-tasks (2)")).toBeTruthy()
    expect(within(dialog).getByText("Schema")).toBeTruthy()
    expect(within(dialog).getByText("builder")).toBeTruthy()
    expect(within(dialog).getByText("claude-code")).toBeTruthy()
    expect(within(dialog).getByText("fast")).toBeTruthy()
    expect(within(dialog).getByText("db/**, store/**")).toBeTruthy()
    expect(within(dialog).getByText("Write the migration")).toBeTruthy()
    expect(within(dialog).getByText("Docs")).toBeTruthy()
    expect(within(dialog).getAllByText(/^scope:/)).toHaveLength(1)
    expect(within(dialog).getAllByText(/^role:/)).toHaveLength(1)
  })

  it("says why a plan could not be loaded, or where it is when it was not read", async () => {
    mocks.plans.mockResolvedValue([{ parent_task_id: "T2", plan_path: "/bad.yaml", reply_socket: "/r", load_error: "bad yaml" }])
    render(<PlanApprovalModal />)
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toBe("Could not load plan: bad yamlPath: /bad.yaml")
    cleanup()
    mocks.plans.mockResolvedValue([{ parent_task_id: "T3", plan_path: "/later.yaml", reply_socket: "/r" }])
    render(<PlanApprovalModal />)
    expect(await screen.findByText("/later.yaml")).toBeTruthy()
    expect(screen.getByText(/Plan path:/)).toBeTruthy()
  })

  it("approves with a verdict over the socket", async () => {
    mocks.plans.mockResolvedValue([full])
    render(<PlanApprovalModal />)
    fireEvent.click(await screen.findByRole("button", { name: "Approve" }))
    expect(mocks.send).toHaveBeenCalledWith({ type: "plan_verdict", parent_task_id: "T1", plan_path: "/plan.yaml", reply_socket: "/reply", status: "approved", notes: "" })
    expect(mocks.addToast).toHaveBeenCalledWith("Plan approved", "success")
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("rejects only with notes, and goes back without deciding", async () => {
    mocks.plans.mockResolvedValue([full])
    render(<PlanApprovalModal />)
    fireEvent.click(await screen.findByRole("button", { name: "Reject" }))
    const notes = screen.getByRole("textbox", { name: "Rejection Notes" })
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Reject with Notes" }))
    expect(screen.getByRole("alert").textContent).toBe("Please provide notes for the rejection.")
    fireEvent.click(screen.getByRole("button", { name: "Back" }))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(screen.queryByRole("textbox")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Reject" }))
    expect(notes).not.toBe(screen.getByRole("textbox", { name: "Rejection Notes" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Rejection Notes" }), { target: { value: "  Split the schema  " } })
    fireEvent.click(screen.getByRole("button", { name: "Reject with Notes" }))
    expect(mocks.send).toHaveBeenCalledWith(expect.objectContaining({ status: "rejected", notes: "Split the schema" }))
    expect(mocks.addToast).toHaveBeenCalledWith("Plan rejected with notes", "error")
  })

  it("says so when the verdict cannot be sent", async () => {
    mocks.plans.mockResolvedValue([full])
    mocks.send.mockImplementation(() => { throw new Error("closed") })
    render(<PlanApprovalModal />)
    fireEvent.click(await screen.findByRole("button", { name: "Approve" }))
    expect(screen.getByRole("alert").textContent).toBe("Failed to send verdict. Please try again.")
    expect(screen.getByRole("dialog")).toBeTruthy()
  })

  it("names an extension plan, and closes when another surface decides it", async () => {
    mocks.plans.mockResolvedValue([])
    render(<PlanApprovalModal />)
    await waitFor(() => expect(mocks.plans).toHaveBeenCalled())
    mocks.plans.mockResolvedValue([full])
    emit({ type: "plan_proposed", parent_task_id: "T1", mode: "extension" })
    expect(await screen.findByRole("dialog", { name: "Extension plan — append to existing swarm" })).toBeTruthy()
    emit({ type: "swarm_changed", parent_task_id: "other", status: "rejected" })
    emit({ type: "swarm_changed", parent_task_id: "T1", status: "running" })
    expect(screen.getByRole("dialog")).toBeTruthy()
    emit({ type: "swarm_changed", parent_task_id: "T1", status: "plan_applied" })
    expect(screen.queryByRole("dialog")).toBeNull()
    expect(mocks.addToast).toHaveBeenCalledWith("Plan approved on another surface", "info")
  })

  it("keeps the notes being typed when the same plan is proposed again", async () => {
    mocks.plans.mockResolvedValue([full])
    render(<PlanApprovalModal />)
    fireEvent.click(await screen.findByRole("button", { name: "Reject" }))
    fireEvent.change(screen.getByRole("textbox", { name: "Rejection Notes" }), { target: { value: "half" } })
    emit({ type: "plan_proposed", parent_task_id: "T1" })
    await waitFor(() => expect(mocks.plans).toHaveBeenCalledTimes(2))
    expect(screen.getByRole("textbox", { name: "Rejection Notes" })).toHaveProperty("value", "half")
  })

  it("looks for plans again on reconnect", async () => {
    mocks.plans.mockResolvedValue([])
    mocks.connected.current = false
    const view = render(<PlanApprovalModal />)
    await waitFor(() => expect(mocks.plans).toHaveBeenCalledTimes(1))
    mocks.plans.mockResolvedValue([full])
    mocks.connected.current = true
    view.rerender(<PlanApprovalModal />)
    expect(await screen.findByRole("dialog", { name: "Plan Proposed" })).toBeTruthy()
  })
})
