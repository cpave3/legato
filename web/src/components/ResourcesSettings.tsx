import { useCallback, useEffect, useId, useState } from "react"
import { FolderLock, Pencil, Plus, Trash2, X } from "lucide-react"
import { accessLabel, deleteResource, listResources, saveResource, type Resource, type ResourcePath } from "../lib/resources"

interface Draft {
  id?: number
  name: string
  paths: ResourcePath[]
}

const emptyDraft = (): Draft => ({ name: "", paths: [{ path: "", access: "read" }] })

// ResourcesSettings is where the person keeps the files and folders they give
// agents often, such as a notes vault, so they never type the path again. A
// resource named in config.yaml is listed but changed there.
export function ResourcesSettings({ baseUrl }: { baseUrl: string }) {
  const [resources, setResources] = useState<Resource[] | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [confirming, setConfirming] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setResources(await listResources(baseUrl))
    } catch (reason) {
      setResources([])
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])

  useEffect(() => { void load() }, [load])

  // The status bar of an agent links here; come into view once the list is in.
  useEffect(() => {
    if (resources !== null && window.location.hash === "#resources") {
      document.getElementById("resources")?.scrollIntoView?.({ block: "start" })
    }
  }, [resources])

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setError(null)
    try {
      await saveResource(baseUrl, { id: draft.id, name: draft.name.trim(), paths: draft.paths.filter((p) => p.path.trim() !== "") })
      setDraft(null)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSaving(false)
    }
  }

  const remove = async (id: number) => {
    setError(null)
    try {
      await deleteResource(baseUrl, id)
      setConfirming(null)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  return (
    <section id="resources" className="max-w-lg mb-8 scroll-mt-4">
      <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">Resources</h2>
      <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
        <div className="flex items-start gap-3">
          <FolderLock size={20} className="mt-0.5 shrink-0 text-indigo-400" />
          <div className="flex min-w-0 flex-1 flex-col gap-3">
            <p className="text-sm text-zinc-300">
              Files and folders you give agents often. Pick them when you start an agent, or add them to one that is running from its status bar.
            </p>
            <p className="text-xs text-zinc-500">
              Chimera is granted each path, to read or to write. Claude Code and Codex are told about them.
            </p>
            {error && <p role="alert" className="rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300">{error}</p>}

            {resources && resources.length > 0 && (
              <ul className="divide-y divide-zinc-800 rounded border border-zinc-800">
                {resources.map((resource) => (
                  <li key={resource.name} aria-label={resource.name} className="flex flex-col gap-1 px-3 py-2">
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-zinc-200">{resource.name}</span>
                      {resource.configured && <span className="rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400" title="Defined in the configuration file">config.yaml</span>}
                      {!resource.configured && resource.id ? (
                        <span className="ml-auto flex items-center gap-1">
                          <button type="button" aria-label={`Edit ${resource.name}`} onClick={() => setDraft({ id: resource.id, name: resource.name, paths: resource.paths.map((p) => ({ ...p })) })}
                            className="flex size-9 items-center justify-center rounded text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 sm:size-7">
                            <Pencil size={14} />
                          </button>
                          {confirming === resource.id ? (
                            <>
                              <button type="button" onClick={() => void remove(resource.id!)} className="rounded bg-red-600 px-2 py-1 text-xs text-white hover:bg-red-500">Remove</button>
                              <button type="button" onClick={() => setConfirming(null)} className="rounded px-2 py-1 text-xs text-zinc-400 hover:bg-zinc-800">Keep</button>
                            </>
                          ) : (
                            <button type="button" aria-label={`Remove ${resource.name}`} onClick={() => setConfirming(resource.id!)}
                              className="flex size-9 items-center justify-center rounded text-zinc-400 hover:bg-zinc-800 hover:text-red-300 sm:size-7">
                              <Trash2 size={14} />
                            </button>
                          )}
                        </span>
                      ) : null}
                    </div>
                    {resource.paths.map((path) => (
                      <div key={path.path} className="flex min-w-0 items-baseline gap-2 text-xs text-zinc-500">
                        <span className="truncate font-mono" title={path.path}>{path.path}</span>
                        <span className="shrink-0 text-[10px] text-zinc-600">{accessLabel(path.access)}</span>
                      </div>
                    ))}
                  </li>
                ))}
              </ul>
            )}

            {draft ? (
              <ResourceForm draft={draft} onChange={setDraft} onSave={() => void save()} onCancel={() => { setDraft(null); setError(null) }} saving={saving} />
            ) : (
              <button type="button" onClick={() => { setDraft(emptyDraft()); setError(null) }}
                className="inline-flex w-fit items-center gap-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500">
                <Plus size={14} /> New resource
              </button>
            )}
          </div>
        </div>
      </div>
    </section>
  )
}

function ResourceForm({ draft, onChange, onSave, onCancel, saving }: {
  draft: Draft
  onChange: (draft: Draft) => void
  onSave: () => void
  onCancel: () => void
  saving: boolean
}) {
  const id = useId()
  const setPath = (index: number, change: Partial<ResourcePath>) =>
    onChange({ ...draft, paths: draft.paths.map((path, i) => (i === index ? { ...path, ...change } : path)) })
  const canSave = draft.name.trim() !== "" && draft.paths.some((p) => p.path.trim() !== "") && !saving

  return (
    <form
      className="flex flex-col gap-3 rounded border border-zinc-700 p-3"
      onSubmit={(event) => { event.preventDefault(); if (canSave) onSave() }}
    >
      <label className="flex flex-col gap-1 text-xs text-zinc-300">
        Name
        <input value={draft.name} onChange={(event) => onChange({ ...draft, name: event.target.value })} placeholder="Obsidian vault"
          className="rounded border border-zinc-700 bg-zinc-950 px-2 py-2 text-sm text-zinc-200 sm:py-1.5" />
      </label>
      {draft.paths.map((path, index) => (
        <fieldset key={index} className="flex flex-col gap-2">
          <legend className="sr-only">Path {index + 1}</legend>
          <div className="flex items-center gap-2">
            <input aria-label={`Path ${index + 1}`} value={path.path} onChange={(event) => setPath(index, { path: event.target.value })}
              placeholder="~/Documents/My Vault" spellCheck={false} autoCapitalize="off" autoCorrect="off"
              className="min-w-0 flex-1 rounded border border-zinc-700 bg-zinc-950 px-2 py-2 font-mono text-sm text-zinc-200 sm:py-1.5 sm:text-xs" />
            {draft.paths.length > 1 && (
              <button type="button" aria-label={`Remove path ${index + 1}`} onClick={() => onChange({ ...draft, paths: draft.paths.filter((_, i) => i !== index) })}
                className="flex size-9 shrink-0 items-center justify-center rounded text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 sm:size-7">
                <X size={14} />
              </button>
            )}
          </div>
          <div role="radiogroup" aria-label={`Access for path ${index + 1}`} className="flex gap-1 text-xs">
            {(["read", "write"] as const).map((access) => (
              <label key={access} className={"flex min-h-9 cursor-pointer items-center gap-1.5 rounded border px-3 sm:min-h-0 sm:py-1 " + (path.access === access ? "border-indigo-500 bg-indigo-950 text-indigo-100" : "border-zinc-700 text-zinc-400 hover:bg-zinc-800")}>
                <input type="radio" name={`${id}-access-${index}`} value={access} checked={path.access === access} onChange={() => setPath(index, { access })} className="sr-only" />
                {access === "read" ? "Read" : "Write"}
              </label>
            ))}
          </div>
        </fieldset>
      ))}
      <button type="button" onClick={() => onChange({ ...draft, paths: [...draft.paths, { path: "", access: "read" }] })}
        className="inline-flex w-fit items-center gap-1 text-xs text-indigo-300 hover:text-indigo-200">
        <Plus size={12} /> Add a path
      </button>
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="rounded px-3 py-1.5 text-sm text-zinc-400 hover:bg-zinc-800">Cancel</button>
        <button type="submit" disabled={!canSave} className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-50">
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
    </form>
  )
}
