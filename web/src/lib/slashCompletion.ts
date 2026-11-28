import type { SlashItem, SlashMatch } from "./slashMenu"

// The native panel's slash menu: what is searched for, which row is
// highlighted, and what picking an item puts in the message box.

export type BrowserFilter = "all" | "skill" | "command"

// What the menu searches for: the browser's own query while it is open,
// else the name after a slash that is the whole draft; none when the draft
// is anything else.
export function slashQueryOf(draft: string, browserOpen: boolean, browserQuery: string): string | undefined {
  return browserOpen ? browserQuery : /^\/(\S*)$/.exec(draft)?.[1]
}

// The items the browser's filter leaves; the menu typed after a slash
// offers every one.
export function filterSlashItems(items: SlashItem[], browserOpen: boolean, filter: BrowserFilter): SlashItem[] {
  return browserOpen && filter !== "all" ? items.filter((item) => item.type === filter) : items
}

// A list's identity: a highlight is kept only while the same list stands.
export function slashListKey(matches: SlashMatch[]): string {
  return matches.map((m) => m.item.type + ":" + m.item.name).join("\n")
}

export function activeSlashIndex(active: { list: string; index: number }, list: string, count: number): number {
  return active.list === list ? Math.min(active.index, Math.max(count - 1, 0)) : 0
}

// Completing an item fills the box and never sends: a command with a body
// expands to it, anything else becomes "/name ", ready for arguments. From
// the browser it joins what was written: a skill or a bare command before
// it, a command's body after it.
export function slashCompletion(item: SlashItem, draft: string, browserOpen: boolean): string {
  const completion = item.type === "command" && item.body ? item.body : `/${item.name} `
  if (!browserOpen || !draft) return completion
  return item.type === "skill" || !item.body ? `${completion}${draft}` : `${draft}\n${completion}`
}
