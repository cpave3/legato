import type { PRMetaView } from "./board-types"
import { prStatus, type PRStatus } from "./prStatus"

// What a board card shows of a stack of pull requests: a bar with a
// segment per layer, the layer that needs the operator with what it holds
// back, the run that can merge now, and the layers grouped by repository
// for the open list. See docs/ai/web-ui.md § Per-card indicators.

// How a layer is drawn in the bar and on the rail.
export type SegmentKind = "merged" | "ready" | "held" | "failing" | "running" | "draft" | "waiting" | "closed"

export interface StackLayer {
  pr: PRMetaView
  status: PRStatus
  kind: SegmentKind
  // A layer below it is not ready, so it cannot merge yet.
  heldBelow: boolean
  // It shares its base with another layer (a fork): drawn indented.
  fork: boolean
}

export interface StackGroup {
  repo: string
  base: string
  layers: StackLayer[]
}

export interface StackSummary {
  layers: StackLayer[]
  groups: StackGroup[]
  // "Stack of 13 PRs", "5 PRs in 2 repos"
  title: string
  // "2 merged", "1 merged · 1 closed", "3 drafts"
  counts: string
  // The bar's counts in words.
  barLabel: string
  merged: number
  closed: number
  allMerged: boolean
  // The lowest layer that needs the operator, and how many open layers
  // above it it holds back.
  needsYou?: { layer: StackLayer; holdsBack: number }
  // The open layers from the bottom that can merge now, up to the first
  // that cannot.
  mergeable: PRMetaView[]
  // Set when every open layer is a draft: "Drafts. CI passing on all 3."
  draftsLine?: string
}

function kindOf(status: PRStatus): SegmentKind {
  switch (status.state) {
    case "merged": return "merged"
    case "closed": return "closed"
    case "draft": return "draft"
    case "failing":
    case "changes": return "failing"
    case "running": return "running"
    case "ready": return "ready"
    case "held": return "held"
    default: return "waiting"
  }
}

const key = (pr: PRMetaView) => `${pr.repo}#${pr.pr_number}`

// The layer each PR is built on: the PR whose head is its base, or, for a
// PR with no base yet, the one before it in its repository.
function parents(prs: PRMetaView[]): Map<string, PRMetaView | undefined> {
  const out = new Map<string, PRMetaView | undefined>()
  const byHead = new Map<string, PRMetaView>()
  for (const pr of prs) byHead.set(`${pr.repo}\u0000${pr.branch}`, pr)
  let previous: PRMetaView | undefined
  for (const pr of prs) {
    if (previous && previous.repo !== pr.repo) previous = undefined
    const parent = pr.base !== undefined && pr.base !== "" ? byHead.get(`${pr.repo}\u0000${pr.base}`) : previous
    out.set(key(pr), parent && parent !== pr ? parent : undefined)
    previous = pr
  }
  return out
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

export function stackSummary(prs: PRMetaView[]): StackSummary {
  const parentOf = parents(prs)
  const open = (pr: PRMetaView) => !["MERGED", "CLOSED"].includes(pr.state.toUpperCase())
  // Whether a layer can merge as far as it alone goes, and whether one
  // below it holds it.
  const held = new Map<string, boolean>()
  const isHeld = (pr: PRMetaView, seen = new Set<string>()): boolean => {
    const k = key(pr)
    if (held.has(k)) return held.get(k)!
    if (seen.has(k)) return false
    seen.add(k)
    const parent = parentOf.get(k)
    let result = false
    if (parent && parent.state.toUpperCase() !== "MERGED") {
      result = isHeld(parent, seen) || prStatus(parent).state !== "ready"
    }
    held.set(k, result)
    return result
  }
  const children = new Map<string, number>()
  for (const pr of prs) {
    const parent = parentOf.get(key(pr))
    const base = parent ? key(parent) : `${pr.repo}\u0000${pr.base ?? ""}`
    children.set(base, (children.get(base) ?? 0) + 1)
  }
  const layers: StackLayer[] = prs.map(pr => {
    const heldBelow = open(pr) && isHeld(pr)
    const status = prStatus(pr, heldBelow)
    const parent = parentOf.get(key(pr))
    const base = parent ? key(parent) : `${pr.repo}\u0000${pr.base ?? ""}`
    return { pr, status, kind: kindOf(status), heldBelow, fork: Boolean(parent) && (children.get(base) ?? 0) > 1 }
  })

  const groups: StackGroup[] = []
  for (const layer of layers) {
    let group = groups.find(g => g.repo === layer.pr.repo)
    if (!group) {
      group = { repo: layer.pr.repo, base: "", layers: [] }
      groups.push(group)
    }
    if (!group.base && !parentOf.get(key(layer.pr)) && layer.pr.base) group.base = layer.pr.base
    group.layers.push(layer)
  }

  const merged = layers.filter(l => l.kind === "merged").length
  const closed = layers.filter(l => l.kind === "closed").length
  const openLayers = layers.filter(l => open(l.pr))
  const drafts = openLayers.length > 0 && openLayers.every(l => l.kind === "draft")
  const counts = drafts && merged === 0 && closed === 0
    ? plural(openLayers.length, "draft")
    : [`${merged} merged`, ...(closed > 0 ? [`${closed} closed`] : [])].join(" · ")

  const title = groups.length > 1 ? `${prs.length} PRs in ${groups.length} repos` : `Stack of ${prs.length} PRs`

  const order: [SegmentKind, (n: number) => string][] = [
    ["merged", n => `${n} merged`],
    ["ready", n => `${n} ready to merge`],
    ["failing", n => `${n} failing`],
    ["running", n => `${n} with CI running`],
    ["waiting", n => `${n} awaiting review`],
    ["held", n => `${n} waiting on a layer below`],
    ["draft", n => plural(n, "draft")],
    ["closed", n => `${n} closed`],
  ]
  const barLabel = order.map(([kind, word]) => {
    const n = layers.filter(l => l.kind === kind).length
    return n > 0 ? word(n) : ""
  }).filter(Boolean).join(", ")

  let needsYou: StackSummary["needsYou"]
  const first = layers.find(l => l.status.needsYou)
  if (first) {
    const above = (pr: PRMetaView): PRMetaView[] => prs.filter(other => parentOf.get(key(other)) === pr).flatMap(child => [child, ...above(child)])
    needsYou = { layer: first, holdsBack: above(first.pr).filter(open).length }
  }

  const mergeable: PRMetaView[] = []
  for (const layer of layers) {
    if (!open(layer.pr)) continue
    if (mergeable.length > 0 && layer.pr.repo !== mergeable[0].repo) break
    if (layer.kind !== "ready") break
    mergeable.push(layer.pr)
  }

  let draftsLine: string | undefined
  if (drafts) {
    const failing = openLayers.filter(l => l.pr.check_status === "fail").length
    const passing = openLayers.filter(l => l.pr.check_status === "pass").length
    draftsLine = failing > 0 ? `Drafts. CI failing on ${failing}.` : passing === openLayers.length ? `Drafts. CI passing on all ${passing}.` : "Drafts."
  }

  return { layers, groups, title, counts, barLabel, merged, closed, allMerged: merged === prs.length, needsYou, mergeable, draftsLine }
}

// "#196–#199", or "#196" for one.
export function prRange(prs: PRMetaView[]): string {
  if (prs.length === 0) return ""
  if (prs.length === 1) return `#${prs[0].pr_number}`
  return `#${prs[0].pr_number}–#${prs[prs.length - 1].pr_number}`
}
