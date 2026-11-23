import { useEffect, useRef, useState } from "react"
import { Bot, FileText, FolderGit2, GitPullRequest, ListChecks, Trash2 } from "lucide-react"
import { pickerGroups } from "./stepPicker"

const GROUP_ICONS: Record<string, typeof Bot> = { Agents: Bot, Code: GitPullRequest, Board: ListChecks, Cleanup: Trash2 }
const iconFor = (group: string, type: string) => type.startsWith("repo.") ? FolderGit2 : GROUP_ICONS[group] ?? FileText

// StepPicker chooses a step type to add (LEG-507): a search, focused, over
// the offered types by group, one line each. A popover by its "+", or a
// sheet from the bottom on a phone. Enter takes the first match.
export function StepPicker({ onPick, onClose, narrow = false }: { onPick: (type: string) => void; onClose: () => void; narrow?: boolean }) {
  const [query, setQuery] = useState("")
  const search = useRef<HTMLInputElement>(null)
  useEffect(() => { search.current?.focus() }, [])
  const groups = pickerGroups(query)
  const first = groups[0]?.entries[0]
  return <div role="dialog" aria-label="Add a step" onKeyDown={event => { if (event.key === "Escape") { event.stopPropagation(); onClose() } }}
    className={narrow
      ? "fixed inset-x-0 bottom-0 z-50 flex max-h-[80vh] flex-col gap-2 rounded-t-xl border-t border-lw-line bg-lw-surface p-3 shadow-2xl"
      : "z-30 my-1 flex max-h-[28rem] flex-col gap-2 rounded-lg border border-lw-line bg-lw-surface p-2 shadow-xl"}>
    <input ref={search} type="search" aria-label="Search step types" placeholder="Search steps" value={query}
      className="min-h-9 w-full rounded-md border border-lw-line-strong bg-lw-ground px-2 text-sm text-lw-text"
      onChange={event => setQuery(event.target.value)}
      onKeyDown={event => { if (event.key === "Enter" && first) { event.preventDefault(); onPick(first.type) } }} />
    <div className="min-h-0 overflow-y-auto">
      {groups.length === 0 && <p className="px-2 py-3 text-sm text-lw-text-2">No step type matches “{query}”.</p>}
      {groups.map(({ group, entries }) => <section key={group} aria-label={group} className="flex flex-col py-1">
        <h3 className="px-2 py-1 text-[11px] font-semibold uppercase tracking-wide text-lw-faint">{group}</h3>
        {entries.map(entry => {
          const Icon = iconFor(group, entry.type)
          return <button key={entry.type} type="button" title={entry.description} className="flex min-h-9 min-w-0 items-center gap-2 rounded-md px-2 py-1 text-left text-sm hover:bg-lw-raised focus-visible:bg-lw-raised focus-visible:outline-none" onClick={() => onPick(entry.type)}>
            <Icon size={14} aria-hidden className="shrink-0 text-lw-faint" />
            <span className="shrink-0 font-medium">{entry.title}</span>
            <span className="min-w-0 flex-1 truncate text-xs text-lw-text-2">{entry.phrase}</span>
            <span className="hidden shrink-0 font-lw-mono text-[11px] text-lw-faint sm:inline">{entry.type}</span>
          </button>
        })}
      </section>)}
    </div>
    {narrow && <button type="button" className="min-h-9 rounded-md text-sm text-lw-text-2" onClick={onClose}>Cancel</button>}
  </div>
}
