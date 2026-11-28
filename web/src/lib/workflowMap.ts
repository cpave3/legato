// The Workflows page's map (LEG-710): every workflow as a node and an edge
// for each workflow a workflow's workflow.run steps may run, as
// GET /api/workflows/graph gives them. What the map shows is decided here;
// components/workflow-map draws it.
import { apiFetch } from "./api"
import type { WorkflowTriggerState } from "./workflow"

// A node is a definition, a file that does not parse (broken, with its
// error), or a name a call names that nothing defines (missing).
export type WorkflowMapNode = {
  id: string
  name: string
  title?: string
  tags?: string[] | null
  source?: string
  built_in?: boolean
  broken?: boolean
  error?: string
  missing?: boolean
  triggers?: WorkflowTriggerState[] | null
}

// An edge is source calling target from calls steps (their paths in
// steps); in_loop when one is in a loop's body, dynamic when one names
// target only as one of an input's choices.
export type WorkflowMapEdge = { id: string; source: string; target: string; calls: number; in_loop?: boolean; dynamic?: boolean; steps: string[] }

export type WorkflowMap = { nodes: WorkflowMapNode[]; edges: WorkflowMapEdge[] }

export async function fetchWorkflowMap(baseUrl: string): Promise<WorkflowMap> {
  const response = await apiFetch(baseUrl, "/api/workflows/graph")
  if (!response.ok) throw new Error(await response.text())
  const map = await response.json() as Partial<WorkflowMap>
  return { nodes: map.nodes ?? [], edges: map.edges ?? [] }
}

// splitStandalone parts the workflows with a call in or out (drawn as a
// graph) from those with none (drawn apart, in their own group).
export function splitStandalone(map: WorkflowMap): { linked: WorkflowMapNode[]; standalone: WorkflowMapNode[] } {
  const ends = new Set(map.edges.flatMap(edge => [edge.source, edge.target]))
  return { linked: map.nodes.filter(node => ends.has(node.id)), standalone: map.nodes.filter(node => !ends.has(node.id)) }
}

// related is a workflow and every workflow that leads to it or that it
// leads to, through any number of calls: what stays lit while the rest
// dims.
export function related(map: WorkflowMap, id: string | null): Set<string> | null {
  if (!id) return null
  const lit = new Set([id])
  const walk = (from: string, next: (edge: WorkflowMapEdge) => [string, string]) => {
    const queue = [from]
    while (queue.length) {
      const at = queue.shift()!
      for (const edge of map.edges) {
        const [here, there] = next(edge)
        if (here === at && !lit.has(there)) { lit.add(there); queue.push(there) }
      }
    }
  }
  walk(id, edge => [edge.source, edge.target])
  walk(id, edge => [edge.target, edge.source])
  return lit
}

export function mapTags(map: WorkflowMap): string[] {
  return [...new Set(map.nodes.flatMap(node => node.tags ?? []))].sort()
}

// tagView is the map for one tag: its workflows, and the workflows they
// call or are called by, kept as context (drawn faint). An empty tag is
// the whole map.
export function tagView(map: WorkflowMap, tag: string): { map: WorkflowMap; context: Set<string> } {
  if (!tag) return { map, context: new Set() }
  const tagged = new Set(map.nodes.filter(node => node.tags?.includes(tag)).map(node => node.id))
  const edges = map.edges.filter(edge => tagged.has(edge.source) || tagged.has(edge.target))
  const kept = new Set([...tagged, ...edges.flatMap(edge => [edge.source, edge.target])])
  return {
    map: { nodes: map.nodes.filter(node => kept.has(node.id)), edges },
    context: new Set([...kept].filter(id => !tagged.has(id))),
  }
}

// edgeLabel says what is unusual about a call: several steps make it, one
// is in a loop, or it is one choice of an input.
export function edgeLabel(edge: WorkflowMapEdge): string {
  return [edge.calls > 1 ? `${edge.calls} calls` : "", edge.in_loop ? "in a loop" : "", edge.dynamic ? "one choice" : ""].filter(Boolean).join(" · ")
}

export type MarkerKind = "schedule" | "column" | "pr" | "chat" | "manual"
export type TriggerMarker = { kind: MarkerKind; label: string; enabled: boolean }

const markerOf: Record<string, { kind: MarkerKind; label: string }> = {
  schedule: { kind: "schedule", label: "On a schedule" },
  "task.entered_column": { kind: "column", label: "When a card moves" },
  "pr.changes_requested": { kind: "pr", label: "When a pull request needs changes" },
  "chat.message": { kind: "chat", label: "From chat" },
  "chat.mention": { kind: "chat", label: "From chat" },
  "chat.button": { kind: "chat", label: "From chat" },
}

// triggerMarkers are a node's small markers: each kind of trigger once, on
// when any of that kind is on here. A workflow with no trigger that no
// workflow calls is started by hand.
export function triggerMarkers(node: WorkflowMapNode, called: boolean): TriggerMarker[] {
  const out: TriggerMarker[] = []
  for (const state of node.triggers ?? []) {
    const marker = markerOf[state.trigger]
    if (!marker) continue
    const seen = out.find(item => item.kind === marker.kind)
    if (seen) seen.enabled = seen.enabled || state.enabled
    else out.push({ ...marker, enabled: state.enabled })
  }
  if (out.length === 0 && !called && !node.broken && !node.missing) out.push({ kind: "manual", label: "Run by hand", enabled: true })
  return out
}

// nodeStatus is whether a workflow's triggers are on here.
export function nodeStatus(node: WorkflowMapNode): "none" | "on" | "paused" | "mixed" {
  const states = node.triggers ?? []
  if (states.length === 0) return "none"
  const on = states.filter(state => state.enabled).length
  return on === states.length ? "on" : on === 0 ? "paused" : "mixed"
}
