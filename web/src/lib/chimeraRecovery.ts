// Chimera currently persists recovery summaries as plain assistant text, without
// message metadata. Match only its exact generated envelope, not quoted prose or
// ordinary model summaries. See packages/compaction/src/compactor.ts in Chimera.
const recoveryPrefix = "## Goal\nRecovery excerpt (not a model summary):\n"
const truncationMarker = "\n[Older context truncated for recovery]\n"

export function isChimeraRecovery(text: string): boolean {
  return text.startsWith(recoveryPrefix)
}

export type RecoveryPart =
  | { kind: "record"; value: unknown }
  | { kind: "partial"; text: string }
  | { kind: "gap" }
  | { kind: "context"; text: string }

// Read one JSON value without letting brackets inside quoted text end it.
function objectEnd(text: string, start: number): number {
  let depth = 0
  let quoted = false
  let escaped = false
  for (let i = start; i < text.length; i++) {
    const c = text[i]
    if (quoted) {
      if (escaped) escaped = false
      else if (c === "\\") escaped = true
      else if (c === '"') quoted = false
    } else if (c === '"') quoted = true
    else if (c === "{" || c === "[") depth++
    else if (c === "}" || c === "]") {
      if (--depth === 0) return i + 1
    }
  }
  return -1
}

export function recoveryParts(text: string): RecoveryPart[] {
  const body = text.slice(recoveryPrefix.length)
  // The optional files/tasks/archived blocks follow the JSON on their own lines.
  const contextAt = body.search(/\n(?:\n)?<(?:files|tasks|archived)>\n/)
  const serialized = contextAt < 0 ? body : body.slice(0, contextAt)
  const parts: RecoveryPart[] = []
  try {
    const values: unknown = JSON.parse(serialized)
    for (const value of Array.isArray(values) ? values : [values]) parts.push({ kind: "record", value })
  } catch {
    // boundText can cut through a JSON string. Keep complete message records on
    // either side of the gap; never invent missing text or silently drop fragments.
    for (const [index, fragment] of serialized.split(truncationMarker).entries()) {
      if (index) parts.push({ kind: "gap" })
      const candidates = fragment.matchAll(/\{"role"\s*:\s*"(?:user|assistant|tool|system)"/g)
      let consumed = 0
      for (const match of candidates) {
        const start = match.index
        if (start < consumed) continue
        const end = objectEnd(fragment, start)
        if (end < 0) continue
        let value: unknown
        try { value = JSON.parse(fragment.slice(start, end)) } catch { continue }
        const before = fragment.slice(consumed, start).replace(/^[\s,[\]]+|[\s,[\]]+$/g, "")
        if (before) parts.push({ kind: "partial", text: before })
        parts.push({ kind: "record", value })
        consumed = end
      }
      const remaining = fragment.slice(consumed).replace(/^[\s,[\]]+|[\s,[\]]+$/g, "")
      if (remaining) parts.push({ kind: "partial", text: remaining })
    }
  }
  if (contextAt >= 0) parts.push({ kind: "context", text: body.slice(contextAt).trim() })
  return parts
}
