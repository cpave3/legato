import { useCallback, useEffect, useState } from "react"
import { AlertTriangle, GitBranch, Plus, Trash2 } from "lucide-react"
import { SettingsCard } from "./settings/SettingsUI"
import { addRepository, listRepositories, removeRepository, type NewRepository, type Repository } from "../lib/repositories"
import { useRepositoryCache } from "../hooks/usePreparingAgents"
import { progressDetail, type RepositoryCacheStatus } from "../lib/preparingAgents"
import { reasonText } from "../lib/nativeAgentView"

function fetchedAgo(iso?: string): string {
  if (!iso) return ""
  const when = new Date(iso)
  if (Number.isNaN(when.getTime())) return ""
  return ` · fetched ${when.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}`
}

// CopyStatus says how a repository's copy in Legato's cache stands: an
// agent in a new clone of it waits for nothing but its clone once copied.
function CopyStatus({ status }: { status: RepositoryCacheStatus }) {
  switch (status.state) {
    case "copying":
    case "fetching": {
      const detail = progressDetail(status)
      const label = `${status.state === "copying" ? "Copying" : "Fetching"}${detail ? ` · ${detail}` : ""}`
      return (
        <div className="flex flex-col gap-1">
          <p className="text-xs text-zinc-400">{label}</p>
          {status.percent != null && (
            <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={status.percent}
              className="h-1 max-w-xs overflow-hidden rounded-full bg-zinc-800">
              <div className="h-full rounded-full bg-emerald-500 transition-[width] duration-300 motion-reduce:transition-none" style={{ width: `${status.percent}%` }} />
            </div>
          )}
        </div>
      )
    }
    case "failed":
      return <p className="text-xs text-amber-300/90">Copy failed: {status.error}</p>
    case "ready":
      return <p className="text-xs text-zinc-500">Copied{fetchedAgo(status.fetched_at)}</p>
    default:
      return <p className="text-xs text-zinc-500">Not copied yet</p>
  }
}

const emptyDraft = (): NewRepository => ({ name: "", repo: "", base_branch: "", primary_dir: "" })

const trimmedDraft = (draft: NewRepository): NewRepository => ({
  name: draft.name.trim(),
  repo: draft.repo.trim(),
  base_branch: draft.base_branch.trim(),
  primary_dir: draft.primary_dir.trim(),
})

// A repository needs a name, and a GitHub repository or a folder or both.
const completeDraft = (draft: NewRepository) => draft.name.trim() !== "" && (draft.repo.trim() !== "" || draft.primary_dir.trim() !== "")

const input = "rounded border border-zinc-700 bg-zinc-950 px-2 py-2 text-sm text-zinc-200 sm:py-1.5"

// RepositoriesSettings is where the person keeps the repositories agents work
// on: a GitHub repository the spawn dialog clones fresh for each agent, or a
// checkout on the server, or both. Those of config.yaml are listed but changed
// there. onChanged lets the page refresh what the dialogs offer.
export function RepositoriesSettings({ baseUrl, onChanged }: { baseUrl: string; onChanged?: () => void }) {
  const [draft, setDraft] = useState<NewRepository | null>(null)
  const [confirming, setConfirming] = useState<number | null>(null)
  const { repositories, error, setError, saving, save, remove } = useRepositories(baseUrl, onChanged)
  // Copies as git goes: repository_cache messages keep this up to date.
  const { data: liveCache = {} } = useRepositoryCache()

  const startOrStop = (next: NewRepository | null) => { setDraft(next); setError(null) }

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
              <RepositoryRow key={repository.id ?? `config-${repository.name}`} repository={repository} cache={liveCache[repository.repo]}
                confirming={confirming === repository.id} onConfirm={setConfirming}
                onRemove={(id) => void remove(id, () => setConfirming(null))} />
            ))}
          </ul>
        )}

        {draft ? (
          <RepositoryForm draft={draft} saving={saving} onChange={setDraft} onCancel={() => startOrStop(null)}
            onSave={() => void save(trimmedDraft(draft), () => setDraft(null))} />
        ) : (
          <button type="button" onClick={() => startOrStop(emptyDraft())}
            className="inline-flex w-fit items-center gap-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500">
            <Plus size={14} /> Add repository
          </button>
        )}
      </div>
    </SettingsCard>
  )
}

// useRepositories lists the repositories and adds and removes them, saying
// why a change failed in error; after runs once a change is made.
function useRepositories(baseUrl: string, onChanged?: () => void) {
  const [repositories, setRepositories] = useState<Repository[] | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setRepositories(await listRepositories(baseUrl))
    } catch (reason) {
      setRepositories([])
      setError(reasonText(reason))
    }
  }, [baseUrl])

  useEffect(() => { void load() }, [load])

  // changed runs a change, then lists again and tells the page.
  const changed = async (change: () => Promise<unknown>, after: () => void) => {
    setError(null)
    try {
      await change()
      after()
      await load()
      onChanged?.()
    } catch (reason) {
      setError(reasonText(reason))
    }
  }

  const save = async (repository: NewRepository, after: () => void) => {
    setSaving(true)
    try {
      await changed(() => addRepository(baseUrl, repository), after)
    } finally {
      setSaving(false)
    }
  }

  const remove = (id: number, after: () => void) => changed(() => removeRepository(baseUrl, id), after)
  return { repositories, error, setError, saving, save, remove }
}

