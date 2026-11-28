import { describe, expect, it, vi } from "vitest"
import { buildAgentMenu } from "../nativeAgentMenu"

const base = { onRefresh: () => {}, refreshing: false, onDowngradeFailed: () => {} }

describe("buildAgentMenu", () => {
  it("offers only the refresh when the panel was given nothing else", () => {
    expect(buildAgentMenu(base).map((entry) => entry.label)).toEqual(["Refresh from snapshot"])
  })

  it("lists every action it was given, in order, the kill last and in red", () => {
    const menu = buildAgentMenu({
      ...base,
      onRename: () => {}, onMoveToGroup: () => {}, onShowInfo: () => {},
      ntfyConfigured: true, notifyEnabled: false, onToggleNotify: () => {},
      onDowngradeChimera: async () => {}, onReload: () => {},
      switchSandbox: { label: "Run outside the sandbox", onSelect: () => {} },
      switchHostNetwork: { label: "Give it the host network", onSelect: () => {} },
      onDisconnect: () => {}, onKill: () => {},
    })
    expect(menu.map((entry) => entry.label)).toEqual([
      "Rename", "Move to group…", "Agent info", "Refresh from snapshot", "Enable notify", "Downgrade to tmux",
      "Reload agent", "Run outside the sandbox", "Give it the host network", "Disconnect", "Kill agent",
    ])
    expect(menu.filter((entry) => entry.danger).map((entry) => entry.label)).toEqual(["Kill agent"])
  })

  it("offers notifications only when ntfy is set up and can be toggled", () => {
    const labels = (actions: object) => buildAgentMenu({ ...base, ...actions }).map((entry) => entry.label)
    expect(labels({ ntfyConfigured: true })).not.toContain("Enable notify")
    expect(labels({ onToggleNotify: () => {} })).not.toContain("Enable notify")
    expect(labels({ ntfyConfigured: true, notifyEnabled: true, onToggleNotify: () => {} })).toContain("Disable notify")
  })

  it("says it is refreshing and holds the refresh while it does", () => {
    const onRefresh = vi.fn()
    const [entry] = buildAgentMenu({ ...base, onRefresh, refreshing: true })
    expect(entry).toMatchObject({ label: "Refreshing…", disabled: true })
    entry.onSelect()
    expect(onRefresh).toHaveBeenCalled()
  })

  it("reports why a downgrade failed", async () => {
    const onDowngradeFailed = vi.fn()
    const menu = buildAgentMenu({ ...base, onDowngradeFailed, onDowngradeChimera: () => Promise.reject(new Error("no tmux")) })
    menu.find((entry) => entry.label === "Downgrade to tmux")!.onSelect()
    await vi.waitFor(() => expect(onDowngradeFailed).toHaveBeenCalledWith(new Error("no tmux")))
  })
})
