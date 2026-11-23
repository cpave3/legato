import { describe, expect, it } from "vitest"
import { getTextWidth } from "@evenrealities/pretext"
import { ALERT_TEXT_WIDTH, COUNTS_TEXT_WIDTH, background } from "./background"
import type { Agent } from "./legato"

const agent = (activity: string, fields: Partial<Agent> = {}): Agent => ({
  task_id: "A",
  title: "",
  status: "running",
  activity,
  sandboxed: false,
  started_at: "2026-11-23T07:00:00Z",
  ...fields,
})

describe("background", () => {
  it("puts who needs the operator bright on the left, and the rest as counts dim and right-aligned on the right", () => {
    const page = background([agent("waiting"), agent("waiting"), agent("error"), agent("working"), agent("background"), agent(""), agent("")])

    expect(page.alert).toBe("● 2  ▲ 1")
    expect(page.counts.trim()).toBe("▶ 1  ▷ 1  ○ 2")
    expect(getTextWidth(page.counts)).toBeLessThanOrEqual(COUNTS_TEXT_WIDTH)
    expect(getTextWidth(page.counts)).toBeGreaterThan(COUNTS_TEXT_WIDTH - 2 * getTextWidth(" "))
  })

  it("leaves out what has none, and the bright side empty when no one needs the operator", () => {
    const page = background([agent("working"), agent("working"), agent("dead", { status: "dead" })])

    expect(page.alert).toBe("")
    expect(page.counts.trim()).toBe("▶ 2")
  })

  it("counts the Reeve only when it needs the operator", () => {
    expect(background([agent("working", { reeve: true })]).counts.trim()).toBe("no agents")
    expect(background([agent("waiting", { reeve: true })])).toEqual({ alert: "● 1", counts: "" })
  })

  it("says so, brightly, when the list can't be read", () => {
    expect(background([agent("working")], { problem: "Can't reach Legato" }).alert).toBe("▲ can't read")
  })

  it("ends the dim side with the glasses' battery, and puts it on the bright side when low", () => {
    expect(background([agent("working")], { battery: 82 }).counts.trim()).toBe("▶ 1  ·  G2 82%")
    expect(background([], { battery: 82 }).counts.trim()).toBe("no agents  ·  G2 82%")
    expect(background([agent("waiting")], { battery: 12 })).toEqual({
      alert: "● 1  ▲ G2 12%",
      counts: "",
    })
    expect(background([agent("working")], { battery: 12, charging: true }).counts.trim()).toBe("▶ 1  ·  G2 12%")
  })

  it("fits busy counts and a full battery, and a busy bright side, in the row", () => {
    const many = (activity: string, n: number) => Array.from({ length: n }, () => agent(activity))
    const page = background([...many("working", 12), ...many("background", 3), ...many("", 14)], { battery: 100 })

    expect(page.counts.trim()).toBe("▶ 12  ▷ 3  ○ 14  ·  G2 100%")
    expect(getTextWidth(page.counts)).toBeLessThanOrEqual(COUNTS_TEXT_WIDTH)
    const busy = background([...many("waiting", 12), ...many("error", 3)], { battery: 12 })
    expect(busy.alert).toBe("● 12  ▲ 3  ▲ G2 12%")
    expect(getTextWidth(busy.alert)).toBeLessThanOrEqual(ALERT_TEXT_WIDTH)
  })
})
