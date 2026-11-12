import { describe, expect, it } from "vitest"
import { reeveName } from "../reeveName"
import { approvalKind, type Approval } from "../approvals"

describe("reeveName", () => {
  it("is the Reeve wherever it falls in a sentence when it has no name", () => {
    for (const none of [undefined, null, "", "   "]) {
      expect(reeveName(none)).toEqual({ name: "", label: "Reeve", start: "The Reeve", mid: "the Reeve" })
    }
  })

  it("is the name itself, at the start of a sentence and in the middle", () => {
    expect(reeveName(" Tessitura ")).toEqual({ name: "Tessitura", label: "Tessitura", start: "Tessitura", mid: "Tessitura" })
    const n = reeveName("Tessitura")
    expect(`${n.start} asks for a secret`).toBe("Tessitura asks for a secret")
    expect(`Change ${n.mid}'s seed`).toBe("Change Tessitura's seed")
  })
})

describe("approvalKind", () => {
  const approval = (source: string, details: Record<string, unknown> = {}) => ({ source, details } as unknown as Approval)
  it("says the name of the Reeve that asked", () => {
    expect(approvalKind(approval("reeve_rules", { reeve_name: "Tessitura" }))).toBe("Tessitura's rules")
    expect(approvalKind(approval("reeve_secret", { reeve_name: "Tessitura" }))).toBe("Secret for Tessitura")
    expect(approvalKind(approval("reeve_seed"))).toBe("The Reeve's seed")
  })
})

describe("approvalNotice", () => {
  it("names the Reeve that asks, not its hidden task", async () => {
    const { approvalNotice } = await import("../approvalNotifications")
    const notice = approvalNotice({ source: "reeve_change_set", task_id: "rv1", title: "Restart Legato", details: { reeve_name: "Tessitura" } } as unknown as Approval)
    expect(notice.body).toBe("Tessitura")
  })
})
