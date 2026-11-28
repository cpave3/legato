import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { SubagentActivity } from "../SubagentParts"

afterEach(cleanup)

describe("SubagentActivity", () => {
  it("names a subagent's MCP call by its server and tool, the raw name in its tooltip", () => {
    render(<SubagentActivity subagent={{
      subagentId: "s1", purpose: "Find the task", status: "running",
      activities: [{ key: "a1", toolName: "mcp__burnlite__get_task", text: "mcp__burnlite__get_task: LEG-1" }],
    }} />)

    const name = screen.getByText("Burnlite › Get task")
    expect(name.getAttribute("title")).toBe("mcp__burnlite__get_task")
    expect(screen.getByText("LEG-1")).toBeTruthy()
  })
})
