import { Bell, BellOff, FolderInput, Info, Network, Pencil, RefreshCw, RotateCw, Shield, Skull, Terminal, Unplug } from "lucide-react"
import type { AgentMenuEntry } from "../lib/agentMenu"
import type { SandboxSwitch } from "../lib/agentReload"

// What a native agent's header menu can do, as its panel was given it. An
// action left out is not offered.
export interface NativeAgentMenuActions {
  onRename?: () => void
  onMoveToGroup?: () => void
  onShowInfo?: () => void
  onRefresh: () => void
  refreshing: boolean
  ntfyConfigured?: boolean
  notifyEnabled?: boolean
  onToggleNotify?: () => void
  onDowngradeChimera?: () => Promise<void>
  // Told why the downgrade failed.
  onDowngradeFailed: (reason: unknown) => void
  onReload?: () => void
  switchSandbox?: SandboxSwitch
  switchHostNetwork?: SandboxSwitch
  onDisconnect?: () => void
  onKill?: () => void
}

// buildAgentMenu lists the menu's entries: the agent itself, the refresh,
// notifications, how it runs, and last the ones that end it.
export function buildAgentMenu(actions: NativeAgentMenuActions): AgentMenuEntry[] {
  return [
    ...identityEntries(actions),
    refreshEntry(actions),
    ...notifyEntries(actions),
    ...downgradeEntries(actions),
    ...runEntries(actions),
    ...endEntries(actions),
  ]
}

function identityEntries({ onRename, onMoveToGroup, onShowInfo }: NativeAgentMenuActions): AgentMenuEntry[] {
  return [
    ...(onRename ? [{ label: "Rename", icon: <Pencil size={12} />, onSelect: onRename }] : []),
    ...(onMoveToGroup ? [{ label: "Move to group…", icon: <FolderInput size={12} />, onSelect: onMoveToGroup }] : []),
    ...(onShowInfo ? [{ label: "Agent info", icon: <Info size={12} />, onSelect: onShowInfo }] : []),
  ]
}

function refreshEntry({ refreshing, onRefresh }: NativeAgentMenuActions): AgentMenuEntry {
  return { label: refreshing ? "Refreshing…" : "Refresh from snapshot", icon: <RefreshCw size={12} className={refreshing ? "animate-spin" : ""} />, onSelect: onRefresh, disabled: refreshing }
}

function notifyEntries({ ntfyConfigured, notifyEnabled, onToggleNotify }: NativeAgentMenuActions): AgentMenuEntry[] {
  if (!ntfyConfigured || !onToggleNotify) return []
  return [{ label: notifyEnabled ? "Disable notify" : "Enable notify", icon: notifyEnabled ? <Bell size={12} /> : <BellOff size={12} />, onSelect: onToggleNotify }]
}

function downgradeEntries({ onDowngradeChimera, onDowngradeFailed }: NativeAgentMenuActions): AgentMenuEntry[] {
  if (!onDowngradeChimera) return []
  return [{
    label: "Downgrade to tmux",
    icon: <Terminal size={12} />,
    onSelect: () => {
      void onDowngradeChimera().catch(onDowngradeFailed)
    },
  }]
}

function runEntries({ onReload, switchSandbox, switchHostNetwork }: NativeAgentMenuActions): AgentMenuEntry[] {
  return [
    ...(onReload ? [{ label: "Reload agent", icon: <RotateCw size={12} />, onSelect: onReload }] : []),
    ...(switchSandbox ? [{ label: switchSandbox.label, icon: <Shield size={12} />, onSelect: switchSandbox.onSelect }] : []),
    ...(switchHostNetwork ? [{ label: switchHostNetwork.label, icon: <Network size={12} />, onSelect: switchHostNetwork.onSelect }] : []),
  ]
}

function endEntries({ onDisconnect, onKill }: NativeAgentMenuActions): AgentMenuEntry[] {
  return [
    ...(onDisconnect ? [{ label: "Disconnect", icon: <Unplug size={12} />, onSelect: onDisconnect }] : []),
    ...(onKill ? [{ label: "Kill agent", icon: <Skull size={12} />, onSelect: onKill, danger: true }] : []),
  ]
}
