import type { ConfigSetting } from "../../lib/configSettings"
import { Link } from "react-router-dom"
import { fileOnlyPlace, findSection, listPlace, settingPlace, settingsCards, type Place } from "../../lib/settingsCatalog"
import { quietButton } from "./SettingsUI"

interface Entry extends Place { id: string; title: string; terms: string; scope?: string }

// SettingsSearch finds settings, lists and cards by their words, and links
// each to its home.
export function SettingsSearch({ query, settings, fileOnly, lists, onSelect, development }: {
  query: string; settings: ConfigSetting[]; fileOnly: string[]; lists: { key: string; title: string }[]; onSelect: () => void; development: boolean
}) {
  const entries: Entry[] = [
    ...settings.map((s) => ({ id: `setting-${s.key}`, title: s.title, ...settingPlace(s), terms: `${s.key} ${s.help ?? s.description ?? ""}`, scope: "This server" })),
    ...fileOnly.map((key) => ({ id: `setting-${key}`, title: key, ...fileOnlyPlace(key), terms: `${key} config.yaml file only`, scope: "This server · config.yaml only" })),
    ...lists.filter((list) => list.key !== "macros").map((list) => ({ id: `list-${list.key}`, title: list.title, ...listPlace(list.key), terms: list.key, scope: "This server" })),
    ...settingsCards.filter((card) => card.id !== "development" || development),
  ]
  const words = query.toLowerCase().trim().split(/\s+/)
  const results = entries.filter((entry) => {
    const section = findSection(entry.section)
    const text = `${entry.title} ${entry.terms} ${entry.group} ${section?.title ?? ""} ${section?.terms ?? ""}`.toLowerCase()
    return words.every((word) => text.includes(word))
  })
  return <section aria-label="Search results">
    <h2 className="mb-3 text-base font-semibold">Search results</h2>
    {results.length === 0 && <p role="status" className="text-sm text-lw-text-2">No settings match “{query}”. Try a setting name or key.</p>}
    <ul className="divide-y divide-lw-line">
      {results.map((entry) => <li key={entry.id}><Link to={`#${entry.id}`} onClick={onSelect} className="block rounded-lg px-3 py-3 hover:bg-lw-surface focus-visible:outline-lw-accent">
        <span className="block text-sm font-medium">{entry.title}</span>
        <span className="text-xs text-lw-text-2">{[findSection(entry.section)?.title, entry.group].filter(Boolean).join(" › ")} · {entry.scope ?? "This server"}</span>
      </Link></li>)}
    </ul>
  </section>
}

export function SettingsLoadState({ loading, error, stale, onRetry }: { loading: boolean; error: Error | null; stale: boolean; onRetry: () => void }) {
  return <>
    {loading && <p role="status" className="text-sm text-lw-text-2">Loading server settings…</p>}
    {error && <div><p role="alert" className="text-sm text-lw-error">Could not load settings: {error.message}{stale ? " Showing last loaded values." : ""}</p><button className={quietButton + " mt-2"} onClick={onRetry}>Retry settings</button></div>}
  </>
}
