import { render, screen, cleanup } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { GoToAgentButton } from "../GoToAgentButton"

vi.mock("../../hooks/useRunningAgent", () => ({
  useRunningAgent: vi.fn(),
}))

import { useRunningAgent } from "../../hooks/useRunningAgent"

afterEach(() => {
  cleanup()
  vi.mocked(useRunningAgent).mockReset()
})

describe("GoToAgentButton", () => {
  it("renders nothing when no agent is running", () => {
    vi.mocked(useRunningAgent).mockReturnValue({ agent: null, agents: [], loading: false })
    const { container } = render(<MemoryRouter><GoToAgentButton taskId="T-1" /></MemoryRouter>)
    expect(container.firstChild).toBeNull()
  })

  it("renders nothing while loading", () => {
    vi.mocked(useRunningAgent).mockReturnValue({ agent: null, agents: [], loading: true })
    const { container } = render(<MemoryRouter><GoToAgentButton taskId="T-1" /></MemoryRouter>)
    expect(container.firstChild).toBeNull()
  })

  it("renders a link to the agent when one is running", () => {
    vi.mocked(useRunningAgent).mockReturnValue({
      agent: { id: 1, task_id: "workflow-T-1", task_title: "Build feature", status: "running", activity: "working" } as never,
      agents: [{ id: 1, task_id: "workflow-T-1" }] as never,
      loading: false,
    })
    render(<MemoryRouter><GoToAgentButton taskId="T-1" /></MemoryRouter>)
    const link = screen.getByRole("link", { name: /go to agent/i })
    expect(link.getAttribute("href")).toBe("/agents?task=workflow-T-1")
  })

  it("links to the agents page without selection when several secondary agents run", () => {
    vi.mocked(useRunningAgent).mockReturnValue({
      agent: null,
      agents: [{ task_id: "workflow-1" }, { task_id: "workflow-2" }] as never,
      loading: false,
    })
    render(<MemoryRouter><GoToAgentButton taskId="T-1" /></MemoryRouter>)
    expect(screen.getByRole("link", { name: /go to agent/i }).getAttribute("href")).toBe("/agents")
  })

  it("shows the agent title as a tooltip", () => {
    vi.mocked(useRunningAgent).mockReturnValue({
      agent: { id: 1, task_id: "workflow-T-1", task_title: "Build feature", status: "running", activity: "working" } as never,
      agents: [{ id: 1, task_id: "workflow-T-1" }] as never,
      loading: false,
    })
    render(<MemoryRouter><GoToAgentButton taskId="T-1" /></MemoryRouter>)
    expect(screen.getByRole("link", { name: /go to agent/i }).getAttribute("title")).toContain("Build feature")
  })
})
