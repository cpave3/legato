import { useCallback, useEffect, useState } from "react"
import { Pencil, Plus, ScrollText, Trash2 } from "lucide-react"
import { SettingsCard } from "./settings/SettingsUI"
import { listRepositoryGuidance, removeRepositoryGuidance, saveRepositoryGuidance, type RepositoryGuidance } from "../lib/repositoryGuidance"

// A guidance being written: a new repository's (editing false) or a saved
// one's text (editing true, the repository fixed).
interface Draft {
  repo: string
  text: string
  editing: boolean
}

// RepositoryGuidanceSettings is where the person keeps the guidance agents
// get for the repository they work in. It is matched by the repository's
// origin remote, so it reaches an agent in any checkout or worktree of it.
// That of config.yaml is listed but changed there.
export function RepositoryGuidanceSettings({ baseUrl }: { baseUrl: string }) {
  const [guidance, setGuidance] = useState<RepositoryGuidance[] | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setGuidance(await listRepositoryGuidance(baseUrl))
    } catch (reason) {
      setGuidance([])
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])

  useEffect(() => { void load() }, [load])

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setError(null)
    try {
      await saveRepositoryGuidance(baseUrl, draft.repo.trim(), draft.text.trim())
      setDraft(null)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSaving(false)
    }
  }

  const remove = async (repo: string) => {
    setError(null)
    try {
      await removeRepositoryGuidance(baseUrl, repo)
      setConfirming(null)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  const canSave = draft !== null && draft.repo.trim() !== "" && draft.text.trim() !== "" && !saving
  const input = "rounded border border-zinc-700 bg-zinc-950 px-2 py-2 text-sm text-zinc-200 sm:py-1.5"

  return (
    <SettingsCard id="repository-guidance" icon={ScrollText} title="Repository guidance"
      description="Instructions for every agent that works in a repository, added to its launch prompt. Legato knows the repository by its origin remote, so the guidance reaches agents in any checkout or worktree of it.">
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className="rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300">{error}</p>}

        {guidance && guidance.length > 0 && (
          <ul className="divide-y divide-zinc-800 rounded border border-zinc-800">
            {guidance.map((entry) => (
              <li key={entry.repo} aria-label={entry.repo} className="flex flex-col gap-1 px-3 py-2">
                <div className="flex items-center gap-2">
                  <span className="truncate font-mono text-sm text-zinc-200">{entry.repo}</span>
                  {entry.configured && <span className="rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400" title="Defined in the configuration file">config.yaml</span>}
                  {!entry.configured && (
                    <span className="ml-auto flex items-center gap-1">
                      {confirming === entry.repo ? (
                        <>
                          <button type="button" onClick={() => void remove(entry.repo)} className="rounded bg-red-600 px-2 py-1 text-xs text-white hover:bg-red-500">Remove</button>
                          <button type="button" onClick={() => setConfirming(null)} className="rounded px-2 py-1 text-xs text-zinc-400 hover:bg-zinc-800">Keep</button>
                        </>
                      ) : (
                        <>
                          <button type="button" aria-label={`Edit guidance for ${entry.repo}`} onClick={() => { setDraft({ repo: entry.repo, text: entry.text, editing: true }); setError(null) }}
                            className="flex size-9 items-center justify-center rounded text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 sm:size-7">
                            <Pencil size={14} />
                          </button>
                          <button type="button" aria-label={`Remove guidance for ${entry.repo}`} onClick={() => setConfirming(entry.repo)}
                            className="flex size-9 items-center justify-center rounded text-zinc-400 hover:bg-zinc-800 hover:text-red-300 sm:size-7">
                            <Trash2 size={14} />
                          </button>
                        </>
                      )}
                    </span>
                  )}
                </div>
                <p className="whitespace-pre-wrap break-words text-xs text-zinc-400">{entry.text}</p>
              </li>
            ))}
          </ul>
        )}

        {draft ? (
          <form
            className="flex flex-col gap-3 rounded border border-zinc-700 p-3"
            onSubmit={(event) => { event.preventDefault(); if (canSave) void save() }}
          >
            {draft.editing ? (
              <p className="font-mono text-xs text-zinc-300">{draft.repo}</p>
            ) : (
              <label className="flex flex-col gap-1 text-xs text-zinc-300">
                Repository
                <input aria-label="Repository" value={draft.repo} onChange={(event) => setDraft({ ...draft, repo: event.target.value })}
                  placeholder="owner/name, host/owner/name or a remote address" spellCheck={false} autoCapitalize="off" autoCorrect="off" className={`${input} font-mono`} />
              </label>
            )}
            <label className="flex flex-col gap-1 text-xs text-zinc-300">
              Guidance
              <textarea aria-label="Guidance" value={draft.text} onChange={(event) => setDraft({ ...draft, text: event.target.value })}
                rows={5} placeholder="When a piece of work is done, run the prove-it skill and show its result." className={input} />
            </label>
            <div className="flex justify-end gap-2">
              <button type="button" onClick={() => { setDraft(null); setError(null) }} className="rounded px-3 py-1.5 text-sm text-zinc-400 hover:bg-zinc-800">Cancel</button>
              <button type="submit" disabled={!canSave} className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-50">
                {saving ? "Saving…" : "Save"}
              </button>
            </div>
          </form>
        ) : (
          <button type="button" onClick={() => { setDraft({ repo: "", text: "", editing: false }); setError(null) }}
            className="inline-flex w-fit items-center gap-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500">
            <Plus size={14} /> Add guidance
          </button>
        )}
      </div>
    </SettingsCard>
  )
}
