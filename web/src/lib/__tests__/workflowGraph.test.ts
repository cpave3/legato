import { describe, expect, it } from "vitest"
import { keyValueText } from "../workflowGraph"

describe("keyValueText", () => {
  it("shortens each expression to what it names", () => {
    expect(keyValueText("Triage ${{ workflow.inputs.task.id }} now")).toBe("Triage {task.id} now")
    expect(keyValueText("${{ steps.triage.outputs.report.path }}")).toBe("{triage.report.path}")
    expect(keyValueText("${{workflow.task.title}}")).toBe("{task.title}")
    expect(keyValueText("Ready")).toBe("Ready")
  })
})
