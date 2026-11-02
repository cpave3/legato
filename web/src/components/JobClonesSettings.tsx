import { useCallback, useEffect, useState } from "react"
import { AlertTriangle, FolderGit2, Trash2 } from "lucide-react"
import { SettingsCard } from "./settings/SettingsUI"
import { listJobClones, removeJobClone, type JobClone } from "../lib/jobClones"

function lastUsed(iso: string): string {
  const when = new Date(iso)
  return Number.isNaN(when.getTime()) ? "" : when.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })
}

// JobClonesSettings lists the clones Legato made for agents, which later
// agents can work in again (Clone in the spawn dialog), and removes
// one early. Legato removes those left idle for repository_cache.keep_idle
// itself. It shows nothing when there are none.
export function JobClonesSettings({ baseUrl }: { baseUrl: string }) {
  const [clones, setClones] = useState<JobClone[]>([])
  const [confirming, setConfirming] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setClones(await listJobClones(baseUrl).catch(() => []))
  }, [baseUrl])

  useEffect(() => { void load() }, [load])

  const remove = async (id: number) => {
    setError(null)
    try {
      await removeJobClone(baseUrl, id)
      setConfirming(null)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  if (clones.length === 0) return null

  return (
    <SettingsCard id="job-clones" icon={FolderGit2} title="Clones" description="Clones Legato made for agents. Another agent can work in one again, for the next branch of a stack. One left idle is removed after repository_cache.keep_idle, unless it holds unpushed work.">
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className="rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300">{error}</p>}
        <ul className="divide-y divide-zinc-800 rounded border border-zinc-800">
          {clones.map((clone) => (
            <li key={clone.id} aria-label={clone.repo} className="flex flex-col gap-1 px-3 py-2">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-sm text-zinc-200">{clone.repo}</span>
                <span className="truncate font-mono text-xs text-zinc-400">{clone.branch || "detached"}</span>
                {clone.busy && <span className="shrink-0 rounded bg-emerald-950 px-1.5 py-0.5 text-[10px] text-emerald-300">agent working</span>}
                <span className="ml-auto flex shrink-0 items-center gap-1">
                  {confirming === clone.id ? (
                    <>
                      <button type="button" onClick={() => void remove(clone.id)} className="rounded bg-red-600 px-2 py-1 text-xs text-white hover:bg-red-500">Remove</button>
                      <button type="button" onClick={() => setConfirming(null)} className="rounded px-2 py-1 text-xs text-zinc-400 hover:bg-zinc-800">Keep</button>
                    </>
                  ) : (
                    <button
                      type="button"
                      aria-label={`Remove ${clone.repo}`}
                      title={clone.busy ? "Stop its agent first" : undefined}
                      disabled={clone.busy}
                      onClick={() => setConfirming(clone.id)}
                      className="flex size-9 items-center justify-center rounded text-zinc-400 hover:bg-zinc-800 hover:text-red-300 disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-zinc-400 sm:size-7"
                    >
                      <Trash2 size={14} />
                    </button>
                  )}
                </span>
              </div>
              <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5 text-xs text-zinc-500">
                <span className="truncate font-mono" title={clone.path}>{clone.path}</span>
                <span>tasks {clone.task_ids.join(", ")}</span>
                {lastUsed(clone.last_used_at) && <span>used {lastUsed(clone.last_used_at)}</span>}
                {clone.unpushed > 0 && <span className="text-amber-300/90">{clone.unpushed} unpushed</span>}
              </div>
              {confirming === clone.id && clone.unpushed > 0 && (
                <p className="flex items-start gap-1.5 text-xs text-amber-300/90">
                  <AlertTriangle size={12} className="mt-0.5 shrink-0" aria-hidden="true" />
                  <span>{clone.unpushed} unpushed branch tips are lost with it.</span>
                </p>
              )}
              {clone.problem && <p className="text-xs text-amber-300/90">{clone.problem}</p>}
            </li>
          ))}
        </ul>
      </div>
    </SettingsCard>
  )
}
