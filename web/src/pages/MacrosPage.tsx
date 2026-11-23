import { fetchAdapters } from "../lib/adapters"
import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Lock, Plus, Star, Zap } from "lucide-react"
import { MacroForm } from "../components/MacroForm"
import { primaryButton, quietButton } from "../components/settings/SettingsUI"
import { InlineNotice } from "../components/ui/InlineNotice"
import { LoadingState } from "../components/ui/LoadingState"
import { useServer } from "../hooks/useServer"
import { agentKindLabel } from "../lib/agentKind"
import { appliesLabel, deleteMacro, fetchMacroList, macroAgentLine, resetMacro, setMacroFavorite, type Macro } from "../lib/macros"
import { queryKeys } from "../lib/query"

const DOCS = "https://cpave3.github.io/legato/using/agents/#macros"
const PLACES: { label: string; code?: string }[] = [
  { label: "Card menu" },
  { label: "Pull request line" },
  { label: "Spawn dialog" },
  { label: "Agents page · M" },
  { label: "TUI · m" },
  { label: "", code: "legato macro run" },
  { label: "Workflows · ", code: "with.macro" },
]
const dangerButton = "inline-flex min-h-10 items-center justify-center rounded-lg border border-lw-danger-line px-3.5 text-sm text-lw-danger transition-colors hover:bg-lw-error-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-50"

export type { Adapters } from "../lib/adapters"

// The macro being made (null name) or edited, or none.
type Editing = { macro: Macro | null } | null

