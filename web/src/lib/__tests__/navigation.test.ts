import { describe, expect, it } from "vitest"
import { legatoHref } from "../navigation"

describe("legatoHref", () => {
  it("maps plan navigation to the plan route", () => {
    expect(legatoHref({ kind: "plan", target: "pl-1" })).toBe("/plans/pl-1")
  })

  it("url-encodes the plan id", () => {
    expect(legatoHref({ kind: "plan", target: "pl/with slash" })).toBe("/plans/pl%2Fwith%20slash")
  })

  it("maps review navigation to the review tour route", () => {
    expect(legatoHref({ kind: "review", target: "rt-1" })).toBe("/review/rt-1")
  })

  it("maps workflow_run navigation to the workflow run route", () => {
    expect(legatoHref({ kind: "workflow_run", target: "wr-1" })).toBe("/workflows/runs/wr-1")
  })

  it("maps task navigation to the board", () => {
    expect(legatoHref({ kind: "task", target: "T-1" })).toBe("/board")
  })

  it("returns null for unknown navigation kinds", () => {
    expect(legatoHref({ kind: "unknown", target: "x" })).toBeNull()
  })
})
