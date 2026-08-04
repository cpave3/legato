import { fireEvent, render, screen } from "@testing-library/react"
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
})
