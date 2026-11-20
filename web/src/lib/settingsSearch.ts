import type { ConfigList, ConfigSetting } from "./configSettings"
import { fileOnlyPlace, fileOnlyTitle, findSection, listPlace, settingPlace, settingsCards, settingsSections } from "./settingsCatalog"
import { formatValue } from "./settingValues"

// What Settings' search looks through: every registry setting by its label,
// key, help and value, the file-only keys, the lists and the cards.
export interface SearchEntry {
  id: string
  section: string
  group: string
  title: string
  key?: string
  help?: string
  value?: string
  source?: string
  terms?: string
}

const sourceWords: Record<string, string> = { "config.yaml": "config.yaml", database: "Set here", default: "Default" }

export function searchEntries(settings: ConfigSetting[], fileOnly: string[], fileOnlyValues: Record<string, string>, lists: ConfigList[], development: boolean): SearchEntry[] {
  return [
    ...settings.map((s) => ({
      id: `setting-${s.key}`, ...settingPlace(s), title: s.title, key: s.key, help: s.help ?? s.description,
      value: s.secret ? undefined : formatValue(s, s.value), source: sourceWords[s.source],
    })),
    ...fileOnly.map((key) => ({ id: `setting-${key}`, ...fileOnlyPlace(key), title: fileOnlyTitle(key), key, value: fileOnlyValues[key] || undefined, source: "config.yaml only" })),
    ...lists.filter((l) => l.key !== "macros").map((l) => ({ id: `list-${l.key}`, ...listPlace(l.key), title: l.title, key: l.key, value: l.entries.length ? `${l.entries.length}` : undefined })),
    ...settingsCards.filter((c) => c.id !== "development" || development).map((c) => ({ id: c.id, section: c.section, group: c.group, title: c.title, terms: c.terms })),
  ]
}

export function queryWords(query: string): string[] {
  return query.toLowerCase().trim().split(/\s+/).filter(Boolean)
}

// matches says whether every word of the query is in the entry's words.
export function matches(entry: SearchEntry, words: string[]): boolean {
  const section = findSection(entry.section)
  const text = [entry.title, entry.key, entry.help, entry.value, entry.terms, entry.group, section?.title].filter(Boolean).join(" ").toLowerCase()
  return words.every((w) => text.includes(w))
}

export interface SearchGroup { section: string; title: string; results: SearchEntry[] }

// search gives the matches grouped by section, in the nav's order, and
// the count for each section.
export function search(entries: SearchEntry[], query: string): { groups: SearchGroup[]; counts: Record<string, number>; total: number } {
  const words = queryWords(query)
  const hits = words.length ? entries.filter((e) => matches(e, words)) : []
  const groups: SearchGroup[] = []
  const counts: Record<string, number> = {}
  for (const section of settingsSections) {
    const results = hits.filter((h) => h.section === section.id)
    counts[section.id] = results.length
    if (results.length) groups.push({ section: section.id, title: section.title, results })
  }
  return { groups, counts, total: hits.length }
}

// marked splits text into the parts a query matched and those it did not.
export function marked(text: string, words: string[]): { text: string; hit: boolean }[] {
  if (!words.length || !text) return [{ text, hit: false }]
  const pattern = new RegExp(`(${words.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})`, "gi")
  return text.split(pattern).filter((part) => part !== "").map((part) => ({ text: part, hit: words.includes(part.toLowerCase()) }))
}

// helpSnippet is the part of a help line around a word the query matched
// that the label and key did not.
export function helpSnippet(entry: SearchEntry, words: string[]): string | undefined {
  if (!entry.help) return undefined
  const plain = `${entry.title} ${entry.key ?? ""}`.toLowerCase()
  const word = words.find((w) => !plain.includes(w) && entry.help!.toLowerCase().includes(w))
  if (!word) return undefined
  const at = entry.help.toLowerCase().indexOf(word)
  const start = Math.max(0, entry.help.lastIndexOf(" ", Math.max(0, at - 24)) + 1)
  const end = Math.min(entry.help.length, at + word.length + 24)
  return `${start > 0 ? "… " : ""}${entry.help.slice(start, end).trim()}${end < entry.help.length ? " …" : ""}`
}
