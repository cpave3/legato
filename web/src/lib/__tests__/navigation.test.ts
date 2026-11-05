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

  it("maps task navigation to the board, with the task picked out", () => {
    expect(legatoHref({ kind: "task", target: "T-1" })).toBe("/board?highlight_task=T-1")
    expect(legatoHref({ kind: "task", target: "a b" })).toBe("/board?highlight_task=a%20b")
  })

  it("maps settings navigation to that section of Settings", () => {
    expect(legatoHref({ kind: "settings", target: "devices" })).toBe("/settings#devices")
  })

  it("returns null for unknown navigation kinds", () => {
    expect(legatoHref({ kind: "unknown", target: "x" })).toBeNull()
  })
})

describe("externalHref", () => {
  it("gives an external navigation's http(s) address and nothing else", async () => {
    const { externalHref } = await import("../navigation")
    expect(externalHref({ kind: "external", target: "https://argus.lan/cases/1" })).toBe("https://argus.lan/cases/1")
    expect(externalHref({ kind: "external", target: "javascript:alert(1)" })).toBeNull()
    expect(externalHref({ kind: "external", target: "not a url" })).toBeNull()
    expect(externalHref({ kind: "task", target: "https://argus.lan/cases/1" })).toBeNull()
  })
})
