import { executors, workflowSchema } from "../../lib/workflowSchema"

// The step picker's list (LEG-507): the offered step types, by the
// catalog's groups, found by name, type or description.

export type PickerEntry = { type: string; title: string; phrase: string; description: string }

const GROUPS: string[] = workflowSchema["x-legato"].step_groups ?? []

const phraseOf = (description: string) => description.split(/\.(\s|$)/)[0].trim()

/** pickerGroups are the offered step types matching query, by group, in
 * the catalog's order; a group with none is left out. */
export function pickerGroups(query: string): { group: string; entries: PickerEntry[] }[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  // A word matches the start of a word of the title or description, or
  // any part of the type (github.pr).
  const offered = executors.filter(e => {
    const tokens = `${e.title} ${e.type} ${e.description}`.toLowerCase().split(/[^a-z0-9]+/)
    return words.every(word => (word.includes(".") && e.type.includes(word)) || tokens.some(token => token.startsWith(word)))
  })
  const groups = [...GROUPS, ...new Set(offered.map(e => e.group ?? "Other").filter(g => !GROUPS.includes(g)))]
  return groups.map(group => ({
    group,
    entries: offered.filter(e => (e.group ?? "Other") === group).map(e => ({ type: e.type, title: e.title, phrase: phraseOf(e.description), description: e.description })),
  })).filter(g => g.entries.length > 0)
}
