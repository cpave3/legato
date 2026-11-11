// A command or skill the slash menu offers. A command's body is the prompt it
// stands for.
export type SlashItem = {
  type: "command" | "skill"
  name: string
  description: string
  body?: string
}

export type SlashMatch = {
  item: SlashItem
  // Indices of the name's characters the query matched, for highlighting.
  matched: number[]
}

// How well a name matches, best first. Lower ranks sort first.
const EXACT = 0
const PREFIX = 1
const WORD_START = 2
const INSIDE = 3
const SCATTERED = 4
const DESCRIPTION = 5

const SEPARATOR = /[-_:\s/.]/

// Where each word of a name starts: the first character, and every character
// after a separator, so "review" finds "3-tier-review" and "plugin:review".
function wordStarts(name: string): number[] {
  const starts = [0]
  for (let i = 1; i < name.length; i++) {
    if (SEPARATOR.test(name[i - 1]) && !SEPARATOR.test(name[i])) starts.push(i)
  }
  return starts
}

function span(from: number, length: number): number[] {
  return Array.from({ length }, (_, i) => from + i)
}

type Scored = SlashMatch & { rank: number; at: number }

function matchName(name: string, query: string): Omit<Scored, "item"> | null {
  if (name === query) return { rank: EXACT, at: 0, matched: span(0, query.length) }
  if (name.startsWith(query)) return { rank: PREFIX, at: 0, matched: span(0, query.length) }
  const starts = wordStarts(name)
  for (const start of starts) {
    if (start > 0 && name.startsWith(query, start)) return { rank: WORD_START, at: start, matched: span(start, query.length) }
  }
  const inside = name.indexOf(query)
  if (inside >= 0) return { rank: INSIDE, at: inside, matched: span(inside, query.length) }
  // The query's letters in order, starting at the start of a word. A query
  // whose first letter falls in the middle of a word matches too loosely.
  for (const start of starts) {
    if (name[start] !== query[0]) continue
    const matched = [start]
    let at = start + 1
    for (let q = 1; q < query.length && at <= name.length; q++) {
      at = name.indexOf(query[q], at)
      if (at < 0) break
      matched.push(at)
      at++
    }
    if (matched.length === query.length) return { rank: SCATTERED, at: start, matched }
  }
  return null
}

function matchDescription(description: string, query: string): boolean {
  if (query.length < 3) return false
  const text = description.toLowerCase()
  return wordStarts(text).some((start) => text.startsWith(query, start))
}

// The items that match a query, best first: an exact name, then a name that
// starts with it, one with a word that starts with it, one that contains it,
// one with its letters in order, and last a description with a word that
// starts with it. Commands and skills are ranked together.
export function rankSlashItems(items: SlashItem[], query: string): SlashMatch[] {
  const q = query.toLowerCase()
  const scored: Scored[] = []
  for (const item of items) {
    if (!q) {
      scored.push({ item, matched: [], rank: 0, at: 0 })
      continue
    }
    const byName = matchName(item.name.toLowerCase(), q)
    if (byName) scored.push({ item, ...byName })
    else if (matchDescription(item.description, q)) scored.push({ item, matched: [], rank: DESCRIPTION, at: 0 })
  }
  scored.sort((a, b) => a.rank - b.rank || a.at - b.at || a.item.name.length - b.item.name.length || a.item.name.localeCompare(b.item.name))
  return scored.map(({ item, matched }) => ({ item, matched }))
}
