import { useEffect, useState } from "react"
import { useServer } from "../../hooks/useServer"
import { createWorktree } from "../../lib/board"
import type { TaskWorktree } from "../../lib/board-types"
import { RepositoryPresetSelect, type RepositoryPreset } from "../RepositoryPresetSelect"
import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"
import { InlineNotice } from "../ui/InlineNotice"

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

  const inputClassName = "mt-1 min-h-11 w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 outline-none focus:border-indigo-500"

  return <Dialog open={open} onClose={onClose} dismissible={!busy}>
    <DialogPanel>
      <DialogHeader><DialogTitle>Create worktree</DialogTitle><DialogClose disabled={busy} /></DialogHeader>
      <form onSubmit={(event) => void submit(event)}>
        <DialogBody className="space-y-4">
          <p className="text-xs text-zinc-500">Create a Yggdrasil worktree and associate it with {taskId}.</p>
          <RepositoryPresetSelect repositories={repositories} value={repositoryPreset} onChange={(name, repository) => {
            setRepositoryPreset(name)
            if (repository) {
              setPrimaryDir(repository.primary_dir)
              setBaseBranch(repository.base_branch || "main")
            }
          }} />
          <label className="block text-xs text-zinc-400">Primary directory
            <input aria-label="Primary directory" required value={primaryDir} onChange={event => { setPrimaryDir(event.target.value); setRepositoryPreset("") }} className={inputClassName} />
          </label>
          <label className="block text-xs text-zinc-400">Branch
            <input aria-label="Branch" required value={branch} onChange={event => setBranch(event.target.value)} className={inputClassName} />
          </label>
          <label className="block text-xs text-zinc-400">Base branch
            <input aria-label="Base branch" required value={baseBranch} onChange={event => setBaseBranch(event.target.value)} className={inputClassName} />
          </label>
          {error && <InlineNotice variant="error" className="whitespace-pre-wrap text-xs">{error}</InlineNotice>}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" size="sm" disabled={busy} onClick={onClose}>Cancel</Button>
          <Button type="submit" size="sm" disabled={busy}>{busy ? "Creating…" : "Create worktree"}</Button>
        </DialogFooter>
      </form>
    </DialogPanel>
  </Dialog>
}
