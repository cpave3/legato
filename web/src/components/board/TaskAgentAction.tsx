import { useCallback, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Bot, Layers } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { useAgentSelection } from "../../hooks/useAgentSelection"
import { useRunningAgent } from "../../hooks/useRunningAgent"
import { useSettings } from "../../hooks/useSettings"
import { apiFetch } from "../../lib/api"
import { Button } from "../ui/Button"
import { SpawnAgentDialog, type SpawnAgentRequest } from "../SpawnAgentDialog"
import { StartSwarmModal } from "../StartSwarmModal"
import { resolveTaskWorkingDirectory } from "../../lib/taskLaunch"

export function TaskAgentAction({ taskId, worktreePath = "", rememberedWorkingDir = "", resumableAgentKinds = [] }: { taskId: string; worktreePath?: string; rememberedWorkingDir?: string; resumableAgentKinds?: string[] }) {
  const { baseUrl } = useServer()
  const settings = useSettings()
  const navigate = useNavigate()
  const { setSelectedAgentId } = useAgentSelection()
  const { agent, agents, loading } = useRunningAgent(taskId)
  const [open, setOpen] = useState(false)
  const [swarmOpen, setSwarmOpen] = useState(false)

  const swarmConductor = agents.find((value) => value.role === "conductor" && value.parent_task_id === taskId)
  const initialWorkingDir = resolveTaskWorkingDirectory(worktreePath, rememberedWorkingDir, settings.data?.working_dir || "")

  const launch = useCallback(async (request: SpawnAgentRequest) => {
    const body: { agent_kind: string; working_dir: string; session_policy?: "resume" | "new"; resources?: string[]; sandbox?: false; prompt?: string } = {
      agent_kind: request.agentKind,
      working_dir: request.workingDir,
    }
    if (request.sessionPolicy) body.session_policy = request.sessionPolicy
    if (request.resources) body.resources = request.resources
    if (request.sandbox === false) body.sandbox = false
    if (request.prompt) body.prompt = request.prompt
    const response = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/agent`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
    if (!response.ok) throw new Error(await response.text())
    setSelectedAgentId(taskId)
    navigate("/agents")
  }, [baseUrl, taskId, setSelectedAgentId, navigate])

  return <>
    <div className="space-y-2">
      {loading
        ? <Button data-task-primary-action size="sm" className="w-full justify-start" disabled><Bot size={14} />Checking agent…</Button>
        : agent
          ? <Button data-task-primary-action size="sm" className="w-full justify-start" onClick={() => {
            setSelectedAgentId(agent.task_id)
            navigate("/agents")
          }}><Bot size={14} />Go to agent</Button>
          : agents.length > 1
            ? <Button data-task-primary-action size="sm" className="w-full justify-start" onClick={() => {
              setSelectedAgentId(null)
              navigate("/agents")
            }}><Bot size={14} />Go to agents</Button>
            : <Button data-task-primary-action size="sm" className="w-full justify-start" onClick={() => setOpen(true)}><Bot size={14} />Open agent</Button>}
      {!loading && (swarmConductor
        ? <Button variant="outline" size="sm" className="w-full justify-start" onClick={() => {
          setSelectedAgentId(swarmConductor.task_id)
          navigate("/agents")
        }}><Layers size={14} />Go to swarm</Button>
        : <Button variant="outline" size="sm" className="w-full justify-start" onClick={() => setSwarmOpen(true)}><Layers size={14} />Start swarm</Button>)}
    </div>
    <SpawnAgentDialog
      open={open}
      onClose={() => setOpen(false)}
      name="Open task agent"
      description="Create a new agent or resume the task's running agent."
      action="Open agent"
      acting="Opening…"
      // The task's own directory when it has one; otherwise the dialog
      // starts in the server's default, as it writes it.
      initialWorkingDir={worktreePath || rememberedWorkingDir}
      resumableAgentKinds={resumableAgentKinds}
      onSubmit={launch}
    />
    <StartSwarmModal
      open={swarmOpen}
      onClose={() => setSwarmOpen(false)}
      preSelectedParentId={taskId}
      initialWorkingDir={initialWorkingDir}
      onStarted={() => navigate(`/agents?task=${encodeURIComponent(taskId)}`)}
    />
  </>
}
