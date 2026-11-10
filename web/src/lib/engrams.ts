import { apiFetch } from "./api"

// An engram as the spawn dialog lists it. mark is "last" for the task's last
// engram, "default" for the place's default, empty otherwise; bound says it
// is bound to the place.
export interface EngramChoice {
  name: string
  summary: string
  mark?: "last" | "default" | ""
  bound?: boolean
}

// What GET /api/engrams answers: whether engrams are offered, the place the
// agent will start in, the engrams in the order to list them, and the one to
// pick first ("" for none).
export interface EngramPicker {
  enabled: boolean
  place?: { key: string; label: string; kind: "repo" | "folder"; default?: string }
  engrams: EngramChoice[]
  preselect?: string
}

// Where the agent will start, as the picker is asked for it: a folder, or a
// repository a clone will be made of.
export interface EngramPlace {
  dir?: string
  repo?: string
  taskId?: string
}

// The agent tools that can be started as an engram: those that take a
// launch prompt.
export function takesEngram(agentKind: string): boolean {
  return agentKind === "claude-code" || agentKind === "codex" || agentKind === "chimera"
}

export async function fetchEngramPicker(baseUrl: string, place: EngramPlace, signal?: AbortSignal): Promise<EngramPicker> {
  const query = new URLSearchParams()
  if (place.repo) query.set("repo", place.repo)
  else if (place.dir) query.set("dir", place.dir)
  if (place.taskId) query.set("task_id", place.taskId)
  const suffix = query.toString() ? `?${query}` : ""
  const response = await apiFetch(baseUrl, `/api/engrams${suffix}`, { signal })
  if (!response.ok) return { enabled: false, engrams: [] }
  const data = await response.json()
  return { enabled: data.enabled === true, place: data.place, engrams: data.engrams ?? [], preselect: data.preselect ?? "" }
}

// The words beside an engram in the list.
export function engramMarkLabel(mark: EngramChoice["mark"]): string {
  if (mark === "last") return "last on this task"
  if (mark === "default") return "default"
  return ""
}

// The fields of a spawn request that start the agent as an engram.
export function engramFields(request: { engram?: string; rememberEngram?: boolean }): Record<string, string | boolean> {
  return {
    ...(request.engram ? { engram: request.engram } : {}),
    ...(request.rememberEngram ? { remember_engram: true } : {}),
  }
}

// readSpawnWarning returns the engram warning a spawn's answer carries, if
// any: the engram did not boot and the agent started without it.
export async function readSpawnWarning(response: Response): Promise<string> {
  try {
    const body = await response.clone().json()
    return typeof body?.engram_warning === "string" ? body.engram_warning : ""
  } catch {
    return ""
  }
}