export function MacrosPage() {
  const { baseUrl } = useServer()
  const client = useQueryClient()
  const list = useQuery({ queryKey: queryKeys.macros(baseUrl), queryFn: () => fetchMacroList(baseUrl) })
  const adapters = useQuery({ queryKey: queryKeys.adapters(baseUrl), queryFn: () => fetchAdapters(baseUrl) })
  const [editing, setEditing] = useState<Editing>(null)
  const [deleting, setDeleting] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const refresh = () => client.invalidateQueries({ queryKey: queryKeys.macros(baseUrl) })
  const act = async (work: () => Promise<void>) => {
    if (busy) return
    setBusy(true)
    setError("")
    try {
      await work()
      setDeleting(null)
      await refresh()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  const macros = list.data?.macros ?? []
  const builtins = macros.filter((macro) => macro.builtin)
  const yours = macros.filter((macro) => !macro.builtin)
  const newMacro = () => { setError(""); setEditing({ macro: null }) }

  const row = (macro: Macro) => {
    const agentLine = macroAgentLine(macro, agentKindLabel)
    const confirming = deleting === macro.name
    return (
      <li key={macro.name} aria-label={macro.name} className={`flex flex-wrap items-start gap-x-4 gap-y-3 border-t border-lw-line px-4 py-3.5 first:border-t-0 ${confirming ? "bg-lw-raised" : ""}`}>
        <div className="flex min-w-0 flex-[1_1_420px] flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <button
              type="button"
              aria-label={`Favourite: ${macro.name}`}
              aria-pressed={Boolean(macro.favorite)}
              title={macro.pinned ? "Star it in config.yaml (favorite: true)" : macro.favorite ? "Favourite: listed first in macro pickers" : "Star to list it first in macro pickers"}
              disabled={busy || macro.pinned}
              onClick={() => void act(() => setMacroFavorite(baseUrl, macro.name, !macro.favorite))}
              className={`-my-2 -ml-2 inline-flex size-10 shrink-0 items-center justify-center rounded-lg transition-colors hover:bg-lw-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:hover:bg-transparent ${macro.favorite ? "text-lw-accent-text" : "text-lw-text-2"} ${macro.pinned ? "opacity-60" : ""}`}
            >
              <Star size={16} aria-hidden fill={macro.favorite ? "currentColor" : "none"} />
            </button>
            <span className="text-[15px] font-medium text-lw-text">{macro.name}</span>
            {macro.overridden && !macro.pinned && <span className="rounded bg-lw-needs-soft px-1.5 py-0.5 text-[11px] text-lw-needs">Edited</span>}
            {macro.pinned && <span className="inline-flex items-center gap-1 rounded bg-lw-raised px-1.5 py-0.5 text-[11px] text-lw-text-2"><Lock size={10} aria-hidden />config.yaml</span>}
          </div>
          <div className="flex flex-wrap gap-x-3.5 gap-y-1 text-xs text-lw-text-2">
            <span>{appliesLabel(macro)}</span>
            {agentLine && <span className="font-medium text-lw-text">{agentLine}</span>}
          </div>
          {confirming
            ? <p className="mt-1 text-sm text-lw-text">Delete {macro.name}? Workflows that use it will fail.</p>
            : <p className="truncate font-lw-mono text-xs text-lw-text-2">{macro.keys}</p>}
        </div>
        {macro.pinned
          ? <span className="self-center text-xs text-lw-text-2">Edit in config.yaml</span>
          : confirming
            ? <span className="flex gap-1.5 self-end">
              <button type="button" className={dangerButton} disabled={busy} onClick={() => void act(() => deleteMacro(baseUrl, macro.name))}>Delete</button>
              <button type="button" className={quietButton} onClick={() => setDeleting(null)}>Cancel</button>
            </span>
            : <span className="flex gap-1.5">
              <button type="button" className={quietButton} aria-label={`Edit ${macro.name}`} onClick={() => { setError(""); setEditing({ macro }) }}>Edit</button>
              {macro.builtin
                ? macro.overridden && <button type="button" className={quietButton} aria-label={`Reset ${macro.name}`} disabled={busy} onClick={() => void act(() => resetMacro(baseUrl, macro.name))}>Reset</button>
                : <button type="button" className={dangerButton} aria-label={`Delete ${macro.name}`} onClick={() => { setError(""); setDeleting(macro.name) }}>Delete</button>}
            </span>}
      </li>
    )
  }

  return (
    <main className="mx-auto w-full max-w-[980px] space-y-4 p-4 font-lw text-lw-text md:px-10 md:py-8">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-semibold">Macros</h1>
        <button type="button" className={`${primaryButton} ml-auto`} onClick={newMacro}><Plus size={16} aria-hidden />New macro</button>
      </div>
      <section aria-label="What a macro is" className="flex flex-col gap-2.5 rounded-xl border border-lw-line bg-lw-surface px-4 py-3.5">
        <p className="text-sm leading-[21px]">A saved message for an agent. Run on a task, it starts the task’s agent with it, or becomes the running agent’s next message.</p>
        <div className="flex flex-wrap items-center gap-1.5">
          <ul aria-label="Where to run one" className="flex flex-wrap gap-1.5">
            {PLACES.map((place) => <li key={place.label + (place.code ?? "")} className="rounded-full border border-lw-line bg-lw-ground px-2.5 py-1 text-xs">{place.label}{place.code && <code className="font-lw-mono text-lw-accent-text">{place.code}</code>}</li>)}
          </ul>
          <a href={DOCS} target="_blank" rel="noreferrer" className="ml-1 text-xs text-lw-link">Docs</a>
        </div>
      </section>
      {error && <p role="alert" className="text-sm text-lw-error">{error}</p>}
      {list.isPending && <LoadingState label="Loading macros…" size="inline" />}
      {list.isError && <InlineNotice variant="error">
        Could not load macros: {list.error.message}
        <button type="button" className={quietButton} onClick={() => void list.refetch()}>Retry</button>
      </InlineNotice>}
      {list.data && <>
        <section aria-labelledby="macros-builtin" className="space-y-2.5 pt-3">
          <h2 id="macros-builtin" className="text-[13px] font-semibold uppercase tracking-[1px] text-lw-text-2">Built in</h2>
          <ul className="overflow-hidden rounded-xl border border-lw-line bg-lw-surface">{builtins.map(row)}</ul>
        </section>
        <section aria-labelledby="macros-yours" className="space-y-2.5 pt-3">
          <h2 id="macros-yours" className="text-[13px] font-semibold uppercase tracking-[1px] text-lw-text-2">Yours</h2>
          {yours.length > 0
            ? <ul className="overflow-hidden rounded-xl border border-lw-line bg-lw-surface">{yours.map(row)}</ul>
            : <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-lw-line-strong bg-lw-surface px-8 py-11 text-center">
              <span className="flex size-12 items-center justify-center rounded-xl bg-lw-accent-soft text-lw-accent-text"><Zap size={24} aria-hidden /></span>
              <h3 className="text-[17px] font-semibold">No macros of your own</h3>
              <button type="button" className={primaryButton} onClick={newMacro}><Plus size={16} aria-hidden />New macro</button>
            </div>}
        </section>
      </>}
      {editing && <MacroForm
        baseUrl={baseUrl}
        macro={editing.macro}
        macros={macros}
        adapters={adapters.data ?? { adapters: [], default: "" }}
        tiers={list.data?.tiers ?? {}}
        onClose={() => setEditing(null)}
        onSaved={async () => { setEditing(null); await refresh() }}
      />}
    </main>
  )
}
