import { describe, expect, it } from "vitest"
import { humanizeToolName, mcpToolName, toolDisplayName } from "../mcpToolName"

describe("mcpToolName", () => {
  it.each([
    // Claude Code: mcp__<server>__<tool>
    ["mcp__burnlite__list_tasks", { server: "burnlite", tool: "list_tasks", serverLabel: "Burnlite", toolLabel: "List tasks" }],
    ["mcp__my-server_2__get_item", { server: "my-server_2", tool: "get_item", serverLabel: "My Server 2", toolLabel: "Get item" }],
    // The rest after the server is the tool, as Claude Code reads it.
    ["mcp__burnlite__odd__name", { server: "burnlite", tool: "odd__name", serverLabel: "Burnlite", toolLabel: "Odd name" }],
    // A plugin's server: mcp__plugin_<plugin>_<server>__<tool>. Plugin names are kebab-case.
    ["mcp__plugin_legato-tools_legato__legato_api", { server: "legato", plugin: "legato-tools", tool: "legato_api", serverLabel: "Legato", toolLabel: "Legato API" }],
    ["mcp__plugin_easel_my_server-x__chart", { server: "my_server-x", plugin: "easel", tool: "chart", serverLabel: "My Server X", toolLabel: "Chart" }],
    // A claude.ai connector keeps its own capitals.
    ["mcp__claude_ai_Google_Calendar__list_events", { server: "claude_ai_Google_Calendar", tool: "list_events", serverLabel: "Google Calendar", toolLabel: "List events" }],
    ["mcp__github__get_pull_request", { server: "github", tool: "get_pull_request", serverLabel: "GitHub", toolLabel: "Get pull request" }],
    // Codex: <server>.<tool>
    ["legato.show_card", { server: "legato", tool: "show_card", serverLabel: "Legato", toolLabel: "Show card" }],
    ["burn-lite.list_tasks", { server: "burn-lite", tool: "list_tasks", serverLabel: "Burn Lite", toolLabel: "List tasks" }],
    // Chimera: mcp_<server>_<tool>, every character outside [a-zA-Z0-9] written as _x<six hex digits>_
    ["mcp_legato_show_x00005f_card", { server: "legato", tool: "show_card", serverLabel: "Legato", toolLabel: "Show card" }],
    ["mcp_burn_x00002d_lite_list_x00005f_tasks", { server: "burn-lite", tool: "list_tasks", serverLabel: "Burn Lite", toolLabel: "List tasks" }],
    ["mcp_legato_speak", { server: "legato", tool: "speak", serverLabel: "Legato", toolLabel: "Speak" }],
  ])("reads %s", (raw, expected) => {
    expect(mcpToolName(raw)).toEqual({ raw, plugin: undefined, ...expected })
  })

  it.each([
    "bash", "read", "spawn_agent", "web_search", "TodoWrite",
    // Not one of the forms: no guessing.
    "mcp__", "mcp__burnlite", "mcp__burnlite__", "mcp____list_tasks",
    "mcp__plugin_legato-tools__x", "mcp__plugin_legato-tools___x",
    "mcp_legato", "mcp_legato_", "mcp_a_b_c", "mcp__a_b",
    ".x", "x.", "a.b.c", "a b.c",
  ])("leaves %s alone", (raw) => {
    expect(mcpToolName(raw)).toBeNull()
  })
})

describe("humanizeToolName", () => {
  it.each([
    ["list_tasks", "List tasks"],
    ["listTasks", "List tasks"],
    ["get-PR", "Get PR"],
    ["fetch_url", "Fetch URL"],
    ["show_html_card", "Show HTML card"],
    ["x", "X"],
  ])("%s → %s", (raw, label) => {
    expect(humanizeToolName(raw)).toBe(label)
  })
})

describe("toolDisplayName", () => {
  it("names an MCP tool by its server and tool", () => {
    expect(toolDisplayName("mcp__burnlite__list_tasks")).toBe("Burnlite › List tasks")
  })

  it("keeps any other name as it is", () => {
    expect(toolDisplayName("bash")).toBe("bash")
    expect(toolDisplayName("mcp__burnlite")).toBe("mcp__burnlite")
  })
})
