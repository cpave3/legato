import { useEffect, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Bot, Layers } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { useAgentSelection } from "../../hooks/useAgentSelection"
import { useRunningAgent } from "../../hooks/useRunningAgent"
import { useSettings } from "../../hooks/useSettings"
import { apiFetch } from "../../lib/api"
import { RepositoryPresetSelect, type RepositoryPreset } from "../RepositoryPresetSelect"
import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"
import { InlineNotice } from "../ui/InlineNotice"
import { StartSwarmModal } from "../StartSwarmModal"
import { agentKindLabel } from "../../lib/agentKind"
import { resolveTaskWorkingDirectory } from "../../lib/taskLaunch"

export function TaskAgentAction({ taskId, worktreePath = "", rememberedWorkingDir = "", resumableAgentKinds = [] }: { taskId: string; worktreePath?: string; rememberedWorkingDir?: string; resumableAgentKinds?: string[] }) {
  const { baseUrl } = useServer()
  const settings = useSettings()
  const navigate = useNavigate()
  const { setSelectedAgentId } = useAgentSelection()
  const { agent, agents, loading } = useRunningAgent(taskId)
  const [open, setOpen] = useState(false)
  const [swarmOpen, setSwarmOpen] = useState(false)
  const [agentKind, setAgentKind] = useState("")
  const [workingDir, setWorkingDir] = useState("")
  const [repositories, setRepositories] = useState<RepositoryPreset[]>([])
  const [repositoryPreset, setRepositoryPreset] = useState("")
  const [adapters, setAdapters] = useState<string[]>([])
  const [defaultAdapter, setDefaultAdapter] = useState("")
  const [sessionPolicy, setSessionPolicy] = useState<"resume" | "new">("resume")
  const [busy, setBusy] = useState(false)
  const [initialized, setInitialized] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    if (!open) return
    setRepositoryPreset("")
    setWorkingDir(resolveTaskWorkingDirectory(worktreePath, rememberedWorkingDir, settings.data?.working_dir || ""))
    setSessionPolicy("resume")
    setInitialized(false)
    setRepositories((settings.data?.repositories ?? []) as RepositoryPreset[])
    apiFetch(baseUrl, "/api/adapters").then(async (response) => {
      if (!response.ok) return
      const data = await response.json()
      setAdapters(data.adapters ?? [])
      setDefaultAdapter(data.default ?? "")
    }).finally(() => setInitialized(true))
  }, [open, baseUrl, worktreePath, rememberedWorkingDir, settings.data])

  const effectiveAdapter = agentKind || defaultAdapter
  const offersSessionChoice = resumableAgentKinds.includes(effectiveAdapter)
  const toolLabel = agentKindLabel(effectiveAdapter)
  const swarmConductor = agents.find((value) => value.role === "conductor" && value.parent_task_id === taskId)
  const initialWorkingDir = resolveTaskWorkingDirectory(worktreePath, rememberedWorkingDir, settings.data?.working_dir || "")

  async function launch() {
    setBusy(true)
    setError("")
    try {
      const body: { agent_kind: string; working_dir: string; session_policy?: "resume" | "new" } = {
        agent_kind: agentKind,
        working_dir: workingDir,
      }
      if (offersSessionChoice) body.session_policy = sessionPolicy
      const response = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/agent`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      })
      if (!response.ok) throw new Error(await response.text())
      setSelectedAgentId(taskId)
      navigate("/agents")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setBusy(false)
    }
  }

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
    <Dialog open={open} onClose={() => setOpen(false)} dismissible={!busy}>
      <DialogPanel>
        <DialogHeader><DialogTitle>Open task agent</DialogTitle><DialogClose disabled={busy} /></DialogHeader>
        <DialogBody className="space-y-4">
          <p className="text-xs text-zinc-500">Create a new agent or resume the task's running agent.</p>
          <label className="block text-xs text-zinc-400">Agent
            <select aria-label="Agent" value={agentKind} onChange={(event) => setAgentKind(event.target.value)} className="mt-1 min-h-11 w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 outline-none focus:border-indigo-500">
              <option value="">Default</option>
              {adapters.map((value) => <option key={value}>{value}</option>)}
            </select>
          </label>
          {offersSessionChoice && <fieldset className="space-y-2 rounded-md border border-zinc-800 p-3">
            <legend className="px-1 text-xs font-medium text-zinc-300">Existing {toolLabel} session</legend>
            <label className="flex items-start gap-2 text-xs text-zinc-300">
              <input type="radio" name="session-policy" value="resume" aria-label="Resume previous session" checked={sessionPolicy === "resume"} onChange={() => setSessionPolicy("resume")} className="mt-0.5" />
              <span><span className="block font-medium">Resume previous session</span><span className="text-zinc-500">Continue with the previous {toolLabel} context.</span></span>
            </label>
            <label className="flex items-start gap-2 text-xs text-zinc-300">
              <input type="radio" name="session-policy" value="new" aria-label="Start new session" checked={sessionPolicy === "new"} onChange={() => setSessionPolicy("new")} className="mt-0.5" />
              <span><span className="block font-medium">Start new session</span><span className="text-zinc-500">Start fresh for this Legato task.</span></span>
            </label>
          </fieldset>}
          <RepositoryPresetSelect
            repositories={repositories}
            value={repositoryPreset}
            onChange={(name, repository) => {
              setRepositoryPreset(name)
              if (repository) setWorkingDir(repository.primary_dir)
            }}
          />
          <label className="block text-xs text-zinc-400">Working directory
            <input
              aria-label="Working directory"
              value={workingDir}
              onChange={(event) => {
                setWorkingDir(event.target.value)
                setRepositoryPreset("")
              }}
              className="mt-1 min-h-11 w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 outline-none focus:border-indigo-500"
            />
          </label>
          {error && <InlineNotice variant="error" className="text-xs">{error}</InlineNotice>}
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" size="sm" disabled={busy} onClick={() => setOpen(false)}>Cancel</Button>
          <Button size="sm" onClick={() => void launch()} disabled={busy || !initialized}>{busy ? "Opening…" : initialized ? "Open agent" : "Loading…"}</Button>
        </DialogFooter>
      </DialogPanel>
    </Dialog>
    <StartSwarmModal
      open={swarmOpen}
      onClose={() => setSwarmOpen(false)}
      preSelectedParentId={taskId}
      initialWorkingDir={initialWorkingDir}
      onStarted={() => navigate(`/agents?task=${encodeURIComponent(taskId)}`)}
    />
  </>
}
