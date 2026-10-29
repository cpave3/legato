import { describe, expect, it } from "vitest"
import { foldAuditRows, type AuditRow } from "../audit"

const row = (seq: number, over: Partial<AuditRow> = {}): AuditRow => ({
  seq, at: `2026-10-29T22:39:${String(60 - seq).padStart(2, "0")}.000Z`, event: "use",
  credential: "github", operation: "api.read", resource: "github:autopilot-dev/autopilot",
  task_id: "AG-10102", task_title: "Comparable sales", ...over,
})

describe("foldAuditRows", () => {
  it("folds a run of the same use by the same agent into one row with a count and a span", () => {
    const folded = foldAuditRows([row(9), row(8), row(7), row(6, { task_id: "0ir7bojk" }), row(5)])
    expect(folded.map((f) => [f.first.seq, f.count])).toEqual([[9, 3], [6, 1], [5, 1]])
    expect(folded[0].last.seq).toBe(7)
  })

  it("never folds decisions: each grant, refusal or request is its own row", () => {
    const folded = foldAuditRows([row(3, { event: "grant" }), row(2, { event: "grant" }), row(1, { event: "refuse" })])
    expect(folded.map((f) => f.count)).toEqual([1, 1, 1])
  })
})
