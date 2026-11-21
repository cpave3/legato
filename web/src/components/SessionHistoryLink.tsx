import { Link } from "react-router-dom"
import { History } from "lucide-react"
import { cn } from "../lib/utils"

// SessionHistoryLink leads to the session history, where every past
// conversation of an agent can be found and restored.
export function SessionHistoryLink({ className }: { className?: string }) {
  return (
    <Link to="/agents/history" className={cn("flex items-center gap-1.5 rounded-md px-2 py-1.5 text-xs text-lw-text-2 hover:bg-lw-raised hover:text-lw-text", className)}>
      <History size={13} aria-hidden="true" />
      Session history
    </Link>
  )
}
