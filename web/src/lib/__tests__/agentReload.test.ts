import { afterEach, describe, expect, it, vi } from "vitest"
import type { AgentInfo } from "../agentWire"
import { canReloadAgent, hostNetworkSwitchLabel, reloadAgent, sandboxSwitchLabel, setAgentHostNetwork, switchAgentSandbox } from "../agentReload"

const agent = (extra: Partial<AgentInfo> = {}) => ({ task_id: "T-1", agent_kind: "claude-code", status: "running", ...extra }) as AgentInfo

afterEach(() => vi.unstubAllGlobals())

describe("reloading an agent", () => {
  it("is offered for a running agent of its own or a workflow's, and not for one a swarm runs", () => {
    expect(canReloadAgent(agent())).toBe(true)
    expect(canReloadAgent(agent({ status: "dead" }))).toBe(false)
    expect(canReloadAgent(agent({ role: "conductor", parent_task_id: "T-1" }))).toBe(false)
    expect(canReloadAgent(agent({ role: "backend", parent_task_id: "P-1" }))).toBe(false)
    // A workflow's agent reloads on the step it is on (LEG-689).
    expect(canReloadAgent(agent({ workflow_run_id: "run-1" }))).toBe(true)
    expect(canReloadAgent(agent({ execution_kind: "workflow_agent" }))).toBe(true)
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

  it("offers to move an agent out of the sandbox or back in, only where there is a sandbox", () => {
    expect(sandboxSwitchLabel(agent({ sandboxed: true }))).toBe("Reload without sandbox")
    expect(sandboxSwitchLabel(agent({ unsandboxed: true }))).toBe("Reload in sandbox")
    // The sandbox is off: there is nowhere to move it.
    expect(sandboxSwitchLabel(agent())).toBeUndefined()
    expect(sandboxSwitchLabel(agent({ sandboxed: true, role: "backend", parent_task_id: "P-1" }))).toBeUndefined()
  })

  it("restarts it on the other side of the sandbox and says where it now runs", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ restarted: true, sandboxed: false }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: "Legato's sandbox is off" }), { status: 500 }))
    vi.stubGlobal("fetch", fetch)

    await expect(switchAgentSandbox("http://legato", "T-1")).resolves.toBe(false)
    expect(fetch.mock.calls[0][0]).toBe("http://legato/api/agents/T-1/restart")
    expect(fetch.mock.calls[0][1]).toMatchObject({ method: "POST", body: JSON.stringify({ switch_sandbox: true }) })

    await expect(switchAgentSandbox("http://legato", "T-1")).rejects.toThrow("Legato's sandbox is off")
  })

  it("offers to give a sandboxed agent's containers the host network, or take it away, only where Docker is in its sandbox", () => {
    expect(hostNetworkSwitchLabel(agent({ sandboxed: true }), true)).toBe("Reload with host network")
    expect(hostNetworkSwitchLabel(agent({ sandboxed: true, docker_host_network: true }), true)).toBe("Reload without host network")
    expect(hostNetworkSwitchLabel(agent({ sandboxed: true }), false)).toBeUndefined()
    expect(hostNetworkSwitchLabel(agent({ unsandboxed: true }), true)).toBeUndefined()
    expect(hostNetworkSwitchLabel(agent({ sandboxed: true, reeve: true }), true)).toBeUndefined()
    expect(hostNetworkSwitchLabel(agent({ sandboxed: true, role: "backend", parent_task_id: "P-1" }), true)).toBeUndefined()
  })

  it("restarts it with the host network set as asked", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ restarted: true, docker_host_network: true }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: "the host network for an agent's containers is not allowed for the Reeve" }), { status: 400 }))
    vi.stubGlobal("fetch", fetch)

    await setAgentHostNetwork("http://legato", "T-1", true)
    expect(fetch.mock.calls[0][0]).toBe("http://legato/api/agents/T-1/restart")
    expect(fetch.mock.calls[0][1]).toMatchObject({ method: "POST", body: JSON.stringify({ docker_host_network: true }) })

    await expect(setAgentHostNetwork("http://legato", "T-1", true)).rejects.toThrow("not allowed for the Reeve")
  })
})
