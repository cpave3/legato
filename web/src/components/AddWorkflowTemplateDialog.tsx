import { useEffect, useRef, useState } from "react"
import { useServer } from "../hooks/useServer"
import { useModalLayer } from "../hooks/useModalLayer"
import { addWorkflowTemplate, fetchWorkflowTemplates, type WorkflowFolder, type WorkflowTemplate } from "../lib/workflow"
import { Button } from "./ui/Button"

const field = "min-h-10 w-full rounded-md border border-lw-line-strong bg-lw-ground px-2 text-sm text-lw-text"

// AddWorkflowTemplateDialog writes a bundled template into a workflows
// folder under a name of the operator's; a name a workflow has is refused,
// never overwritten. It shows where the file went.
export function AddWorkflowTemplateDialog({ onClose, onAdded }: { onClose: () => void; onAdded: () => Promise<unknown> | void }) {
  const { baseUrl } = useServer()
  const ref = useRef<HTMLDivElement>(null)
  useModalLayer({ open: true, onClose, containerRef: ref })
  const [templates, setTemplates] = useState<WorkflowTemplate[]>([])
  const [folders, setFolders] = useState<WorkflowFolder[]>([])
  const [template, setTemplate] = useState("")
  const [name, setName] = useState("")
  const [folder, setFolder] = useState("global")
  const [error, setError] = useState("")
  const [added, setAdded] = useState("")
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    let current = true
    fetchWorkflowTemplates(baseUrl).then(list => {
      if (!current) return
      setTemplates(list.templates)
      setFolders(list.folders)
      if (list.templates[0]) { setTemplate(list.templates[0].name); setName(list.templates[0].name) }
      if (list.folders[0]) setFolder(list.folders[0].id)
    }, reason => { if (current) setError(reason instanceof Error ? reason.message : String(reason)) })
    return () => { current = false }
  }, [baseUrl])
  const add = async () => {
    setBusy(true)
    setError("")
    try {
      const result = await addWorkflowTemplate(baseUrl, template, name.trim(), folder)
      setAdded(result.path)
      await onAdded()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  return <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
    <div ref={ref} role="dialog" aria-modal="true" aria-label="Add from template" className="w-full max-w-md rounded-xl border border-lw-line bg-lw-surface p-5 text-lw-text shadow-2xl">
      <h2 className="text-base font-semibold">Add from template</h2>
      <p className="mt-1 text-xs text-lw-text-2">Templates arrive with their triggers paused: nothing runs until you switch it on.</p>
      {added ? <div className="mt-4 flex flex-col gap-3 text-sm">
        <p>Added. Edit it at <code className="break-all font-lw-mono text-xs">{added}</code></p>
        <div className="flex justify-end"><Button onClick={onClose}>Done</Button></div>
      </div> : <form className="mt-4 flex flex-col gap-3" onSubmit={event => { event.preventDefault(); void add() }}>
        <label className="flex flex-col gap-1 text-xs text-lw-text-2">Template
          <select aria-label="Template" className={field} value={template} onChange={event => { setTemplate(event.target.value); setName(event.target.value); setError("") }}>
            {templates.map(t => <option key={t.name} value={t.name}>{t.title || t.name}</option>)}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-xs text-lw-text-2">Name
          <input aria-label="Name" className={field} value={name} onChange={event => { setName(event.target.value); setError("") }} />
        </label>
        <label className="flex flex-col gap-1 text-xs text-lw-text-2">Folder
          <select aria-label="Folder" className={field} value={folder} onChange={event => setFolder(event.target.value)}>
            {folders.map(f => <option key={f.id} value={f.id}>{f.label} · {f.path}</option>)}
          </select>
        </label>
        {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" disabled={busy || !template || !name.trim()}>Add</Button>
        </div>
      </form>}
    </div>
  </div>
}
