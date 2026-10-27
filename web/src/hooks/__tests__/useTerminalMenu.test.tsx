import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { useTerminalMenu } from "../useTerminalMenu"

afterEach(cleanup)

function options(extra: Partial<Parameters<typeof useTerminalMenu>[0]> = {}) {
  return {
    agentKind: "claude-code",
    onRefresh: vi.fn(),
    onDetectPrompt: vi.fn(),
    onTogglePromptDetection: vi.fn(),
    promptDetectionEnabled: true,
    onDisconnect: vi.fn(),
    onKill: vi.fn(),
    ...extra,
  }
}

it("lists what can be done with an agent driven through its terminal, killing last", () => {
  const { result } = renderHook(() => useTerminalMenu(options({ onUpgradeChimera: vi.fn(), ntfyConfigured: true, onToggleNotify: vi.fn() })))
  expect(result.current.entries.map((entry) => entry.label)).toEqual([
    "Upgrade to native panel", "Refresh terminal", "Re-detect prompt", "Disable prompt detection", "Enable notify", "Disconnect", "Kill agent",
  ])
  expect(result.current.entries.at(-1)?.danger).toBe(true)
})

it("offers no upgrade for a tool Legato cannot drive natively", () => {
  const { result } = renderHook(() => useTerminalMenu(options({ agentKind: "shell", onUpgradeChimera: vi.fn() })))
  expect(result.current.entries.some((entry) => entry.label.startsWith("Upgrade"))).toBe(false)
})

it("says why an upgrade failed", async () => {
  const onUpgradeChimera = vi.fn().mockRejectedValue(new Error("No bridge for this session"))
  const { result } = renderHook(() => useTerminalMenu(options({ agentKind: "chimera", onUpgradeChimera })))
  expect(result.current.entries[0].label).toBe("Upgrade to native Chimera")
  await act(async () => { result.current.entries[0].onSelect() })
  expect(result.current.upgradeError).toBe("No bridge for this session")
})

it("offers to reload the agent, just before disconnecting, when it can be reloaded", () => {
  const onReload = vi.fn()
  const { result } = renderHook(() => useTerminalMenu(options({ onReload })))
  const labels = result.current.entries.map((entry) => entry.label)
  expect(labels.slice(-3)).toEqual(["Reload agent", "Disconnect", "Kill agent"])
  result.current.entries.find((entry) => entry.label === "Reload agent")?.onSelect()
  expect(onReload).toHaveBeenCalled()

  const { result: without } = renderHook(() => useTerminalMenu(options()))
  expect(without.current.entries.some((entry) => entry.label === "Reload agent")).toBe(false)
})
