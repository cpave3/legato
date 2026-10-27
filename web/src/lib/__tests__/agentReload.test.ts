import { afterEach, describe, expect, it, vi } from "vitest"
import type { AgentInfo } from "../../hooks/useWebSocket"
import { canReloadAgent, reloadAgent } from "../agentReload"

const agent = (extra: Partial<AgentInfo> = {}) => ({ task_id: "T-1", agent_kind: "claude-code", status: "running", ...extra }) as AgentInfo

afterEach(() => vi.unstubAllGlobals())

describe("reloading an agent", () => {
  it("is offered for a running agent of its own, and not for one a swarm or a workflow runs", () => {
    expect(canReloadAgent(agent())).toBe(true)
    expect(canReloadAgent(agent({ status: "dead" }))).toBe(false)
    expect(canReloadAgent(agent({ role: "conductor", parent_task_id: "T-1" }))).toBe(false)
    expect(canReloadAgent(agent({ role: "backend", parent_task_id: "P-1" }))).toBe(false)
    expect(canReloadAgent(agent({ workflow_run_id: "run-1" }))).toBe(false)
    expect(canReloadAgent(agent({ execution_kind: "workflow_agent" }))).toBe(false)
    expect(canReloadAgent(undefined)).toBe(false)
  })

  it("asks the server to restart it, and gives the server's reason when it cannot", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ restarted: true }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: "agent cannot be restarted: no conversation yet" }), { status: 409 }))
    vi.stubGlobal("fetch", fetch)

    await reloadAgent("http://legato", "T-1")
    expect(fetch.mock.calls[0][0]).toBe("http://legato/api/agents/T-1/restart")
    expect(fetch.mock.calls[0][1]).toMatchObject({ method: "POST" })

    await expect(reloadAgent("http://legato", "T-1")).rejects.toThrow("agent cannot be restarted: no conversation yet")
  })
})
