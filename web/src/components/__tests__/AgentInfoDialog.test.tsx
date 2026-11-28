import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AgentInfoDialog } from "../AgentInfoDialog"
import type { AgentInfo } from "../../lib/agentWire"

const { attachedResources } = vi.hoisted(() => ({ attachedResources: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/resources", () => ({ attachedResources }))

function agent(overrides: Partial<AgentInfo> = {}): AgentInfo {
  return {
    id: 1,
    task_id: "TASK-1",
    task_title: "Fix the widget",
    tmux_session: "legato-TASK-1",
    command: "claude",
    agent_kind: "claude-code",
    status: "running",
    activity: "working",
    started_at: "2026-11-16T03:00:00Z",
    working_seconds: 0,
    waiting_seconds: 0,
    error_seconds: 0,
    execution_id: "ex-abc123",
    sandboxed: true,
    working_dir: "/home/me/code/widget",
    ...overrides,
  }
}

const field = (label: string) => screen.getByLabelText(label)

beforeEach(() => {
  attachedResources.mockReset()
  attachedResources.mockResolvedValue(["notes"])
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true })
})

describe("AgentInfoDialog", () => {
  it("shows the configured account separately from the agent tool", async () => {
    render(<AgentInfoDialog agent={agent({ account: "work", account_label: "Work subscription" })} onClose={() => {}} />)
    expect(field("Account").textContent).toBe("Work subscription")
    expect(field("Agent tool").textContent).toBe("Claude Code")
    expect(await screen.findByText("notes")).toBeTruthy()
  })

  it("shows what Legato knows about the agent", async () => {
    render(<AgentInfoDialog agent={agent()} onClose={() => {}} />)
    const dialog = screen.getByRole("dialog", { name: "Agent info" })
    expect(within(dialog).getByText("Fix the widget")).toBeTruthy()
    expect(field("Task").textContent).toBe("TASK-1")
    expect(within(dialog).getByText("Claude Code")).toBeTruthy()
    expect(within(dialog).getByText("In the sandbox")).toBeTruthy()
    expect(field("Execution").textContent).toBe("ex-abc123")
    expect(field("tmux session").textContent).toBe("legato-TASK-1")
    expect(field("Working directory").textContent).toBe("/home/me/code/widget")
    expect(await within(dialog).findByText("notes")).toBeTruthy()
    expect(attachedResources).toHaveBeenCalledWith("", "TASK-1")
  })

  it("names the job clone an agent works in", () => {
    render(<AgentInfoDialog agent={agent({ working_dir: "/data/jobs/ex-1/widget", clone: { repo: "acme/widget", branch: "legato/task-1" } })} onClose={() => {}} />)
    expect(field("Working directory").textContent).toBe("/data/jobs/ex-1/widget")
    expect(field("Repository").textContent).toBe("acme/widget")
    expect(field("Branch").textContent).toBe("legato/task-1")
    expect(screen.getByText(/job clone/i)).toBeTruthy()
  })

  it("says when no directory is recorded", () => {
    render(<AgentInfoDialog agent={agent({ working_dir: undefined })} onClose={() => {}} />)
    expect(screen.queryByRole("button", { name: "Copy working directory" })).toBeNull()
    expect(screen.getByText(/no working directory is recorded/i)).toBeTruthy()
  })

  it("says the Reeve's directory is its own folder", () => {
    render(<AgentInfoDialog agent={agent({ reeve: true, working_dir: "/data/reeve/work" })} onClose={() => {}} />)
    expect(field("Working directory").textContent).toBe("/data/reeve/work")
    expect(screen.getByText(/the reeve's own folder/i)).toBeTruthy()
  })

  it("copies the exact working directory", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    const path = "/home/me/a very/long path/with spaces/and-more/widget"
    render(<AgentInfoDialog agent={agent({ working_dir: path })} onClose={() => {}} />)
    fireEvent.click(screen.getByRole("button", { name: "Copy working directory" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(path))
    expect(await screen.findByRole("button", { name: "Copied" })).toBeTruthy()
  })

  it("selects the path and says so where the browser has no clipboard", async () => {
    // A page not served over a safe connection has no clipboard.
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true })
    render(<AgentInfoDialog agent={agent()} onClose={() => {}} />)
    fireEvent.click(screen.getByRole("button", { name: "Copy working directory" }))
    expect(await screen.findByRole("button", { name: "Could not copy" })).toBeTruthy()
    expect(screen.getByRole("status").textContent).toMatch(/selected/i)
    expect(window.getSelection()?.toString()).toBe("/home/me/code/widget")
  })

  it("closes on Escape", () => {
    const onClose = vi.fn()
    render(<AgentInfoDialog agent={agent()} onClose={onClose} />)
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" })
    expect(onClose).toHaveBeenCalled()
  })
})
