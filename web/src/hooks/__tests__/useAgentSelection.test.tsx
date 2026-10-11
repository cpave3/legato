import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentSelectionProvider, useAgentReselected, useAgentSelection } from "../useAgentSelection"

function AgentControls() {
  const { setSelectedAgentId } = useAgentSelection()
  return (
    <>
      <button onClick={() => setSelectedAgentId("task 42")}>Open agent</button>
      <button onClick={() => setSelectedAgentId(null)}>Close agent</button>
    </>
  )
}

// Stands in for the panel of the agent that is open: mounted for as long as
// that agent is the selected one.
function OpenAgentPanel({ onReselected }: { onReselected: (agentId: string) => void }) {
  const { selectedAgentId } = useAgentSelection()
  return selectedAgentId ? <Panel key={selectedAgentId} agentId={selectedAgentId} onReselected={onReselected} /> : null
}

function Panel({ agentId, onReselected }: { agentId: string; onReselected: (agentId: string) => void }) {
  useAgentReselected(agentId, () => onReselected(agentId))
  return null
}

function renderWithOpenAgentPanel() {
  const onReselected = vi.fn<(agentId: string) => void>()
  render(
    <AgentSelectionProvider>
      <AgentControls />
      <OpenAgentPanel onReselected={onReselected} />
    </AgentSelectionProvider>
  )
  return onReselected
}

function renderInMobileShell() {
  const postMessage = vi.fn<(message: string) => void>()
  window.ReactNativeWebView = { postMessage }
  render(
    <AgentSelectionProvider>
      <AgentControls />
    </AgentSelectionProvider>
  )
  return { lastPosted: () => JSON.parse(postMessage.mock.calls.at(-1)![0]) }
}

describe("AgentSelectionProvider", () => {
  afterEach(() => {
    cleanup()
    delete window.ReactNativeWebView
  })

  it("tells the mobile shell which address reopens the open agent", () => {
    const shell = renderInMobileShell()

    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))

    expect(shell.lastPosted()).toEqual({ legatoShell: "restore", url: `${window.location.origin}/agents?task=task%2042` })
  })

  it("tells the mobile shell when no agent is open any more", () => {
    const shell = renderInMobileShell()

    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    fireEvent.click(screen.getByRole("button", { name: "Close agent" }))

    expect(shell.lastPosted()).toEqual({ legatoShell: "restore", url: `${window.location.origin}/agents` })
  })

  it("selects agents in a browser, where there is no shell to tell", () => {
    render(
      <AgentSelectionProvider>
        <AgentControls />
      </AgentSelectionProvider>
    )

    expect(() => fireEvent.click(screen.getByRole("button", { name: "Open agent" }))).not.toThrow()
  })

  it("tells the open agent's panel when that agent is picked again", () => {
    const onReselected = renderWithOpenAgentPanel()

    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    expect(onReselected).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))
    expect(onReselected).toHaveBeenCalledTimes(1)
    expect(onReselected).toHaveBeenCalledWith("task 42")
  })

  it("tells only the picked agent's panel when several are open", () => {
    const conductor = vi.fn()
    const worker = vi.fn()
    render(
      <AgentSelectionProvider>
        <AgentControls />
        <Panel agentId="conductor" onReselected={conductor} />
        <Panel agentId="task 42" onReselected={worker} />
      </AgentSelectionProvider>
    )

    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))

    expect(worker).toHaveBeenCalledTimes(1)
    expect(conductor).not.toHaveBeenCalled()
  })

  it("treats the mobile app returning to the foreground as picking the open agent again", () => {
    const onReselected = renderWithOpenAgentPanel()
    fireEvent.click(screen.getByRole("button", { name: "Open agent" }))

    act(() => { window.dispatchEvent(new Event("legato:shell-return")) })

    expect(onReselected).toHaveBeenCalledTimes(1)
  })
})
