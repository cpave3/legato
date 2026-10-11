import { describe, expect, it } from "vitest"
import { buildSandboxDocument, legatoUICall, proposedMessageText } from "../agentUi"
import type { NativeToolItem } from "../nativeAgentSync"

function tool(toolName: string, result?: Record<string, unknown>, args: Record<string, unknown> = {}): NativeToolItem {
  return { key: "t1", toolName, args, result }
}

const shown = "Shown as a card in the conversation [legato-ui:ui-0123456789:2]. The person can see it now; do not repeat its content in your reply."

describe("legatoUICall", () => {
  it.each([
    // Each agent tool names an MCP server's tools in its own way.
    ["Claude Code", "mcp__plugin_legato-tools_legato__show_card", { output: shown }],
    ["Codex", "legato.show_card", { output: shown }],
    ["Chimera", "mcp_legato_show_x00005f_card", { content: [{ type: "text", text: shown }] }],
  ])("recognises a card shown by %s and finds its item", (_agent, name, result) => {
    expect(legatoUICall(tool(name, result, { fallback_text: "27 C", title: "Weather" }))).toEqual({
      tool: "show_card", state: "shown", itemId: "ui-0123456789", revision: 2, title: "Weather", fallbackText: "27 C",
    })
  })

  it("recognises component cards and artifacts", () => {
    expect(legatoUICall(tool("legato.show_components", { output: shown }))?.tool).toBe("show_components")
    expect(legatoUICall(tool("mcp__plugin_legato-tools_legato__show_artifact", { output: "Shown in Legato's side panel as \"Design\" [legato-ui:ui-aaaaaaaaaa:1]." }, { title: "Design" }))).toEqual({
      tool: "show_artifact", state: "shown", itemId: "ui-aaaaaaaaaa", revision: 1, title: "Design", fallbackText: "",
    })
  })

  it("is pending while the call has no result", () => {
    expect(legatoUICall(tool("legato.show_card", undefined, { fallback_text: "27 C" }))).toEqual({
      tool: "show_card", state: "pending", title: "", fallbackText: "27 C",
    })
  })

  it("has failed when the result names no item, so the error can be shown as any other tool's", () => {
    expect(legatoUICall(tool("legato.show_card", { error: "invalid agent UI item: a card needs fallback_text" }))?.state).toBe("failed")
  })

  it.each([
    "bash",
    "show_card_like",
    "mcp__other__show_card",
    "mcp__plugin_legato-tools_legato__session_info",
    "legato.task_context",
    "notlegato.show_card",
  ])("leaves %s to the ordinary tool row", (name) => {
    expect(legatoUICall(tool(name, { output: shown }))).toBeNull()
  })
})

describe("buildSandboxDocument", () => {
  const html = `<div id="card">27 C</div><script>document.title = "x"</script>`

  it("puts a policy that allows no network ahead of the agent's content", () => {
    const doc = buildSandboxDocument({ html, resourceDomains: [], fill: false })

    const policy = /<meta http-equiv="Content-Security-Policy" content="([^"]*)">/.exec(doc)?.[1] ?? ""
    expect(policy).toContain("default-src 'none'")
    expect(policy).toContain("connect-src 'none'")
    expect(policy).toContain("frame-src 'none'")
    expect(policy).toContain("form-action 'none'")
    expect(policy).toContain("base-uri 'none'")
    expect(policy).toContain("script-src 'unsafe-inline'")
    expect(policy).not.toContain("https:")
    expect(policy).not.toContain("unsafe-eval")
    // A policy only governs what comes after it.
    expect(doc.indexOf("Content-Security-Policy")).toBeLessThan(doc.indexOf(html))
    expect(doc).toContain(html)
  })

  it("lets scripts, styles, fonts and images load from the allowed hosts only", () => {
    const doc = buildSandboxDocument({ html, resourceDomains: ["cdn.jsdelivr.net", "fonts.gstatic.com"], fill: false })

    const policy = /http-equiv="Content-Security-Policy" content="([^"]*)"/.exec(doc)?.[1] ?? ""
    for (const directive of ["script-src", "style-src", "font-src", "img-src"]) {
      const value = new RegExp(`${directive} ([^;]*)`).exec(policy)?.[1] ?? ""
      expect(value).toContain("https://cdn.jsdelivr.net")
      expect(value).toContain("https://fonts.gstatic.com")
    }
    expect(/connect-src ([^;]*)/.exec(policy)?.[1].trim()).toBe("'none'")
  })

  it("drops anything that is not a plain host name, so a value cannot rewrite the policy", () => {
    const doc = buildSandboxDocument({ html, resourceDomains: ["cdn.jsdelivr.net", "'unsafe-eval'", "*", "evil.example.com; connect-src *", "https://x.example.com"], fill: false })

    const policy = /http-equiv="Content-Security-Policy" content="([^"]*)"/.exec(doc)?.[1] ?? ""
    expect(policy).toContain("https://cdn.jsdelivr.net")
    expect(policy).not.toContain("unsafe-eval")
    expect(policy).not.toContain("evil.example.com")
    expect(policy).not.toContain("x.example.com")
    expect(/connect-src ([^;]*)/.exec(policy)?.[1].trim()).toBe("'none'")
  })

  it("gives the content Legato's theme and a way to ask the host for things", () => {
    const doc = buildSandboxDocument({ html, resourceDomains: [], fill: false })

    for (const name of ["--color-background-primary", "--color-text-primary", "--color-text-secondary", "--color-border-primary", "--font-sans", "--font-mono"]) {
      expect(doc).toContain(name)
    }
    expect(doc).toContain("proposeMessage")
    expect(doc).toContain("openLink")
    expect(doc).toContain("ui/notifications/size-changed")
  })
})

describe("proposedMessageText", () => {
  it("reads the text of a message a view asks to put in the composer", () => {
    expect(proposedMessageText({ role: "user", content: { type: "text", text: "  Show the forecast  " } })).toBe("Show the forecast")
  })

  it("bounds what a view can put there and ignores what is not text", () => {
    expect(proposedMessageText({ content: { type: "text", text: "x".repeat(9000) } })?.length).toBe(4000)
    expect(proposedMessageText({ content: { type: "image", data: "…" } })).toBeNull()
    expect(proposedMessageText({ content: { type: "text", text: "   " } })).toBeNull()
    expect(proposedMessageText(null)).toBeNull()
    expect(proposedMessageText("hello")).toBeNull()
  })
})
