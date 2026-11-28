import { describe, expect, it } from "vitest"
import { accountPlaceholder, accountsOffered, agentOptionsFor, macroStart } from "../spawnAgent"

describe("agentOptionsFor", () => {
  const adapters = { adapters: ["chimera", "codex"], default: "chimera" }
  it("offers the usage profile's default, each tool, then a shell", () => {
    expect(agentOptionsFor(adapters, "").map((option) => [option.value, option.label, option.note])).toEqual([
      ["", "Default", "usage profile"], ["chimera", "Chimera", ""], ["codex", "Codex", ""], ["shell", "Shell", ""],
    ])
  })
  it("keeps a tool picked that is no longer listed, but lists the shell once", () => {
    expect(agentOptionsFor(adapters, "claude-code").map((option) => option.value)).toEqual(["", "chimera", "codex", "claude-code", "shell"])
    expect(agentOptionsFor(adapters, "shell").map((option) => option.value)).toEqual(["", "chimera", "codex", "shell"])
    expect(agentOptionsFor(adapters, "codex").map((option) => option.value)).toEqual(["", "chimera", "codex", "shell"])
  })
  it("offers the default and a shell before the tools are known", () => {
    expect(agentOptionsFor(undefined, "").map((option) => option.value)).toEqual(["", "shell"])
  })
})

it("says whose account the list starts with", () => {
  expect(accountPlaceholder(true, "claude-code")).toBe("Saved session account")
  expect(accountPlaceholder(false, "claude-code")).toBe("Default account")
  expect(accountPlaceholder(false, "")).toBe("Follow usage profile")
})

it("offers accounts for Claude Code when it has some", () => {
  const accounts = [{ name: "work", label: "Work", provider: "claude" }]
  expect(accountsOffered("claude-code", { adapters: [], default: "", accounts })).toBe(true)
  expect(accountsOffered("codex", { adapters: [], default: "", accounts })).toBe(false)
  expect(accountsOffered("claude-code", { adapters: [], default: "" })).toBe(false)
  expect(accountsOffered("claude-code", undefined)).toBe(false)
})

describe("macroStart", () => {
  it("clears the tier and account for None", () => {
    expect(macroStart(undefined)).toEqual({ tier: "", account: "" })
  })
  it("sets the message, and leaves the tool for a macro that names none", () => {
    expect(macroStart({ name: "Plan", keys: "Plan this.\n" })).toEqual({ tier: "", account: "", prompt: "Plan this." })
  })
  it("sets the tool and tier a macro names", () => {
    expect(macroStart({ name: "Deep", keys: "Go", agent: "codex", tier: "smart" })).toEqual({ tier: "smart", account: "", prompt: "Go", agentKind: "codex" })
  })
  it("starts Claude Code for a macro with an account", () => {
    expect(macroStart({ name: "Work", keys: "Go", account: "work" })).toMatchObject({ account: "work", agentKind: "claude-code" })
  })
})
