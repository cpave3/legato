import type { ReactNode } from "react"

// One entry of an agent's menu, in its chat header or a context menu.
export interface AgentMenuEntry {
  label: string
  icon?: ReactNode
  onSelect: () => unknown
  disabled?: boolean
  // Ends the agent, or something like it: listed last, apart, in red.
  danger?: boolean
}
