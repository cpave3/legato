import { useState } from "react"
import { Plus, X } from "lucide-react"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"
import { Button } from "../ui/Button"
import { useServer } from "../../hooks/useServer"
import { setWorkflowTags, type WorkflowDefinition } from "../../lib/workflow"
import { copyBuiltInWorkflow } from "../../lib/workflowFiles"
import { sourceLabel } from "../../lib/workflowView"
import { tagProblem } from "../../lib/workflowList"

// errorText is an API error's message: its JSON's error, else its text.
function errorText(reason: unknown): string {
  const text = reason instanceof Error ? reason.message : String(reason)
  try {
    const parsed = JSON.parse(text) as { error?: unknown }
    if (typeof parsed.error === "string") return parsed.error
  } catch { /* plain text */ }
  return text
}

// WorkflowTagsDialog changes a workflow's tags, which are written into its
// file. known are the tags workflows already have, with how many have each.
export function WorkflowTagsDialog({ definition, known, onClose, onSaved }: {
  definition: WorkflowDefinition
  known: Map<string, number>
  onClose: () => void
  onSaved: () => Promise<unknown> | void
}) {
  const { baseUrl } = useServer()
  const [tags, setTags] = useState<string[]>(definition.tags ?? [])
  const [added, setAdded] = useState<string[]>([])
  const [query, setQuery] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const builtIn = definition.source.startsWith("builtin:")
  const typed = query.trim().toLowerCase()
  const offered = [...new Set([...known.keys(), ...(definition.tags ?? []), ...added])].sort()
  const shown = offered.filter(tag => tag.includes(typed))
  const canAdd = typed !== "" && !offered.includes(typed)
  const problem = canAdd ? tagProblem(typed) : ""
  const toggle = (tag: string) => setTags(current => current.includes(tag) ? current.filter(item => item !== tag) : [...current, tag])
  const add = () => {
    if (!canAdd || problem) return
    setAdded(current => [...current, typed])
    setTags(current => [...current, typed])
    setQuery("")
  }
  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    setError("")
    try {
      await action()
      await onSaved()
      onClose()
    } catch (reason) {
      setError(errorText(reason))
    } finally {
      setBusy(false)
    }
  }
  const save = () => run(() => setWorkflowTags(baseUrl, definition.name, offered.filter(tag => tags.includes(tag))))
  const copy = () => run(() => copyBuiltInWorkflow(baseUrl, definition.name, "global"))

  return <Dialog open onClose={onClose}>
    <DialogPanel className="max-w-sm border-lw-line-strong bg-lw-surface text-lw-text">
      <DialogHeader className="border-lw-line"><DialogTitle className="text-lw-text">Tags for {definition.title}</DialogTitle><DialogClose /></DialogHeader>
      {!definition.editable
        ? <>
          <DialogBody>
            {(definition.tags ?? []).length > 0 && <p className="flex flex-wrap gap-1.5">{(definition.tags ?? []).map(tag => <TagChip key={tag} tag={tag} />)}</p>}
            <p className="text-sm">{builtIn
              ? `${definition.title} is built in, so its tags can't be changed here. A copy in your workflows can have its own.`
              : `Read-only here: ${definition.read_only_reason || "Legato can't edit this file"}.`}</p>
            {error && <p role="alert" className="text-sm text-lw-error">{error}</p>}
          </DialogBody>
          <DialogFooter className="border-lw-line">
            <Button variant="outline" size="sm" onClick={onClose}>Close</Button>
            {builtIn && <Button variant="outline" size="sm" disabled={busy} onClick={() => void copy()}>Copy to my workflows</Button>}
          </DialogFooter>
        </>
        : <>
          <DialogBody className="space-y-3">
            <div className="flex gap-2">
              <input type="text" aria-label="Find or add a tag" placeholder="Find or add a tag" value={query} autoFocus
                onChange={event => setQuery(event.target.value)}
                onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); add() } }}
                className="min-h-9 min-w-0 flex-1 rounded-md border border-lw-line-strong bg-lw-ground px-2.5 text-sm text-lw-text placeholder:text-lw-faint focus:border-lw-accent focus:outline-none" />
            </div>
            {canAdd && (problem
              ? <p className="text-xs text-lw-error">{problem}</p>
              : <button type="button" onClick={add} aria-label={`Add tag ${typed}`} className="flex min-h-9 w-full items-center gap-2 rounded-md bg-lw-raised px-2.5 text-left text-sm hover:bg-lw-selected">
                <Plus size={14} aria-hidden="true" className="text-lw-link" />Add tag <span className="font-medium">{typed}</span>
              </button>)}
            {shown.length > 0 && <ul aria-label="Tags" className="flex flex-col">
              {shown.map(tag => <li key={tag}>
                <label className="flex min-h-9 cursor-pointer items-center gap-2.5 rounded-md px-2.5 text-sm hover:bg-lw-raised">
                  <input type="checkbox" checked={tags.includes(tag)} onChange={() => toggle(tag)} className="size-4 accent-[var(--color-lw-accent)]" />
                  <span className="flex-1">{tag}</span>
                  <span aria-hidden="true" title={`${known.get(tag) ?? 0} workflows`} className="font-lw-mono text-xs text-lw-text-2">{known.get(tag) ?? 0}</span>
                </label>
              </li>)}
            </ul>}
            {offered.length === 0 && !canAdd && <p className="text-sm text-lw-text-2">No tags yet</p>}
            <p className="text-xs text-lw-text-2">Writes <code className="font-lw-mono">tags:</code> to {sourceLabel(definition.source)}</p>
            {error && <p role="alert" className="text-sm text-lw-error">{error}</p>}
          </DialogBody>
          <DialogFooter className="border-lw-line">
            <Button variant="outline" size="sm" onClick={onClose}>Cancel</Button>
            <Button size="sm" disabled={busy} onClick={() => void save()}>Save</Button>
          </DialogFooter>
        </>}
    </DialogPanel>
  </Dialog>
}

