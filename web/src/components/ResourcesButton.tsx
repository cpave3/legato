import { useCallback, useEffect, useRef, useState } from "react"
import { Link } from "react-router-dom"
import { Check, FolderLock, Plus } from "lucide-react"
import { accessLabel, attachedResources, attachResources, listResources, type Resource } from "../lib/resources"

export interface ResourcesButtonProps {
  agentId: string
  baseUrl: string
}

// ResourcesButton gives a running agent more of the person's files and
// folders without typing their paths: a vault, a folder of specs. It sits in
// the native panel's status bar. On a wide screen it opens a list above
// itself; on a phone, a sheet from the bottom with rows sized for a thumb.
// Nothing can be taken away again once given: Chimera has no way to narrow
// what a session may touch.
export function ResourcesButton({ agentId, baseUrl }: ResourcesButtonProps) {
  const [open, setOpen] = useState(false)
  const [resources, setResources] = useState<Resource[] | null>(null)
  const [attached, setAttached] = useState<string[]>([])
  const [pending, setPending] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const ref = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      const [all, given] = await Promise.all([listResources(baseUrl), attachedResources(baseUrl, agentId)])
      setResources(all)
      setAttached(given)
    } catch (reason) {
      setResources([])
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [agentId, baseUrl])

  // Count what the agent has from the start, so the button says so.
  useEffect(() => {
    let current = true
    attachedResources(baseUrl, agentId).then((given) => { if (current) setAttached(given) }, () => {})
    return () => { current = false }
  }, [agentId, baseUrl])

  const close = useCallback(() => {
    setOpen(false)
    trigger.current?.focus()
  }, [])

  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape") close() }
    const onDown = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener("keydown", onKey)
    document.addEventListener("mousedown", onDown)
    return () => {
      document.removeEventListener("keydown", onKey)
      document.removeEventListener("mousedown", onDown)
    }
  }, [open, close])

  const toggle = () => {
    if (open) { close(); return }
    setOpen(true)
    void load()
  }

  const attach = async (name: string) => {
    setPending(name)
    setError(null)
    try {
      setAttached(await attachResources(baseUrl, agentId, [name]))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setPending(null)
    }
  }

  const isAttached = (name: string) => attached.some((given) => given.toLowerCase() === name.toLowerCase())

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        ref={trigger}
        type="button"
        onClick={toggle}
        aria-expanded={open}
        aria-label={attached.length > 0 ? `Resources (${attached.length} attached)` : "Resources"}
        title="Files and folders this agent may use"
        className={"flex items-center gap-1 rounded border px-2 py-1 text-xs transition-colors " + (open
          ? "border-zinc-600 bg-zinc-800 text-zinc-200"
          : "border-zinc-700 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200")}
      >
        <FolderLock size={12} />
        <span className="hidden sm:inline">Resources</span>
        {attached.length > 0 && <span className="tabular-nums">{attached.length}</span>}
      </button>
      {open && (
        <>
          {/* On a phone the sheet covers the conversation; a tap beside it closes it. */}
          <div aria-hidden="true" className="fixed inset-0 z-40 bg-black/50 sm:hidden" onClick={close} />
          <div
            role="dialog"
            aria-label="Resources"
            className="fixed inset-x-0 bottom-0 z-50 max-h-[70dvh] overflow-y-auto rounded-t-xl border-t border-zinc-700 bg-zinc-900 pb-[env(safe-area-inset-bottom)] shadow-2xl sm:absolute sm:inset-x-auto sm:bottom-full sm:right-0 sm:mb-1 sm:max-h-96 sm:w-80 sm:rounded-lg sm:border sm:pb-0"
          >
            <div className="flex items-center justify-between gap-2 border-b border-zinc-800 px-4 py-3 sm:px-3 sm:py-2">
              <span className="text-sm font-medium text-zinc-200 sm:text-xs">Give this agent</span>
              <Link to="/settings#resources" onClick={() => setOpen(false)} className="flex min-h-11 items-center gap-1 text-xs text-indigo-300 hover:text-indigo-200 sm:min-h-0">
                <Plus size={12} /> Manage in Settings
              </Link>
            </div>
            {error && <p role="alert" className="mx-4 mt-3 rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300 sm:mx-3">{error}</p>}
            {resources === null ? (
              <p className="px-4 py-4 text-xs text-zinc-500 sm:px-3">Loading…</p>
            ) : resources.length === 0 ? (
              <p className="px-4 py-4 text-xs text-zinc-400 sm:px-3">
                No resources yet. Add folders you give agents often, such as a notes vault, in <Link to="/settings#resources" onClick={() => setOpen(false)} className="text-indigo-300 underline">Settings</Link>.
              </p>
            ) : (
              <ul aria-label="Resources" className="divide-y divide-zinc-800 py-1">
                {resources.map((resource) => {
                  const given = isAttached(resource.name)
                  return (
                    <li key={resource.name}>
                      <button
                        type="button"
                        disabled={given || pending !== null}
                        onClick={() => void attach(resource.name)}
                        aria-label={given ? `${resource.name}, attached` : `Attach ${resource.name}`}
                        className="flex min-h-14 w-full items-start gap-3 px-4 py-3 text-left transition-colors enabled:hover:bg-zinc-800 disabled:cursor-default sm:min-h-0 sm:px-3 sm:py-2"
                      >
                        <span className={"mt-0.5 flex size-5 shrink-0 items-center justify-center rounded border " + (given ? "border-emerald-500 bg-emerald-600 text-white" : "border-zinc-600 text-transparent")}>
                          <Check size={12} />
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className="flex items-center gap-2 text-sm text-zinc-200 sm:text-xs">
                            {resource.name}
                            {pending === resource.name && <span className="text-zinc-500">Attaching…</span>}
                          </span>
                          {resource.paths.map((path) => (
                            <span key={path.path} className="mt-0.5 flex min-w-0 items-baseline gap-2 text-xs text-zinc-500">
                              <span className="truncate font-mono" title={path.path}>{path.path}</span>
                              <span className="shrink-0 text-[10px] text-zinc-600">{accessLabel(path.access)}</span>
                            </span>
                          ))}
                        </span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
          </div>
        </>
      )}
    </div>
  )
}
