import { describe, expect, it } from "vitest"
import type { Approval } from "../approvals"
import { approvalReason, approvalRunText } from "../approvalCard"

function approval(source: string, details: Approval["details"] = {}, summary?: string): Approval {
  return { id: "a", source, source_key: "k", title: "t", summary, details, options: [], blocking: false, status: "pending", revision: 1, created_at: "" }
}

describe("approvalCard", () => {
  it("shows the command, else the summary, for a kind without a body of its own", () => {
    expect(approvalRunText(approval("agent_permission", { command: "ls" }, "List"))).toBe("ls")
    expect(approvalRunText(approval("agent_permission", {}, "List"))).toBe("List")
    expect(approvalRunText(approval("agent_permission"))).toBeUndefined()
  })

  it("shows no command for a plan or a kind with its own body", () => {
    expect(approvalRunText(approval("agent_permission", { command: "ls", plan: "1." }))).toBeUndefined()
    for (const source of ["credential", "reeve_change_set", "reeve_secret"]) {
      expect(approvalRunText(approval(source, { command: "ls" }, "List"))).toBeUndefined()
    }
  })

  it("shows the reason unless the credential or change set body does", () => {
    expect(approvalReason(approval("agent_permission", { reason: "why" }))).toBe("why")
    expect(approvalReason(approval("reeve_secret", { reason: "why" }))).toBe("why")
    expect(approvalReason(approval("credential", { reason: "why" }))).toBeUndefined()
    expect(approvalReason(approval("reeve_change_set", { reason: "why" }))).toBeUndefined()
    expect(approvalReason(approval("agent_permission"))).toBeUndefined()
  })
})