// TagChip is one tag, as the Workflows page shows tags.
export function TagChip({ tag, className }: { tag: string; className?: string }) {
  return <span className={["inline-flex h-5 items-center whitespace-nowrap rounded-full bg-lw-accent-soft px-2 text-[11px] font-medium text-lw-accent-text", className].filter(Boolean).join(" ")}>{tag}</span>
}

// WorkflowTagLine is a workflow's tags on its own page: each removable,
// and Add tag, where Legato can write its file.
export function WorkflowTagLine({ definition, known, onChanged, className }: { definition: WorkflowDefinition; known: Map<string, number>; onChanged: () => Promise<unknown> | void; className?: string }) {
  const { baseUrl } = useServer()
  const [adding, setAdding] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const tags = definition.tags ?? []
  if (tags.length === 0 && !definition.editable) return null
  const remove = async (tag: string) => {
    setBusy(true)
    setError("")
    try {
      await setWorkflowTags(baseUrl, definition.name, tags.filter(item => item !== tag))
      await onChanged()
    } catch (reason) {
      setError(errorText(reason))
    } finally {
      setBusy(false)
    }
  }
  return <div className={["flex flex-col gap-1", className].filter(Boolean).join(" ")}>
    <div className="flex flex-wrap items-center gap-2">
      {tags.length > 0 && <ul aria-label="Tags" className="flex flex-wrap gap-2">
        {tags.map(tag => <li key={tag} className="inline-flex h-7 items-center gap-1 rounded-full bg-lw-accent-soft pl-2.5 pr-1 text-xs font-medium text-lw-accent-text">
          {tag}
          {definition.editable && <button type="button" disabled={busy} aria-label={`Remove tag ${tag}`} onClick={() => void remove(tag)}
            className="inline-flex size-5 items-center justify-center rounded-full hover:bg-lw-selected"><X size={12} aria-hidden="true" /></button>}
        </li>)}
      </ul>}
      {definition.editable && <button type="button" onClick={() => setAdding(true)}
        className="inline-flex h-7 items-center gap-1 rounded-full border border-dashed border-lw-line-strong px-2.5 text-xs text-lw-text-2 hover:text-lw-text"><Plus size={12} aria-hidden="true" />Add tag</button>}
    </div>
    {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
    {adding && <WorkflowTagsDialog definition={definition} known={known} onClose={() => setAdding(false)} onSaved={onChanged} />}
  </div>
}
