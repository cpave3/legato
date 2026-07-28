import { useEffect, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Bot } from "lucide-react"
import { useServer } from "../../hooks/useServer"
import { useAgentSelection } from "../../hooks/useAgentSelection"
import { apiFetch } from "../../lib/api"
import { RepositoryPresetSelect, type RepositoryPreset } from "../RepositoryPresetSelect"

export function TaskAgentAction({ taskId }: { taskId: string }) {
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
  const [error, setError] = useState("")

  useEffect(() => {
    if (!open) return
    setRepositoryPreset("")
    Promise.all([apiFetch(baseUrl, "/api/adapters"), apiFetch(baseUrl, "/api/settings")]).then(async ([adaptersResponse, settingsResponse]) => {
      if (adaptersResponse.ok) {
        const data = await adaptersResponse.json()
        setAdapters(data.adapters ?? [])
      }
      if (settingsResponse.ok) {
        const data = await settingsResponse.json()
        setWorkingDir(data.working_dir ?? "")
        setRepositories(data.repositories ?? [])
      }
    })
  }, [open, baseUrl])

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
    <button onClick={() => setOpen(true)} className="flex min-h-10 items-center gap-2 rounded bg-indigo-600 px-3 text-xs font-medium text-white"><Bot size={14} />Open agent</button>
    {open && <div className="fixed inset-0 z-50 flex items-end bg-black/70 p-2 sm:items-center sm:justify-center">
      <div role="dialog" className="w-full rounded-xl border border-zinc-700 bg-zinc-900 p-4 sm:max-w-md">
        <h3 className="font-semibold">Create or resume task agent</h3>
        <p className="mt-1 text-xs text-zinc-500">A running agent will be resumed automatically.</p>
        <label className="mt-4 block text-xs text-zinc-400">Agent
          <select aria-label="Agent" value={agentKind} onChange={(event) => setAgentKind(event.target.value)} className="mt-1 min-h-11 w-full rounded border border-zinc-700 bg-zinc-950 px-3">
            <option value="">Default</option>
            {adapters.map((value) => <option key={value}>{value}</option>)}
          </select>
        </label>
        <div className="mt-3">
          <RepositoryPresetSelect
            repositories={repositories}
            value={repositoryPreset}
            onChange={(name, repository) => {
              setRepositoryPreset(name)
              if (repository) setWorkingDir(repository.primary_dir)
            }}
          />
        </div>
        <label className="mt-3 block text-xs text-zinc-400">Working directory
          <input
            aria-label="Working directory"
            value={workingDir}
            onChange={(event) => {
              setWorkingDir(event.target.value)
              setRepositoryPreset("")
            }}
            className="mt-1 min-h-11 w-full rounded border border-zinc-700 bg-zinc-950 px-3"
          />
        </label>
        {error && <p className="mt-3 text-xs text-red-400">{error}</p>}
        <div className="mt-4 flex gap-2">
          <button onClick={() => setOpen(false)} className="min-h-11 flex-1 rounded border border-zinc-700">Cancel</button>
          <button onClick={() => void launch()} disabled={busy} className="min-h-11 flex-1 rounded bg-indigo-600 text-white">{busy ? "Opening…" : "Open agent"}</button>
        </div>
      </div>
    </div>}
  </>
}
