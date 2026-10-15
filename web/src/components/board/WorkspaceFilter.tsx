import { cn } from "../../lib/utils"
import type { Workspace } from "../../lib/board-types"

interface WorkspaceFilterProps {
  workspaces: Workspace[]
  active: string // "all", "unassigned", or workspace id
  onChange: (value: string) => void
  className?: string
}

// Which workspace the board shows, as a row of buttons with the one shown
// pressed. A long list scrolls sideways rather than pushing the header apart.
export function WorkspaceFilter({ workspaces, active, onChange, className }: WorkspaceFilterProps) {
  const options = [
    { value: "all", label: "All" },
    { value: "unassigned", label: "Unassigned" },
    ...workspaces.map((workspace) => ({ value: String(workspace.id), label: workspace.name })),
  ]
  return (
    <div role="group" aria-label="Workspace" className={cn("flex max-w-full overflow-x-auto rounded-md border border-lw-line bg-lw-surface p-0.5", className)} style={{ scrollbarWidth: "none" }}>
      {options.map((option) => {
        const pressed = option.value === active
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={pressed}
            onClick={() => onChange(option.value)}
            className={cn(
              "h-[26px] shrink-0 whitespace-nowrap rounded px-2.5 text-xs",
              pressed ? "bg-lw-selected text-lw-text" : "text-lw-text-2 hover:text-lw-text",
            )}
          >
            {option.label}
          </button>
        )
      })}
    </div>
  )
}
