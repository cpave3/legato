import { describe, expect, it, vi } from "vitest"
import type { ConfigSetting } from "../configSettings"
import { emptyPickerData, type PickerData } from "../settingPickers"
import {
  agentName, agentPartners, agentUnsetLabel, fallbackAgent, parseChoice, planAgent, planSecond, runPlan, secondChoice, secondOptions, unsetAgent,
} from "../agentPicker"

const setting = (key: string, value: unknown = ""): ConfigSetting => ({
  key, title: key, kind: "string", class: "change", mode: "restart", value, default: "", source: "default", pinned: false,
})

const data: PickerData = {
  ...emptyPickerData, defaultAgent: "claude-code",
  tiers: { "claude-code": ["deep"], codex: ["fast"] }, models: { "claude-code": ["opus"] },
}

describe("agent picker", () => {
  it("names agents and what unset means", () => {
    expect(agentName("codex")).toBe("Codex")
    expect(agentName("other")).toBe("other")
    expect(unsetAgent(undefined, "codex")).toBe("Default agent · Codex")
    expect(unsetAgent("claude-code", "")).toBe("Default agent")
    expect(unsetAgent("Ask each time", "codex")).toBe("Ask each time")
    expect(agentUnsetLabel({ kind: "agent", fallback: "w" }, undefined, "codex", "claude-code")).toBe("Fallback · Codex")
    expect(agentUnsetLabel({ kind: "agent", fallback: "w" }, "Workers' agent", "codex", "claude-code")).toBe("Workers' agent · Codex")
    expect(agentUnsetLabel({ kind: "agent" }, undefined, "codex", "claude-code")).toBe("Default agent · Claude Code")
  })

  it("finds the partners and the fallback agent", () => {
    const settings = { m: setting("m"), t: setting("t"), w: setting("w", "codex") }
    expect(agentPartners({ kind: "agent", tier: "t", model: "m" }, settings).second?.key).toBe("t")
    expect(agentPartners({ kind: "agent" }, settings).second).toBeUndefined()
    expect(fallbackAgent({ kind: "agent", fallback: "w" }, { ...data, settings })).toBe("codex")
    expect(fallbackAgent({ kind: "agent", fallback: "none" }, { ...data, settings })).toBe("claude-code")
    expect(fallbackAgent({ kind: "agent" }, data)).toBe("claude-code")
  })

  it("offers tiers and models as the keys hold them", () => {
    expect(secondOptions({ tier: setting("t") }, ["deep"], ["opus"])).toEqual([{ value: "tier:deep", label: "Tier: deep" }])
    expect(secondOptions({ model: setting("m") }, ["deep"], ["opus"])).toEqual([{ value: "model:opus", label: "opus" }])
    expect(secondOptions({ modelOrTier: setting("x") }, ["deep"], ["opus"])).toHaveLength(2)
  })

  it("reads the current model or tier", () => {
    expect(secondChoice({ modelOrTier: setting("x", "deep") }, ["deep"])).toEqual({ current: "deep", value: "tier:deep" })
    expect(secondChoice({ modelOrTier: setting("x", "opus") }, ["deep"])).toEqual({ current: "opus", value: "model:opus" })
    expect(secondChoice({ modelOrTier: setting("x") }, ["deep"])).toEqual({ current: "", value: "" })
    expect(secondChoice({ tier: setting("t", "deep"), model: setting("m") }, [])).toEqual({ current: "tier:deep", value: "tier:deep" })
    expect(secondChoice({ tier: setting("t"), model: setting("m", "opus") }, [])).toEqual({ current: "model:opus", value: "model:opus" })
    expect(secondChoice({}, [])).toEqual({ current: "", value: "" })
  })

  it("plans a model or tier", () => {
    expect(parseChoice("model:a:b")).toEqual(["model", "a:b"])
    expect(parseChoice("")).toEqual(["", ""])
    expect(planSecond({ modelOrTier: setting("x") }, "tier:deep")).toEqual({ resets: [], then: { key: "x", value: "deep" } })
    expect(planSecond({ modelOrTier: setting("x", "deep") }, "")).toEqual({ resets: [], then: { key: "x", value: undefined } })
    const two = { tier: setting("t", "deep"), model: setting("m", "opus") }
    expect(planSecond(two, "tier:fast")).toEqual({ resets: ["m"], then: { key: "t", value: "fast" } })
    expect(planSecond(two, "")).toEqual({ resets: ["t", "m"], then: undefined })
  })

  it("plans an agent, clearing what the new one lacks", () => {
    const partners = { modelOrTier: setting("x", "deep") }
    expect(planAgent(setting("a"), partners, "codex", "codex", data)).toEqual({ resets: ["x"], then: { key: "a", value: "codex" } })
    expect(planAgent(setting("a"), partners, "", "claude-code", data)).toEqual({ resets: [], then: { key: "a", value: undefined } })
  })

  it("runs a plan in order and stops at a refused clear", async () => {
    const calls: string[] = []
    const set = vi.fn(async (key: string) => { calls.push(`set ${key}`); return true })
    const reset = vi.fn(async (key: string) => { calls.push(`reset ${key}`); return key !== "refused" })
    await runPlan({ resets: ["a", "b"], then: { key: "c", value: "v" } }, set, reset)
    expect(calls).toEqual(["reset a", "reset b", "set c"])
    calls.length = 0
    await runPlan({ resets: ["refused"], then: { key: "c", value: "v" } }, set, reset)
    expect(calls).toEqual(["reset refused"])
    calls.length = 0
    await runPlan({ resets: [], then: { key: "c" } }, set, reset)
    await runPlan({ resets: [] }, set, reset)
    expect(calls).toEqual(["reset c"])
  })
})
