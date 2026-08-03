import { Link } from "react-router-dom"
import { ArrowRightCircle } from "lucide-react"
import { useRunningAgent } from "../hooks/useRunningAgent"
import { Button } from "./ui/Button"

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
    <Button asChild variant="outline" size="sm" className="border-indigo-800 bg-indigo-950/50 text-indigo-300 hover:bg-indigo-900/60">
      <Link
        to={`/agents?task=${encodeURIComponent(taskId)}`}
        title={`Go to agent: ${agent.task_title || taskId}`}
      >
        <ArrowRightCircle size={14} />
        Go to agent
      </Link>
    </Button>
  )
}
