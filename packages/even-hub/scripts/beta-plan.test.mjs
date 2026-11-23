import { describe, expect, it } from "vitest"
import { nextVersion, parseArgs } from "./beta-plan.mjs"

describe("nextVersion", () => {
  it("bumps the patch number", () => {
    expect(nextVersion("0.1.0")).toBe("0.1.1")
    expect(nextVersion("1.2.9")).toBe("1.2.10")
  })

  it("refuses what is not x.y.z", () => {
    expect(() => nextVersion("0.1")).toThrow(/x\.y\.z/)
    expect(() => nextVersion("0.1.0-beta")).toThrow(/x\.y\.z/)
  })
})

describe("parseArgs", () => {
  it("takes a changelog, a version and whether to create the app", () => {
    expect(parseArgs(["-m", "Talk to the Reeve", "--version", "0.3.0", "--create"])).toEqual({
      changelog: "Talk to the Reeve",
      version: "0.3.0",
      create: true,
      help: false,
    })
    expect(parseArgs([])).toEqual({ create: false, help: false })
    expect(parseArgs(["--help"]).help).toBe(true)
  })

  it("skips the separator pnpm passes on", () => {
    expect(parseArgs(["--", "-m", "x"]).changelog).toBe("x")
  })

  it("refuses an unknown flag, a missing value, a bad version and a changelog over 500 characters", () => {
    expect(() => parseArgs(["--channel", "public"])).toThrow(/--channel/)
    expect(() => parseArgs(["-m"])).toThrow(/-m/)
    expect(() => parseArgs(["--version", "1"])).toThrow(/x\.y\.z/)
    expect(() => parseArgs(["-m", "x".repeat(501)])).toThrow(/500/)
  })
})
