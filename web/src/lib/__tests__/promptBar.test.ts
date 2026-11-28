import { describe, expect, it } from "vitest"
import { askingPrompt, choiceStep, choiceTone, isBashCommand, terminalKey } from "../promptBar"
import type { PromptState } from "../agentWire"

describe("terminalKey", () => {
  it("names the navigation keys for tmux while the box is empty", () => {
    expect(["ArrowUp", "ArrowDown", "Enter", "Escape", "Tab"].map(key => terminalKey(key, ""))).toEqual(["Up", "Down", "Enter", "Escape", "Tab"])
    expect(terminalKey("a", "")).toBeUndefined()
  })
  it("passes nothing while the box has text", () => {
    expect(terminalKey("Enter", "hi")).toBeUndefined()
  })
})

describe("askingPrompt", () => {
  const prompt = (type: PromptState["type"], actions?: PromptState["actions"]) => ({ type, context: "", actions }) as PromptState
  const actions = [{ label: "Yes", keys: "y" }]
  it("offers the choices of a prompt that asks while the agent waits", () => {
    for (const type of ["tool_approval", "plan_approval", "question", "menu"] as const) expect(askingPrompt("waiting", prompt(type, actions))).toEqual({ type, actions })
  })
  it("offers none mid-output, without choices, or for a prompt that does not ask", () => {
    expect(askingPrompt("working", prompt("menu", actions))).toBeNull()
    expect(askingPrompt("waiting", prompt("menu"))).toBeNull()
    expect(askingPrompt("waiting", prompt("free_text", actions))).toBeNull()
    expect(askingPrompt("waiting", null)).toBeNull()
  })
})

describe("isBashCommand", () => {
  it("marks a ! command for Claude Code and Codex only", () => {
    expect(isBashCommand("claude-code", "! ls")).toBe(true)
    expect(isBashCommand("claude", "! ls")).toBe(true)
    expect(isBashCommand("codex", "! ls")).toBe(true)
    expect(isBashCommand("chimera", "! ls")).toBe(false)
    expect(isBashCommand("codex", "!ls")).toBe(false)
    expect(isBashCommand(undefined, "! ls")).toBe(false)
  })
})

describe("choiceTone and choiceStep", () => {
  it("colours yes green, no red and the rest with the accent", () => {
    expect(["Yes", "Accept", "Always", "No", "Reject", "Edit"].map(choiceTone)).toEqual(["text-emerald-400", "text-emerald-400", "text-emerald-400", "text-red-400", "text-red-400", "text-indigo-400"])
  })
  it("moves within the choices with the arrows and j and k", () => {
    expect(choiceStep("j", 0, 3)).toBe(1)
    expect(choiceStep("ArrowDown", 2, 3)).toBe(2)
    expect(choiceStep("k", 0, 3)).toBe(0)
    expect(choiceStep("ArrowUp", 2, 3)).toBe(1)
    expect(choiceStep("Enter", 1, 3)).toBeNull()
  })
})
