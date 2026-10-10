import { describe, expect, it } from "vitest"
import { canBeNative, nativeToolName } from "../agentKind"

describe("agent kinds and the native panel", () => {
  it("knows which agent tools Legato can drive through a native endpoint", () => {
    for (const kind of ["chimera", "claude-code", "codex"]) expect(canBeNative(kind)).toBe(true)
    for (const kind of ["shell", "aider", "", undefined]) expect(canBeNative(kind)).toBe(false)
  })

  it("names the tool a native session belongs to", () => {
    expect(nativeToolName("codex")).toBe("Codex")
    expect(nativeToolName("claude-code")).toBe("Claude Code")
    // The panel was Chimera's first: a session that says nothing is one.
    expect(nativeToolName(undefined)).toBe("Chimera")
    // An adapter this build does not know keeps its own name.
    expect(nativeToolName("aider")).toBe("aider")
  })
})