// RepositoryRow is one repository: its name, where it comes from, how its
// copy stands and, for one added here, Remove after asking.
function RepositoryRow({ repository, cache, confirming, onConfirm, onRemove }: {
  repository: Repository; cache?: RepositoryCacheStatus; confirming: boolean
  onConfirm: (id: number | null) => void; onRemove: (id: number) => void
}) {
  const copy = cache ?? repository.cache
  return (
    <li aria-label={repository.name} className="flex flex-col gap-1 px-3 py-2">
      <div className="flex items-center gap-2">
        <span className="text-sm text-zinc-200">{repository.name}</span>
        {repository.configured && <span className="rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400" title="Defined in the configuration file">config.yaml</span>}
        {!repository.configured && repository.id ? (
          <RemoveRepository id={repository.id} name={repository.name} confirming={confirming} onConfirm={onConfirm} onRemove={onRemove} />
        ) : null}
      </div>
      <RepositoryPlace repository={repository} />
      {repository.repo && !repository.clone_problem && copy && <CopyStatus status={copy} />}
      {repository.clone_problem && (
        <p className="flex items-start gap-1.5 text-xs text-amber-300/90">
          <AlertTriangle size={12} className="mt-0.5 shrink-0" aria-hidden="true" />
          <span>Can't be cloned: {repository.clone_problem}</span>
        </p>
      )}
    </li>
  )
}

// RepositoryPlace is where a repository comes from: GitHub, the branch new
// work starts from, and its folder on this server.
function RepositoryPlace({ repository }: { repository: Repository }) {
  return (
    <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5 text-xs text-zinc-500">
      {repository.repo && <span className="truncate font-mono">{repository.repo}</span>}
      {repository.base_branch && <span className="font-mono text-zinc-600">from {repository.base_branch}</span>}
      {repository.primary_dir && <span className="truncate font-mono" title={repository.primary_dir}>{repository.primary_dir}</span>}
    </div>
  )
}

function RemoveRepository({ id, name, confirming, onConfirm, onRemove }: {
  id: number; name: string; confirming: boolean; onConfirm: (id: number | null) => void; onRemove: (id: number) => void
}) {
  return (
    <span className="ml-auto flex items-center gap-1">
      {confirming ? (
        <>
          <button type="button" onClick={() => onRemove(id)} className="rounded bg-red-600 px-2 py-1 text-xs text-white hover:bg-red-500">Remove</button>
          <button type="button" onClick={() => onConfirm(null)} className="rounded px-2 py-1 text-xs text-zinc-400 hover:bg-zinc-800">Keep</button>
        </>
      ) : (
        <button type="button" aria-label={`Remove ${name}`} onClick={() => onConfirm(id)}
          className="flex size-9 items-center justify-center rounded text-zinc-400 hover:bg-zinc-800 hover:text-red-300 sm:size-7">
          <Trash2 size={14} />
        </button>
      )}
    </span>
  )
}

function RepositoryForm({ draft, saving, onChange, onCancel, onSave }: {
  draft: NewRepository; saving: boolean; onChange: (draft: NewRepository) => void; onCancel: () => void; onSave: () => void
}) {
  const canSave = completeDraft(draft) && !saving
  return (
    <form
      className="flex flex-col gap-3 rounded border border-zinc-700 p-3"
      onSubmit={(event) => { event.preventDefault(); if (canSave) onSave() }}
    >
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label className="flex flex-col gap-1 text-xs text-zinc-300">
          Name
          <input aria-label="Name" value={draft.name} onChange={(event) => onChange({ ...draft, name: event.target.value })} placeholder="Legato" className={input} />
        </label>
        <label className="flex flex-col gap-1 text-xs text-zinc-300">
          GitHub repository
          <input aria-label="GitHub repository" value={draft.repo} onChange={(event) => onChange({ ...draft, repo: event.target.value })}
            placeholder="owner/name" spellCheck={false} autoCapitalize="off" autoCorrect="off" className={`${input} font-mono`} />
        </label>
        <label className="flex flex-col gap-1 text-xs text-zinc-300">
          Base branch
          <input aria-label="Base branch" value={draft.base_branch} onChange={(event) => onChange({ ...draft, base_branch: event.target.value })}
            placeholder="the default branch" spellCheck={false} autoCapitalize="off" autoCorrect="off" className={`${input} font-mono`} />
        </label>
        <label className="flex flex-col gap-1 text-xs text-zinc-300">
          <span>Folder on this server <span className="text-zinc-500">· optional</span></span>
          <input aria-label="Folder on this server" value={draft.primary_dir} onChange={(event) => onChange({ ...draft, primary_dir: event.target.value })}
            placeholder="~/src/legato" spellCheck={false} autoCapitalize="off" autoCorrect="off" className={`${input} font-mono`} />
        </label>
      </div>
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="rounded px-3 py-1.5 text-sm text-zinc-400 hover:bg-zinc-800">Cancel</button>
        <button type="submit" disabled={!canSave} className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-50">
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
    </form>
  )
}
