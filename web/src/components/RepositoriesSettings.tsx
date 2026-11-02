import { useCallback, useEffect, useState } from "react"
import { AlertTriangle, GitBranch, Plus, Trash2 } from "lucide-react"
import { SettingsCard } from "./settings/SettingsUI"
import { addRepository, listRepositories, removeRepository, type NewRepository, type Repository } from "../lib/repositories"

const emptyDraft = (): NewRepository => ({ name: "", repo: "", base_branch: "", primary_dir: "" })

// RepositoriesSettings is where the person keeps the repositories agents work
// on: a GitHub repository the spawn dialog clones fresh for each agent, or a
// checkout on the server, or both. Those of config.yaml are listed but changed
// there. onChanged lets the page refresh what the dialogs offer.
export function RepositoriesSettings({ baseUrl, onChanged }: { baseUrl: string; onChanged?: () => void }) {
  const [repositories, setRepositories] = useState<Repository[] | null>(null)
  const [draft, setDraft] = useState<NewRepository | null>(null)
  const [confirming, setConfirming] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setRepositories(await listRepositories(baseUrl))
    } catch (reason) {
      setRepositories([])
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])

  useEffect(() => { void load() }, [load])

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setError(null)
    try {
      await addRepository(baseUrl, {
        name: draft.name.trim(),
        repo: draft.repo.trim(),
        base_branch: draft.base_branch.trim(),
        primary_dir: draft.primary_dir.trim(),
      })
      setDraft(null)
      await load()
      onChanged?.()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSaving(false)
    }
  }

  const remove = async (id: number) => {
    setError(null)
    try {
      await removeRepository(baseUrl, id)
      setConfirming(null)
      await load()
      onChanged?.()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  const canSave = draft !== null && draft.name.trim() !== "" && (draft.repo.trim() !== "" || draft.primary_dir.trim() !== "") && !saving
  const input = "rounded border border-zinc-700 bg-zinc-950 px-2 py-2 text-sm text-zinc-200 sm:py-1.5"

  return (
    <SettingsCard id="repositories" icon={GitBranch} title="Repositories" description="What agents work on. The spawn dialog offers each as a fresh clone of the GitHub repository on a branch of its own, or as its folder on this server.">
      <div className="flex flex-col gap-3">
        <p className="text-xs text-zinc-500">
          Legato clones a repository only when its GitHub credential lists it. List a whole owner in config.yaml with <code className="font-mono">owner/*</code> and every repository of that owner you add here can be cloned.
        </p>
        {error && <p role="alert" className="rounded border border-red-900 bg-red-950/60 px-2 py-1.5 text-xs text-red-300">{error}</p>}

        {repositories && repositories.length > 0 && (
          <ul className="divide-y divide-zinc-800 rounded border border-zinc-800">
            {repositories.map((repository) => (
              <li key={repository.id ?? `config-${repository.name}`} aria-label={repository.name} className="flex flex-col gap-1 px-3 py-2">
                <div className="flex items-center gap-2">
                  <span className="text-sm text-zinc-200">{repository.name}</span>
                  {repository.configured && <span className="rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400" title="Defined in the configuration file">config.yaml</span>}
                  {!repository.configured && repository.id ? (
                    <span className="ml-auto flex items-center gap-1">
                      {confirming === repository.id ? (
                        <>
                          <button type="button" onClick={() => void remove(repository.id!)} className="rounded bg-red-600 px-2 py-1 text-xs text-white hover:bg-red-500">Remove</button>
                          <button type="button" onClick={() => setConfirming(null)} className="rounded px-2 py-1 text-xs text-zinc-400 hover:bg-zinc-800">Keep</button>
                        </>
                      ) : (
                        <button type="button" aria-label={`Remove ${repository.name}`} onClick={() => setConfirming(repository.id!)}
                          className="flex size-9 items-center justify-center rounded text-zinc-400 hover:bg-zinc-800 hover:text-red-300 sm:size-7">
                          <Trash2 size={14} />
                        </button>
                      )}
                    </span>
                  ) : null}
                </div>
                <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5 text-xs text-zinc-500">
                  {repository.repo && <span className="truncate font-mono">{repository.repo}</span>}
                  {repository.base_branch && <span className="font-mono text-zinc-600">from {repository.base_branch}</span>}
                  {repository.primary_dir && <span className="truncate font-mono" title={repository.primary_dir}>{repository.primary_dir}</span>}
                </div>
                {repository.clone_problem && (
                  <p className="flex items-start gap-1.5 text-xs text-amber-300/90">
                    <AlertTriangle size={12} className="mt-0.5 shrink-0" aria-hidden="true" />
                    <span>Can't be cloned: {repository.clone_problem}</span>
                  </p>
                )}
              </li>
            ))}
          </ul>
        )}

        {draft ? (
          <form
            className="flex flex-col gap-3 rounded border border-zinc-700 p-3"
            onSubmit={(event) => { event.preventDefault(); if (canSave) void save() }}
          >
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <label className="flex flex-col gap-1 text-xs text-zinc-300">
                Name
                <input aria-label="Name" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="Legato" className={input} />
              </label>
              <label className="flex flex-col gap-1 text-xs text-zinc-300">
                GitHub repository
                <input aria-label="GitHub repository" value={draft.repo} onChange={(event) => setDraft({ ...draft, repo: event.target.value })}
                  placeholder="owner/name" spellCheck={false} autoCapitalize="off" autoCorrect="off" className={`${input} font-mono`} />
              </label>
              <label className="flex flex-col gap-1 text-xs text-zinc-300">
                Base branch
                <input aria-label="Base branch" value={draft.base_branch} onChange={(event) => setDraft({ ...draft, base_branch: event.target.value })}
                  placeholder="the default branch" spellCheck={false} autoCapitalize="off" autoCorrect="off" className={`${input} font-mono`} />
              </label>
              <label className="flex flex-col gap-1 text-xs text-zinc-300">
                <span>Folder on this server <span className="text-zinc-500">· optional</span></span>
                <input aria-label="Folder on this server" value={draft.primary_dir} onChange={(event) => setDraft({ ...draft, primary_dir: event.target.value })}
                  placeholder="~/src/legato" spellCheck={false} autoCapitalize="off" autoCorrect="off" className={`${input} font-mono`} />
              </label>
            </div>
            <div className="flex justify-end gap-2">
              <button type="button" onClick={() => { setDraft(null); setError(null) }} className="rounded px-3 py-1.5 text-sm text-zinc-400 hover:bg-zinc-800">Cancel</button>
              <button type="submit" disabled={!canSave} className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-50">
                {saving ? "Saving…" : "Save"}
              </button>
            </div>
          </form>
        ) : (
          <button type="button" onClick={() => { setDraft(emptyDraft()); setError(null) }}
            className="inline-flex w-fit items-center gap-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500">
            <Plus size={14} /> Add repository
          </button>
        )}
      </div>
    </SettingsCard>
  )
}
