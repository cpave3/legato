import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { AgentSelect } from "../AgentSelect"
import type { AgentInfo } from "../../hooks/useWebSocket"

const errorAgent = {
  task_id: "task-error",
  task_title: "Broken agent",
  command: "chimera",
  activity: "error",
  working_seconds: 0,
  waiting_seconds: 0,
} as AgentInfo

describe("AgentSelect", () => {
  it("renders an error dot and label", () => {
    render(<AgentSelect agents={[errorAgent]} selectedId="task-error" onSelect={() => {}} />)
    expect(screen.getByTitle("Error").className).toContain("bg-red-500")
    fireEvent.click(screen.getByRole("button"))
    expect(screen.getByText("Error").className).toContain("text-red-400")
  })

  it("tells an agent waiting on background work from an idle one", () => {
    // Scoped to this render: the file's tests share one document.
    const view = within(render(<AgentSelect agents={[{ ...errorAgent, activity: "background" }]} selectedId="task-error" onSelect={() => {}} />).container)
    expect(view.getByTitle("Background work running").className).toContain("bg-sky-400")
    fireEvent.click(view.getByRole("button"))
    expect(view.getByText("Background").className).toContain("text-sky-300")
  })
})
