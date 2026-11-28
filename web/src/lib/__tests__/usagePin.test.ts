import { afterAll, beforeAll, describe, expect, it } from "vitest"
import { pinDraftStart, pinRequest } from "../usagePin"
import type { UsagePolicy } from "../usage"

const zone = process.env.TZ
beforeAll(() => { process.env.TZ = "UTC" })
afterAll(() => { process.env.TZ = zone })

const policy = (pin?: UsagePolicy["active"]["pin"]): UsagePolicy => ({
  active: { profile: { name: "Quality", agent: "claude-code" }, reason: { text: "Auto" }, since: "2026-11-26T08:00:00Z", pin },
  profiles: [{ name: "Economy", agent: "codex" }, { name: "Quality", agent: "claude-code" }],
  switches: [],
} as UsagePolicy)

describe("pinDraftStart", () => {
  it("starts at the profile the address names when the policy has it", () => {
    expect(pinDraftStart(policy(), "Economy", ["claude.weekly"])).toEqual({ profile: "Economy", expiry: "indefinite", until: "", meter: "claude.weekly" })
  })
  it("starts at the pinned profile, else the active one", () => {
    expect(pinDraftStart(policy({ profile: "Economy" }), "Missing", []).profile).toBe("Economy")
    expect(pinDraftStart(policy(), null, []).profile).toBe("Quality")
  })
  it("keeps the pin's time, in local time, or its limit", () => {
    expect(pinDraftStart(policy({ profile: "Economy", until: "2026-11-27T12:30:00Z" }), null, [])).toMatchObject({ expiry: "time", until: "2026-11-27T12:30" })
    expect(pinDraftStart(policy({ profile: "Economy", until_reset: "codex.weekly" }), null, ["claude.weekly"])).toMatchObject({ expiry: "reset", meter: "codex.weekly" })
  })
})

describe("pinRequest", () => {
  const draft = { profile: "Economy", expiry: "indefinite" as const, until: "2026-11-27T12:00", meter: "codex.weekly" }
  it("sends the profile, and only the expiry chosen", () => {
    expect(pinRequest(draft)).toEqual({ profile: "Economy" })
    expect(pinRequest({ ...draft, expiry: "time" })).toEqual({ profile: "Economy", until: "2026-11-27T12:00:00.000Z" })
    expect(pinRequest({ ...draft, expiry: "reset" })).toEqual({ profile: "Economy", until_reset: "codex.weekly" })
  })
})
