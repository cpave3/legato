import { describe, expect, it } from "vitest"
import { splitRelated, relatedStatusLabel, relatedStatusNote } from "../relatedWork"
import type { RelatedWorkItem } from "../task-activity"

const item = (kind: RelatedWorkItem["kind"], status: string): RelatedWorkItem => ({ kind, id: `${kind}-${status}`, title: status, status, href: "/" })

describe("splitRelated", () => {
  it("puts what is still under way apart from what is linked", () => {
    const items = [item("review", "capturing"), item("plan", "proposed"), item("workflow_run", "running"), item("workflow_run", "succeeded"), item("workflow_run", "waiting_manual")]
    const { inProgress, linked } = splitRelated(items)
    expect(inProgress.map((entry) => entry.status)).toEqual(["capturing", "running", "waiting_manual"])
    expect(linked.map((entry) => entry.status)).toEqual(["proposed", "succeeded"])
  })
})

describe("relatedStatusLabel", () => {
  it("writes a status as words", () => {
    expect(relatedStatusLabel("changes_requested")).toBe("changes requested")
  })
})

describe("a suspended run", () => {
  it("is still under way, and says how it goes on", () => {
    expect(splitRelated([item("workflow_run", "suspended")]).inProgress).toHaveLength(1)
    expect(relatedStatusNote(item("workflow_run", "suspended"))).toBe("Suspended: Legato stopped while its agent was working. Resume it to go on.")
  })
})
