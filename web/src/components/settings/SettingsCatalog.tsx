import type { ConfigSetting } from "../../lib/configSettings"
import { Link } from "react-router-dom"
import { isAdvancedSetting, settingOwner, settingsCards, settingsCategories, type SettingsCategory } from "../../lib/settingsCatalog"
import { RegistrySettings } from "./RegistrySettings"
import { ListSettings } from "./ListSettings"
import { quietButton } from "./SettingsUI"

export function SettingsSearch({ query, settings, fileOnly, lists, onSelect, development }: {
  query: string; settings: ConfigSetting[]; fileOnly: string[]; lists: { key: string; title: string }[]; onSelect: () => void; development: boolean
}) {
  const entries = [
    ...settings.map((s) => ({ id: `setting-${s.key}`, title: s.title, category: settingOwner(s.key), terms: `${s.key} ${s.description ?? ""}`, scope: "This server" })),
    ...fileOnly.map((key) => ({ id: `setting-${key}`, title: key, category: settingOwner(key), terms: `${key} config.yaml file only`, scope: "This server · File only" })),
    ...lists.map((list) => ({ id: `list-${list.key}`, title: list.title, category: settingOwner(list.key), terms: list.key, scope: "This server" })),
    ...settingsCards.filter((card) => card.id !== "development" || development),
  ]
  const words = query.toLowerCase().trim().split(/\s+/)
  const results = entries.filter((entry) => {
    const category = settingsCategories.find((c) => c.id === entry.category)!
    const text = `${entry.title} ${entry.terms} ${category.title} ${category.terms}`.toLowerCase()
    return words.every((word) => text.includes(word))
  })
  return <section aria-label="Search results">
    <h2 className="mb-3 text-base font-semibold">Search results</h2>
    {results.length === 0 && <p role="status" className="text-sm text-lw-text-2">No settings match “{query}”. Try a setting name or key.</p>}
    <ul className="divide-y divide-lw-line">
      {results.map((entry) => <li key={entry.id}><Link to={`#${entry.id}`} onClick={onSelect} className="block rounded-lg px-3 py-3 hover:bg-lw-surface focus-visible:outline-lw-accent">
        <span className="block text-sm font-medium">{entry.title}</span>
        <span className="text-xs text-lw-text-2">{settingsCategories.find((c) => c.id === entry.category)!.title} · {entry.scope ?? "This server"}</span>
      </Link></li>)}
    </ul>
  </section>
}

export function CategorySettings({ category, settings, fileOnly, baseUrl, listKeys }: { category: SettingsCategory; settings: ConfigSetting[]; fileOnly: string[]; baseUrl: string; listKeys: string[] }) {
  const owned = settings.filter((s) => settingOwner(s.key) === category && !["web.tint", "updates.mode"].includes(s.key) && !s.key.startsWith("integrations.mqtt.") && !s.key.startsWith("web.rail."))
  const regular = owned.filter((s) => !isAdvancedSetting(s.key))
  const advanced = owned.filter((s) => isAdvancedSetting(s.key))
  return <>
    <RegistrySettings settings={regular} baseUrl={baseUrl} prefixes={regular.map((s) => s.key)} />
    {advanced.length > 0 && <section aria-label="Advanced"><h3 className="mb-2 text-sm font-medium">Advanced</h3><RegistrySettings settings={advanced} baseUrl={baseUrl} prefixes={advanced.map((s) => s.key)} /></section>}
    {/* Macros have their own page, which Settings links to. */}
    <ListSettings baseUrl={baseUrl} lists={listKeys.filter((key) => settingOwner(key) === category && key !== "macros")} />
    {fileOnly.filter((key) => settingOwner(key) === category).map((key) => <section key={key} id={`setting-${key}`} tabIndex={-1} className="scroll-mt-40 rounded-lg border border-lw-line p-4">
      <h3 className="break-all text-sm font-medium">{key}</h3>
      <p className="mt-1 text-sm text-lw-text-2">File only · Read-only here. Edit <code className="break-all">{key}</code> in this server’s <code>config.yaml</code>, then restart Legato to apply the change.</p>
    </section>)}
  </>
}

export function SettingsLoadState({ loading, error, stale, onRetry }: { loading: boolean; error: Error | null; stale: boolean; onRetry: () => void }) {
  return <>
    {loading && <p role="status" className="text-sm text-lw-text-2">Loading server settings…</p>}
    {error && <div><p role="alert" className="text-sm text-lw-error">Could not load settings: {error.message}{stale ? " Showing last loaded values." : ""}</p><button className={quietButton + " mt-2"} onClick={onRetry}>Retry settings</button></div>}
  </>
}
