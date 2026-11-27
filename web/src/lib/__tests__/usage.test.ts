import { afterAll, beforeAll, describe, expect, it } from "vitest"
import { agoShort, formatCredits, resetShort, sinceText, summaryProviders, tightestWindow, whenText, worstTone, type ProviderUsage } from "../usage"

// Times are read in the browser's zone; these tests pin it.
const zone = process.env.TZ
beforeAll(() => { process.env.TZ = "UTC" })
afterAll(() => { process.env.TZ = zone })

// A Friday, 14:00 UTC.
const NOW = new Date("2026-11-27T14:00:00Z")

describe("tightestWindow", () => {
  it("is the window closest to its limit", () => {
    expect(tightestWindow([
      { meter: "five_hour", used_percent: 50 },
      { meter: "weekly", used_percent: 20 },
      { meter: "monthly", used_percent: 10 },
    ])?.meter).toBe("five_hour")
    expect(tightestWindow([{ meter: "five_hour", used_percent: 42 }, { meter: "weekly", used_percent: 99 }])?.meter).toBe("weekly")
  })

  it("is nothing without windows", () => {
    expect(tightestWindow([])).toBeUndefined()
  })
})

describe("worstTone", () => {
  const provider = (percents: number[], extra: Partial<ProviderUsage> = {}): ProviderUsage => ({
    provider: "claude", state: "ok", stale: false,
    windows: percents.map((used_percent, i) => ({ meter: `m${i}`, used_percent })), ...extra,
  } as ProviderUsage)

  it("is the tone of the fullest window of any account", () => {
    expect(worstTone([provider([10, 40]), provider([20])])).toBe("calm")
    expect(worstTone([provider([10, 80]), provider([20])])).toBe("warn")
    expect(worstTone([provider([10, 80]), provider([95])])).toBe("over")
  })

  it("leaves out stale and unavailable readings", () => {
    expect(worstTone([provider([95], { stale: true }), provider([95], { state: "unavailable" }), provider([5])])).toBe("calm")
  })

  it("is calm with nothing read", () => {
    expect(worstTone([])).toBe("calm")
  })
})

describe("formatCredits", () => {
  it("groups thousands and keeps two decimals", () => {
    expect(formatCredits("1240.50382910", "en-US")).toBe("1,240.50")
    expect(formatCredits("7", "en-US")).toBe("7.00")
  })

  it("keeps a balance it cannot read as it came", () => {
    expect(formatCredits("about 40", "en-US")).toBe("about 40")
  })
})

describe("whenText", () => {
  it("is the time alone today", () => {
    expect(whenText("2026-11-27T09:05:00Z", NOW)).toBe("09:05")
  })

  it("names the day within a week either way", () => {
    expect(whenText("2026-11-30T09:00:00Z", NOW)).toBe("Mon 09:00")
    expect(whenText("2026-11-26T22:40:00Z", NOW)).toBe("Thu 22:40")
  })

  it("is the date further off", () => {
    // In the browser's own order: "Dec 9" or "9 Dec".
    expect(whenText("2026-12-09T00:00:00Z", NOW)).toBe(new Date("2026-12-09T00:00:00Z").toLocaleDateString(undefined, { month: "short", day: "numeric" }))
    expect(whenText("2026-12-09T00:00:00Z", NOW)).toMatch(/Dec/)
  })
})

describe("resetShort", () => {
  it("counts down within a day", () => {
    expect(resetShort("2026-11-27T16:10:00Z", NOW)).toBe("in 2h 10m")
  })

  it("names the day and time further off", () => {
    expect(resetShort("2026-11-30T09:00:00Z", NOW)).toBe("Mon 09:00")
  })

  it("says when a reset has passed", () => {
    expect(resetShort("2026-11-27T12:00:00Z", NOW)).toBe("reset 2h ago")
  })

  it("is empty without a reset", () => {
    expect(resetShort(undefined, NOW)).toBe("")
  })
})

describe("sinceText", () => {
  it("is the short time and how long ago", () => {
    expect(sinceText("2026-11-27T11:02:00Z", NOW)).toBe("11:02 · 2h 58m ago")
  })
})

describe("agoShort", () => {
  it("is the largest unit only", () => {
    expect(agoShort("2026-11-27T13:58:00Z", NOW)).toBe("2m")
    expect(agoShort("2026-11-27T10:55:00Z", NOW)).toBe("3h")
    expect(agoShort("2026-11-27T13:59:40Z", NOW)).toBe("now")
  })
})

describe("summaryProviders", () => {
  const usage = (provider: string, percent: number): ProviderUsage => ({ provider, state: "ok", stale: false, windows: [{ meter: "weekly", used_percent: percent }] })

  it("keeps every provider in server order while they fit", () => {
    const all = [usage("a", 10), usage("b", 90), usage("c", 50)]
    expect(summaryProviders(all, 4)).toEqual({ shown: all, hidden: [] })
  })

  it("past the limit keeps the most constrained, still in server order", () => {
    const all = [usage("a", 10), usage("b", 90), usage("c", 50), usage("d", 5), usage("e", 75), usage("f", 61)]
    const { shown, hidden } = summaryProviders(all, 4)
    expect(shown.map((p) => p.provider)).toEqual(["b", "c", "e", "f"])
    expect(hidden.map((p) => p.provider)).toEqual(["a", "d"])
  })

  it("treats an unavailable provider as unconstrained", () => {
    const all = [{ provider: "x", state: "unavailable" as const, stale: false, windows: [] }, usage("a", 1), usage("b", 2)]
    expect(summaryProviders(all, 2).hidden.map((p) => p.provider)).toEqual(["x"])
  })
})
