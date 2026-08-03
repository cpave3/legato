import { useEffect, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Bot } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { useAgentSelection } from "../../hooks/useAgentSelection"
import { apiFetch } from "../../lib/api"
import { RepositoryPresetSelect, type RepositoryPreset } from "../RepositoryPresetSelect"
import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"
import { InlineNotice } from "../ui/InlineNotice"

export function TaskAgentAction({ taskId, worktreePath = "" }: { taskId: string; worktreePath?: string }) {
  const { baseUrl } = useServer()
  const navigate = useNavigate()
  const { setSelectedAgentId } = useAgentSelection()
  const [open, setOpen] = useState(false)
  const [agentKind, setAgentKind] = useState("")
  const [workingDir, setWorkingDir] = useState("")
  const [repositories, setRepositories] = useState<RepositoryPreset[]>([])
  const [repositoryPreset, setRepositoryPreset] = useState("")
  const [adapters, setAdapters] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [initialized, setInitialized] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    if (!open) return
    setRepositoryPreset("")
    setWorkingDir(worktreePath)
    setInitialized(false)
    Promise.all([apiFetch(baseUrl, "/api/adapters"), apiFetch(baseUrl, "/api/settings")]).then(async ([adaptersResponse, settingsResponse]) => {
      if (adaptersResponse.ok) {
        const data = await adaptersResponse.json()
        setAdapters(data.adapters ?? [])
      }
      if (settingsResponse.ok) {
        const data = await settingsResponse.json()
        if (!worktreePath) setWorkingDir(data.working_dir || "")
        setRepositories(data.repositories ?? [])
      }
    }).finally(() => setInitialized(true))
  }, [open, baseUrl, worktreePath])

  async function launch() {
    setBusy(true)
    setError("")
    try {
      const response = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/agent`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ agent_kind: agentKind, working_dir: workingDir }),
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
    <Button data-task-primary-action size="sm" onClick={() => setOpen(true)}><Bot size={14} />Open agent</Button>
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
  </>
}
