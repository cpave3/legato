import { describe, expect, it } from "vitest"
import { agentChatState, isBangText, isChimeraKind, reasonText } from "../nativeAgentView"

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
