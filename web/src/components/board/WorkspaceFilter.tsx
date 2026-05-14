import { cn } from "../../lib/utils"
import type { Workspace } from "../../lib/board-types"
import { ChevronDown } from "lucide-react"

interface WorkspaceFilterProps {
  workspaces: Workspace[]
  active: string // "all", "unassigned", or workspace id
  onChange: (value: string) => void
}

export function WorkspaceFilter({ workspaces, active, onChange }: WorkspaceFilterProps) {
  return (
    <div className="relative">
      <select
        value={active}
        onChange={(e) => onChange(e.target.value)}
        className={cn(
          "appearance-none rounded border border-zinc-700 bg-zinc-900 px-3 py-1.5 pr-8 text-xs text-zinc-300 outline-none focus:border-indigo-500"
        )}
      >
        <option value="all">All</option>
        <option value="unassigned">Unassigned</option>
        {workspaces.map((ws) => (
          <option key={ws.id} value={String(ws.id)}>
            {ws.name}
          </option>
        ))}
      </select>
      <ChevronDown size={12} className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-zinc-500" />
    </div>
  )
}
