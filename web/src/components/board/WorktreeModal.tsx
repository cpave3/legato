import { useEffect, useState } from "react"
import { useServer } from "../../hooks/useServer"
import { createWorktree } from "../../lib/board"
import type { TaskWorktree } from "../../lib/board-types"
import { RepositoryPresetSelect, type RepositoryPreset } from "../RepositoryPresetSelect"

interface WorktreeModalProps {
  open: boolean
  taskId: string
  defaultPrimaryDir: string
  repositories: RepositoryPreset[]
  onClose: () => void
  onCreated: (worktree: TaskWorktree) => void
}

export function WorktreeModal({ open, taskId, defaultPrimaryDir, repositories, onClose, onCreated }: WorktreeModalProps) {
  const { baseUrl } = useServer()
  const [primaryDir, setPrimaryDir] = useState(defaultPrimaryDir)
  const [branch, setBranch] = useState("")
  const [baseBranch, setBaseBranch] = useState("main")
  const [repositoryPreset, setRepositoryPreset] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    if (!open) return
    setPrimaryDir(defaultPrimaryDir)
    setBranch(taskId.toLowerCase().replaceAll("_", "-"))
    setBaseBranch("main")
    setRepositoryPreset("")
    setBusy(false)
    setError("")
  }, [open, taskId, defaultPrimaryDir])

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (busy) return
    setBusy(true)
    setError("")
    try {
      onCreated(await createWorktree(baseUrl, taskId, {
        primary_dir: primaryDir.trim(),
        branch: branch.trim(),
        base_branch: baseBranch.trim(),
      }))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setBusy(false)
    }
  }

  if (!open) return null
  return <div className="fixed inset-0 z-50 flex items-end bg-black/70 p-2 sm:items-center sm:justify-center" onClick={onClose}>
    <form role="dialog" aria-modal="true" onSubmit={(event) => void submit(event)} onClick={event => event.stopPropagation()} className="w-full rounded-xl border border-zinc-700 bg-zinc-900 p-4 sm:max-w-md">
      <h3 className="font-semibold">Create task worktree</h3>
      <p className="mt-1 text-xs text-zinc-500">Creates a Yggdrasil worktree and associates it with {taskId}.</p>
      <div className="mt-4"><RepositoryPresetSelect repositories={repositories} value={repositoryPreset} onChange={(name, repository) => {
        setRepositoryPreset(name)
        if (repository) {
          setPrimaryDir(repository.primary_dir)
          setBaseBranch(repository.base_branch || "main")
        }
      }} /></div>
      <label className="mt-3 block text-xs text-zinc-400">Primary directory
        <input aria-label="Primary directory" required value={primaryDir} onChange={event => { setPrimaryDir(event.target.value); setRepositoryPreset("") }} className="mt-1 min-h-11 w-full rounded border border-zinc-700 bg-zinc-950 px-3" />
      </label>
      <label className="mt-3 block text-xs text-zinc-400">Branch
        <input aria-label="Branch" required value={branch} onChange={event => setBranch(event.target.value)} className="mt-1 min-h-11 w-full rounded border border-zinc-700 bg-zinc-950 px-3" />
      </label>
      <label className="mt-3 block text-xs text-zinc-400">Base branch
        <input aria-label="Base branch" required value={baseBranch} onChange={event => setBaseBranch(event.target.value)} className="mt-1 min-h-11 w-full rounded border border-zinc-700 bg-zinc-950 px-3" />
      </label>
      {error && <p role="alert" className="mt-3 whitespace-pre-wrap text-xs text-red-400">{error}</p>}
      <div className="mt-4 flex gap-2">
        <button type="button" onClick={onClose} className="min-h-11 flex-1 rounded border border-zinc-700">Cancel</button>
        <button type="submit" disabled={busy} className="min-h-11 flex-1 rounded bg-indigo-600 text-white disabled:opacity-60">{busy ? "Creating…" : "Create worktree"}</button>
      </div>
    </form>
  </div>
}
