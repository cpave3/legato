import { describe, expect, it, vi } from "vitest"
import { dispatchAgentInput } from "../agentInput"

describe("dispatchAgentInput", () => {
  it("executes leading bang input without sending it as a message", () => {
    const executeBang = vi.fn()
    const sendMessage = vi.fn()

    dispatchAgentInput("!legato task show $LEGATO_TASK_ID --format full", { executeBang, sendMessage })

    expect(executeBang).toHaveBeenCalledWith("legato task show $LEGATO_TASK_ID --format full")
    expect(sendMessage).not.toHaveBeenCalled()
  })

  it("sends ordinary input as a message", () => {
    const executeBang = vi.fn()
    const sendMessage = vi.fn()

    dispatchAgentInput("show me the task", { executeBang, sendMessage })

    expect(sendMessage).toHaveBeenCalledWith("show me the task")
    expect(executeBang).not.toHaveBeenCalled()
  })
})
