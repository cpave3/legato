import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ connected: true, send: vi.fn(), subscribe: () => () => {} }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  localStorage.clear()
})

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function mockSession(toolCalls: unknown[], pendingPermissions: unknown[] = []) {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (String(input).includes("/snapshot")) {
      return new Response(JSON.stringify({
        id: "task-1", status: pendingPermissions.length ? "waiting" : "idle", pendingPermissions,
        messages: [
          { role: "user", content: "You are an agent." },
          { role: "user", content: "What is open?" },
          { role: "assistant", content: toolCalls.map(() => ({ type: "tool-call" })) },
        ],
        toolCalls,
      }), { status: 200 })
    }
    return new Response("[]", { status: 200 })
  })
}

describe("NativeAgentPanel and MCP tool calls", () => {
  it.each([
    ["Claude Code", "mcp__burnlite__list_tasks"],
    ["Codex", "burnlite.list_tasks"],
    ["Chimera", "mcp_burnlite_list_x00005f_tasks"],
  ])("names %s's MCP call by its server and tool, keeping the raw name to hand", async (_agent, raw) => {
    mockSession([
      { callId: "c1", name: raw, args: { project: "LEG" }, result: { output: "LEG-1 open" }, display: { summary: "LEG" } },
    ])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })

    const row = await screen.findByRole("button", { name: /Burnlite.*List tasks/ })
    expect(row.getAttribute("title")).toBe(raw)
    expect(row.querySelector("[data-mcp]")?.textContent).toContain("Burnlite")
    expect(screen.queryByText(raw)).toBeNull()

    fireEvent.click(row)
    expect(screen.getByText(raw)).toBeTruthy()
    expect(screen.getByText(/LEG-1 open/)).toBeTruthy()
  })

  it("names a plugin's server, not the plugin", async () => {
    mockSession([{ callId: "c1", name: "mcp__plugin_legato-tools_legato__legato_api", args: {}, result: { output: "ok" } }])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })

    const row = await screen.findByRole("button", { name: /Legato.*Legato API/ })
    expect(row.getAttribute("title")).toBe("mcp__plugin_legato-tools_legato__legato_api")
  })

  it("keeps a name it cannot read as it is", async () => {
    mockSession([{ callId: "c1", name: "mcp__burnlite", args: {}, result: { output: "ok" } }])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })

    expect(await screen.findByText("mcp__burnlite")).toBeTruthy()
    expect(document.querySelector("[data-mcp]")).toBeNull()
  })

  it("names the MCP tool a permission request is for", async () => {
    mockSession([], [{ requestId: "req-1", tool: "mcp__burnlite__close_task", target: "host", command: "close LEG-1", cwd: "/work" }])
    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />, { wrapper })

    const request = await screen.findByRole("region", { name: "Permission request for Burnlite › Close task" })
    expect(request.textContent).toContain("Burnlite › Close task")
    expect(request.querySelector("[title='mcp__burnlite__close_task']")).toBeTruthy()
  })
})
