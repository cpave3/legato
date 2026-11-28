import { describe, expect, it } from "vitest"
import { agentChatState, awaitsRequest, isBangText, isChimeraKind, reasonText, terminalAsks } from "../nativeAgentView"
import type { PromptState } from "../agentWire"
import type { NativeConversationItem } from "../nativeAgentSync"

describe("nativeAgentView", () => {
  it("puts waiting on the operator before an error, a turn, and background work", () => {
    expect(agentChatState(true, "error", true)).toBe("needs-you")
    expect(agentChatState(false, "error", true)).toBe("error")
    expect(agentChatState(false, "background", true)).toBe("running")
    expect(agentChatState(false, "background", false)).toBe("background")
    expect(agentChatState(false, undefined, false)).toBe("idle")
  })

  it("gives the words of anything thrown", () => {
    expect(reasonText(new Error("boom"))).toBe("boom")
    expect(reasonText("plain")).toBe("plain")
    expect(reasonText(404)).toBe("404")
  })

  it("waits on the operator while a permission or a question is pending", () => {
    const item = (status: string, kind: "permission" | "question") => ({ key: "k", role: "system", text: "", [kind]: { status } }) as unknown as NativeConversationItem
    expect(awaitsRequest([item("pending", "permission")])).toBe(true)
    expect(awaitsRequest([item("pending", "question")])).toBe(true)
    expect(awaitsRequest([item("allowed", "permission"), item("answered", "question")])).toBe(false)
    expect(awaitsRequest([])).toBe(false)
  })

  it("shows a terminal's prompt only when it offers choices the agent's own request does not", () => {
    const menu: PromptState = { type: "menu", actions: [{ label: "Yes", keys: "1" }] }
    expect(terminalAsks(menu, false)).toBe(true)
    expect(terminalAsks(menu, true)).toBe(false)
    expect(terminalAsks({ type: "menu", actions: [] }, false)).toBe(false)
    expect(terminalAsks({ type: "menu" }, false)).toBe(false)
    expect(terminalAsks({ type: "working", actions: [{ label: "x", keys: "x" }] }, false)).toBe(false)
    expect(terminalAsks(null, false)).toBe(false)
  })

  it("takes ! and a command for a shell command, and a lone ! for a message", () => {
    expect(isBangText("!ls")).toBe(true)
    expect(isBangText("!")).toBe(false)
    expect(isBangText("ls !")).toBe(false)
  })

  it("takes a panel with no agent tool for Chimera's", () => {
    expect(isChimeraKind(undefined)).toBe(true)
    expect(isChimeraKind("")).toBe(true)
    expect(isChimeraKind("chimera")).toBe(true)
    expect(isChimeraKind("claude-code")).toBe(false)
  })
})
