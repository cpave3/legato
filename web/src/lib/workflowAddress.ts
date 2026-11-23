import type { ItemRef } from "./workflowDocument"

// A selection in the workflow editor's outline is an address in the page's
// hash (LEG-505), so a reload and Back keep it: about, inputs/<name>,
// on/<kind>, or steps/<StepPath> (steps/each/review), the engine's path of
// a step. The About row is every top-level field at once.

export type Selection = { kind: "about" } | Exclude<ItemRef, { kind: "about" }>

const encode = encodeURIComponent

export function addressOf(selection: Selection | ItemRef): string {
  switch (selection.kind) {
    case "about": return "about"
    case "input": return `inputs/${encode(selection.name)}`
    case "trigger": return `on/${encode(selection.name)}`
    case "step": return `steps/${selection.path.split("/").map(encode).join("/")}`
  }
}

/** selectionOf reads an address, with or without its #; null for none. */
export function selectionOf(hash: string): Selection | null {
  const address = hash.startsWith("#") ? hash.slice(1) : hash
  if (address === "about") return { kind: "about" }
  const slash = address.indexOf("/")
  if (slash < 0) return null
  const head = address.slice(0, slash)
  const rest = address.slice(slash + 1)
  if (rest === "" || rest.split("/").some(part => part === "")) return null
  let parts: string[]
  try { parts = rest.split("/").map(decodeURIComponent) } catch { return null }
  if (head === "steps") return { kind: "step", path: parts.join("/") }
  if (parts.length !== 1) return null
  if (head === "inputs") return { kind: "input", name: parts[0] }
  if (head === "on") return { kind: "trigger", name: parts[0] }
  return null
}

/** sameSelection says whether two selections name one row. */
export function sameSelection(a: Selection | null, b: Selection | ItemRef | null): boolean {
  if (!a || !b) return a === b
  return addressOf(a) === addressOf(b)
}
