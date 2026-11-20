import { Link } from "react-router-dom"
import { helpSnippet, marked, queryWords, type SearchGroup } from "../../lib/settingsSearch"
import { quietButton } from "./SettingsUI"

// Mark is text with the parts a query matched highlighted.
function Mark({ text, words }: { text: string; words: string[] }) {
  return <>{marked(text, words).map((part, i) => part.hit ? <mark key={i} className="rounded-sm bg-lw-needs-soft px-px text-lw-needs">{part.text}</mark> : <span key={i}>{part.text}</span>)}</>
}

// SettingsSearch shows what a query found, grouped by section: each with
// its breadcrumb, label, key, value and source. active is the result the
// arrow keys are on.
export function SettingsSearch({ query, groups, total, active, onSelect }: {
  query: string; groups: SearchGroup[]; total: number; active: number; onSelect: () => void
}) {
  const words = queryWords(query)
  let index = -1
  return <section aria-label="Search results" className="flex flex-col gap-4">
    <p role="status" className="text-sm text-lw-text-2">{total === 0 ? `No settings match “${query.trim()}”. Try a setting's name, key or value.` : `${total} ${total === 1 ? "setting matches" : "settings match"} “${query.trim()}”`}</p>
    {groups.map((group) => <section key={group.section} aria-label={`${group.title} results`}>
      <h3 className="mb-1.5 px-1 font-lw-mono text-[10.5px] uppercase tracking-wider text-lw-text-2">{group.title}</h3>
      <ul className="divide-y divide-lw-line overflow-hidden rounded-xl border border-lw-line bg-lw-surface">
        {group.results.map((entry) => {
          index++
          const on = index === active
          const snippet = helpSnippet(entry, words)
          return <li key={entry.id}><Link to={`#${entry.id}`} onClick={onSelect} aria-current={on ? "true" : undefined} data-active={on || undefined}
            className={`flex items-center gap-3 px-4 py-3 hover:bg-lw-raised focus-visible:outline-lw-accent ${on ? "bg-lw-selected" : ""}`}>
            <span className="min-w-0 flex-1">
              <span className="block text-xs text-lw-text-2">{[group.title, entry.group].filter(Boolean).join(" › ")}</span>
              <span className="block text-sm text-lw-text"><Mark text={entry.title} words={words} /></span>
              {entry.key && <span className="block truncate font-lw-mono text-[11.5px] text-lw-text-2"><Mark text={entry.key} words={words} />{snippet && <> · “<Mark text={snippet} words={words} />”</>}</span>}
            </span>
            {(entry.value || entry.source) && <span className="shrink-0 text-right text-[13px] text-lw-text-2">
              {entry.value && <span className="block"><Mark text={entry.value} words={words} /></span>}
              {entry.source && <span className="block text-[11px]">{entry.source}</span>}
            </span>}
          </Link></li>
        })}
      </ul>
    </section>)}
  </section>
}

export function SettingsLoadState({ loading, error, stale, onRetry }: { loading: boolean; error: Error | null; stale: boolean; onRetry: () => void }) {
  return <>
    {loading && <p role="status" className="text-sm text-lw-text-2">Loading server settings…</p>}
    {error && <div><p role="alert" className="text-sm text-lw-error">Could not load settings: {error.message}{stale ? " Showing last loaded values." : ""}</p><button className={quietButton + " mt-2"} onClick={onRetry}>Retry settings</button></div>}
  </>
}
