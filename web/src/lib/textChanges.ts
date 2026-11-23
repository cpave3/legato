// textChanges are the small edits that turn one text into another, for an
// editor that should keep its cursor, scroll and marks when the text is
// changed from outside it (a form edit, an undo). The changes are in the
// first text's offsets, in order and apart, as CodeMirror takes them.

export type TextChange = { from: number; to: number; insert: string }

// Past this many lines on a side, the middle is one change: a line diff of
// it would cost more than it saves.
const MAX_LINES = 2000

const lines = (text: string) => text.match(/[^\n]*\n|[^\n]+$/g) ?? []

// trimmed is the change from..to → insert without the ends both share.
function trimmed(before: string, from: number, to: number, insert: string): TextChange {
  let start = 0
  const old = before.slice(from, to)
  while (start < old.length && start < insert.length && old[start] === insert[start]) start++
  let end = 0
  while (end < old.length - start && end < insert.length - start && old[old.length - 1 - end] === insert[insert.length - 1 - end]) end++
  return { from: from + start, to: to - end, insert: insert.slice(start, insert.length - end) }
}

export function textChanges(before: string, after: string): TextChange[] {
  if (before === after) return []
  let prefix = 0
  while (prefix < before.length && prefix < after.length && before[prefix] === after[prefix]) prefix++
  // Back to a line's start, so the line diff sees whole lines.
  prefix = before.lastIndexOf("\n", prefix - 1) + 1
  let suffix = 0
  while (suffix < before.length - prefix && suffix < after.length - prefix && before[before.length - 1 - suffix] === after[after.length - 1 - suffix]) suffix++
  const old = lines(before.slice(prefix, before.length - suffix))
  const fresh = lines(after.slice(prefix, after.length - suffix))
  if (old.length === 0 || fresh.length === 0 || old.length > MAX_LINES || fresh.length > MAX_LINES) {
    const change = trimmed(before, prefix, before.length - suffix, after.slice(prefix, after.length - suffix))
    return change.from === change.to && change.insert === "" ? [] : [change]
  }
  // The longest common subsequence of lines, by a table of suffix lengths.
  const n = old.length
  const m = fresh.length
  const table: Uint32Array[] = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) table[i][j] = old[i] === fresh[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1])
  }
  const out: TextChange[] = []
  let i = 0
  let j = 0
  let at = prefix
  while (i < n || j < m) {
    if (i < n && j < m && old[i] === fresh[j]) { at += old[i].length; i++; j++; continue }
    // A run of lines that differ: one change.
    const from = at
    let insert = ""
    while ((i < n || j < m) && !(i < n && j < m && old[i] === fresh[j])) {
      if (j < m && (i >= n || table[i][j + 1] >= table[i + 1][j])) { insert += fresh[j]; j++ }
      else { at += old[i].length; i++ }
    }
    const change = trimmed(before, from, at, insert)
    if (change.from !== change.to || change.insert !== "") out.push(change)
  }
  return out
}

/** applyChanges makes the text the changes describe. */
export function applyChanges(text: string, changes: TextChange[]): string {
  let out = ""
  let at = 0
  for (const change of changes) {
    out += text.slice(at, change.from) + change.insert
    at = change.to
  }
  return out + text.slice(at)
}
