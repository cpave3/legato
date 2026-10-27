import { createElement, useState } from "react"
import { Bell, BellOff, Eye, EyeOff, RefreshCw, RotateCw, ScanSearch, Skull, Sparkles, Unplug } from "lucide-react"
import { canBeNative } from "../lib/agentKind"
import type { AgentMenuEntry } from "../components/AgentChatHeader"

interface TerminalMenuOptions {
  agentKind?: string
  onUpgradeChimera?: () => Promise<void>
  onRefresh: () => void
  onDetectPrompt: () => void
  onTogglePromptDetection: () => void
  promptDetectionEnabled: boolean
  ntfyConfigured?: boolean
  notifyEnabled?: boolean
  onToggleNotify?: () => void
  // Stops the agent and starts it again, resuming its conversation. Absent
  // for an agent a swarm or a workflow runs.
  onReload?: () => void
  onDisconnect: () => void
  onKill: () => void
}

const icon = (component: typeof Bell) => createElement(component, { size: 12 })

// What can be done with an agent Legato drives through its terminal, as the
// entries of its More actions menu, and why the last upgrade to the native
// panel failed.
export function useTerminalMenu(options: TerminalMenuOptions): { entries: AgentMenuEntry[]; upgradeError: string } {
  const [upgradePending, setUpgradePending] = useState(false)
  const [upgradeError, setUpgradeError] = useState("")
  const { agentKind, onUpgradeChimera } = options

  const entries: AgentMenuEntry[] = []
  if (canBeNative(agentKind) && onUpgradeChimera) {
    entries.push({
      label: upgradePending ? "Upgrading…" : agentKind === "chimera" ? "Upgrade to native Chimera" : "Upgrade to native panel",
      icon: icon(Sparkles),
      disabled: upgradePending,
      onSelect: () => {
        setUpgradeError("")
        setUpgradePending(true)
        return onUpgradeChimera()
          .catch((error: unknown) => setUpgradeError(error instanceof Error ? error.message : String(error)))
          .finally(() => setUpgradePending(false))
      },
    })
  }
  entries.push(
    { label: "Refresh terminal", icon: icon(RefreshCw), onSelect: options.onRefresh },
    { label: "Re-detect prompt", icon: icon(ScanSearch), onSelect: options.onDetectPrompt },
    {
      label: options.promptDetectionEnabled ? "Disable prompt detection" : "Enable prompt detection",
      icon: icon(options.promptDetectionEnabled ? EyeOff : Eye),
      onSelect: options.onTogglePromptDetection,
    },
  )
  if (options.ntfyConfigured && options.onToggleNotify) {
    entries.push({ label: options.notifyEnabled ? "Disable notify" : "Enable notify", icon: icon(options.notifyEnabled ? Bell : BellOff), onSelect: options.onToggleNotify })
  }
  if (options.onReload) {
    entries.push({ label: "Reload agent", icon: icon(RotateCw), onSelect: options.onReload })
  }
  entries.push(
    { label: "Disconnect", icon: icon(Unplug), onSelect: options.onDisconnect },
    { label: "Kill agent", icon: icon(Skull), onSelect: options.onKill, danger: true },
  )
  return { entries, upgradeError }
}
