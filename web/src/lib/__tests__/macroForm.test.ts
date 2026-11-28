import { beforeEach, describe, expect, it, vi } from "vitest"
import {
  canSaveMacro,
  macroAccountsShown,
  macroAgentChoices,
  macroEffectiveAgent,
  macroFields,
  macroFormStart,
  macroNameStatus,
  macroTiers,
  persistMacro,
  sameMacroName,
  tierAfterAgent,
  unlistedAccount,
} from "../macroForm"

const { save, rename } = vi.hoisted(() => ({ save: vi.fn(), rename: vi.fn() }))
vi.mock("../macros", async (actual) => ({ ...await actual<typeof import("../macros")>(), saveMacro: save, renameMacro: rename }))

const adapters = { adapters: ["claude-code", "codex"], default: "codex", accounts: [{ name: "work", label: "Work", provider: "claude" }] }
const tiers = { codex: [{ name: "fast" }, { name: "smart" }], "claude-code": [{ name: "smart" }] }
const macros = [{ name: "Plan", keys: "p" }, { name: "Review", keys: "r" }]

it("starts with the macro's fields, or none", () => {
  expect(macroFormStart(null)).toEqual({ name: "", message: "", applies: "", agent: "", tier: "", account: "" })
  expect(macroFormStart({ name: "Deep", keys: "Go deep.\n", applies_to: "task", agent: "codex", tier: "smart", account: "work" }))
    .toEqual({ name: "Deep", message: "Go deep.", applies: "task", agent: "codex", tier: "smart", account: "work" })
})

it("matches names without case or surrounding space", () => {
  expect(sameMacroName(" plan ", "Plan")).toBe(true)
  expect(sameMacroName("plan", "plans")).toBe(false)
})

describe("macroNameStatus", () => {
  it("says a new macro's name is taken by another's, whatever its case", () => {
    expect(macroNameStatus("review", null, macros)).toEqual({ renamed: false, taken: true })
    expect(macroNameStatus("Ship", null, macros)).toEqual({ renamed: false, taken: false })
    expect(macroNameStatus("  ", null, macros)).toEqual({ renamed: false, taken: false })
  })
  it("lets a macro keep its own name, in another case, and says when it is renamed", () => {
    expect(macroNameStatus("PLAN", macros[0], macros)).toEqual({ renamed: false, taken: false })
    expect(macroNameStatus("Plan it", macros[0], macros)).toEqual({ renamed: true, taken: false })
    expect(macroNameStatus("Review", macros[0], macros)).toEqual({ renamed: true, taken: true })
  })
  it("never renames a built-in", () => {
    expect(macroNameStatus("Other", { name: "Plan", keys: "p", builtin: true }, macros).renamed).toBe(false)
  })
})

it("saves only a named macro with a message and a free name", () => {
  expect(canSaveMacro("Ship", "go", false)).toBe(true)
  expect(canSaveMacro(" ", "go", false)).toBe(false)
  expect(canSaveMacro("Ship", " ", false)).toBe(false)
  expect(canSaveMacro("Ship", "go", true)).toBe(false)
})

it("starts the agent named, else Claude Code for an account, else the default", () => {
  expect(macroEffectiveAgent("codex", "work", adapters)).toBe("codex")
  expect(macroEffectiveAgent("", "work", adapters)).toBe("claude-code")
  expect(macroEffectiveAgent("", "", adapters)).toBe("codex")
})

it("offers the default, the tools, and a tool no longer configured", () => {
  expect(macroAgentChoices(adapters, "")).toEqual([["", "Your default (Codex)"], ["claude-code", "Claude Code"], ["codex", "Codex"]])
  expect(macroAgentChoices({ adapters: [], default: "" }, "aider")).toEqual([["", "Your default"], ["aider", "aider"]])
})

it("lists an agent's tiers, and knows a tier it lacks", () => {
  expect(macroTiers(tiers, "codex", "fast")).toEqual({ agentTiers: tiers.codex, unknownTier: false })
  expect(macroTiers(tiers, "codex", "huge").unknownTier).toBe(true)
  expect(macroTiers(tiers, "chimera", "")).toEqual({ agentTiers: [], unknownTier: false })
})

it("keeps a tier the next agent, or the default for none, has", () => {
  expect(tierAfterAgent(tiers, "claude-code", "codex", "smart")).toBe("smart")
  expect(tierAfterAgent(tiers, "claude-code", "codex", "fast")).toBe("")
  expect(tierAfterAgent(tiers, "", "codex", "fast")).toBe("fast")
})

it("knows an account the settings do not list", () => {
  expect(unlistedAccount(adapters, "old")).toBe(true)
  expect(unlistedAccount(adapters, "work")).toBe(false)
  expect(unlistedAccount(adapters, "")).toBe(false)
})

it("shows accounts for Claude Code with some to pick or one set", () => {
  expect(macroAccountsShown("claude-code", adapters, "")).toBe(true)
  expect(macroAccountsShown("claude-code", { adapters: [], default: "" }, "old")).toBe(true)
  expect(macroAccountsShown("claude-code", { adapters: [], default: "" }, "")).toBe(false)
  expect(macroAccountsShown("codex", adapters, "work")).toBe(false)
})

it("saves the account only when one is set", () => {
  expect(macroFields("go", "pr", "codex", "fast", "")).toEqual({ message: "go", applies_to: "pr", agent: "codex", tier: "fast" })
  expect(macroFields("go", "", "", "", "work")).toEqual({ message: "go", applies_to: "", agent: "", tier: "", account: "work" })
})

describe("persistMacro", () => {
  const fields = macroFields("go", "", "", "", "")
  beforeEach(() => {
    save.mockReset().mockResolvedValue(undefined)
    rename.mockReset().mockResolvedValue(undefined)
  })
  it("saves a new macro under its trimmed name", async () => {
    await persistMacro("", null, " Ship ", fields)
    expect(save).toHaveBeenCalledWith("", "Ship", fields)
    expect(rename).not.toHaveBeenCalled()
  })
  it("saves an edited macro under its own name, then renames it", async () => {
    await persistMacro("", macros[0], "plan", fields)
    expect(save).toHaveBeenCalledWith("", "Plan", fields)
    expect(rename).toHaveBeenCalledWith("", "Plan", "plan")
  })
  it("renames nothing when the name stays, nor a built-in", async () => {
    await persistMacro("", macros[0], " Plan ", fields)
    await persistMacro("", { name: "Review", keys: "r", builtin: true }, "Other", fields)
    expect(rename).not.toHaveBeenCalled()
  })
})
