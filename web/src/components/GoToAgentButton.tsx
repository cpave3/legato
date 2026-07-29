import { Link } from "react-router-dom"
import { ArrowRightCircle } from "lucide-react"
import { useRunningAgent } from "../hooks/useRunningAgent"

interface GoToAgentButtonProps {
  taskId: string
}

/**
 * A compact "Go to agent" link that appears only when there's a running agent
 * for the given task. Links to the agents page with the task preselected.
 */
export function GoToAgentButton({ taskId }: GoToAgentButtonProps) {
  const { agent, loading } = useRunningAgent(taskId)

  if (loading || !agent) return null

  return (
    <Link
      to={`/agents?task=${encodeURIComponent(taskId)}`}
      className="flex items-center gap-1.5 rounded-lg border border-indigo-800 bg-indigo-950/50 px-3 py-1.5 text-xs font-medium text-indigo-300 transition-colors hover:bg-indigo-900/60"
      title={`Go to agent: ${agent.task_title || taskId}`}
    >
      <ArrowRightCircle size={14} className="shrink-0" />
      Go to agent
    </Link>
  )
}
