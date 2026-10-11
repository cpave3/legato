import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentSelectionProvider, useAgentSelection } from "../../hooks/useAgentSelection"
import { NativeAgentPanel } from "../NativeAgentPanel"

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ connected: true, send: vi.fn(), subscribe: () => () => {} }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

// The picker and the open agent's panel, as the agents page mounts them.
function PickerAndPanel() {
  const { selectedAgentId, setSelectedAgentId } = useAgentSelection()
  return (
    <>
      <button onClick={() => setSelectedAgentId("task-1")}>Pick task-1</button>
      {selectedAgentId && <NativeAgentPanel key={selectedAgentId} agentId={selectedAgentId} />}
    </>
  )
}

async function openAgent() {
  const snapshotRequests: string[] = []
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const path = String(input)
    if (!path.includes("/snapshot")) return new Response("[]", { status: 200 })
    snapshotRequests.push(path)
    return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
  })
  render(
    <AgentSelectionProvider>
      <PickerAndPanel />
    </AgentSelectionProvider>
  )
  fireEvent.click(screen.getByRole("button", { name: "Pick task-1" }))
  await screen.findByText("Session ready — send a message to start")
  expect(snapshotRequests).toEqual(["/api/agents/task-1/chimera/snapshot?initial=true"])
  return snapshotRequests
}

describe("NativeAgentPanel when its agent is picked again", () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("refreshes from the snapshot", async () => {
    const snapshotRequests = await openAgent()

    fireEvent.click(screen.getByRole("button", { name: "Pick task-1" }))

    await waitFor(() => expect(snapshotRequests).toHaveLength(2))
    expect(snapshotRequests[1]).toBe("/api/agents/task-1/chimera/snapshot")
  })

  it("refreshes from the snapshot when the mobile app returns to the foreground", async () => {
    const snapshotRequests = await openAgent()

    act(() => { window.dispatchEvent(new Event("legato:shell-return")) })

    await waitFor(() => expect(snapshotRequests).toHaveLength(2))
    expect(snapshotRequests[1]).toBe("/api/agents/task-1/chimera/snapshot")
  })
})
