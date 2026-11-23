import { useEffect, useState } from "react"
import { apiFetch } from "./api"

// A macro: named text for an agent. Started on a task or a pull request it
// also starts the task's agent when none is running.
export interface Macro {
  name: string
  keys: string
  applies_to?: "task" | "pr"
  agent?: string
  tier?: string
  source?: "builtin" | "override" | "config.yaml" | "database"
  builtin?: boolean
  overridden?: boolean
  pinned?: boolean
  // Starred: pickers list the favourites and keep the rest behind Show all.
  favorite?: boolean
}

export interface MacroPR {
  repo: string
  pr_number: number
  url?: string
}

export interface MacroStartResult {
  task_id: string
  started: boolean
  queued: boolean
  agent_kind?: string
}

async function failed(response: Response): Promise<Error> {
  let message = response.statusText
  try {
    const body = await response.json()
    if (body && typeof body.error === "string") message = body.error
  } catch {
    // Not JSON: the status says enough.
  }
  return new Error(message || `request failed (${response.status})`)
}

// The macro suggested for each target: offered first, started by a plain press.
export interface MacroSuggestions {
  task: string
  my_pr: string
  others_pr: string
}

// What an older server, which suggests nothing, is taken to suggest.
export const defaultSuggestions: MacroSuggestions = { task: "Plan", my_pr: "Address feedback", others_pr: "Review" }

// One of an agent tool's tiers: a named launch profile from config.yaml,
// usually choosing the model.
export interface AgentTier {
  name: string
  description?: string
}

export interface MacroList {
  macros: Macro[]
  suggested: MacroSuggestions
  // Each agent tool's tiers; a tool with none is absent.
  tiers: Record<string, AgentTier[]>
}

export async function fetchMacroList(baseUrl: string): Promise<MacroList> {
  const response = await apiFetch(baseUrl, "/api/macros")
  if (!response.ok) throw await failed(response)
  const body = await response.json() as { macros?: Macro[]; suggested?: MacroSuggestions; tiers?: Record<string, AgentTier[]> }
  return { macros: Array.isArray(body.macros) ? body.macros : [], suggested: body.suggested ?? defaultSuggestions, tiers: body.tiers ?? {} }
}

export async function fetchMacros(baseUrl: string): Promise<Macro[]> {
  return (await fetchMacroList(baseUrl)).macros
}

// prIsMine says whether a card's pull request is the operator's own: one
// Legato found from the task's branch is; one linked by its number (a
// review of someone else's work) is not.
export function prIsMine(pr: { pinned?: boolean }): boolean {
  return !pr.pinned
}

// suggestedFirst is the macros for a target with the suggested one first;
// the suggested is undefined when no macro of its name applies.
export function suggestedFirst(macros: Macro[], target: "task" | "my_pr" | "others_pr", suggested: MacroSuggestions): { list: Macro[]; suggested?: Macro } {
  const list = macrosFor(macros, target === "task" ? "task" : "pr")
  const name = suggested[target].toLowerCase()
  const index = list.findIndex(macro => macro.name.toLowerCase() === name)
  if (index < 0) return { list }
  const first = list[index]
  return { list: [first, ...list.slice(0, index), ...list.slice(index + 1)], suggested: first }
}

// macrosFor keeps the macros that apply to a task or a pull request: those
// that say so, and those that say nothing.
export function macrosFor(macros: Macro[], target: "task" | "pr"): Macro[] {
  return macros.filter(macro => !macro.applies_to || macro.applies_to === target)
}

// anyFavorite says whether any macro is starred.
export function anyFavorite(macros: Pick<Macro, "name" | "favorite">[]): boolean {
  return macros.some(macro => macro.favorite)
}

// splitFavorites is a picker's macros: the favourites, and the rest it
// keeps behind Show all, each in the list's order. While no macro is
// starred every macro is a favourite. starred says whether any is, when
// list is part of a longer list (the macros for a task, say).
export function splitFavorites<T extends Pick<Macro, "name" | "favorite">>(list: T[], starred = anyFavorite(list)): { favorites: T[]; rest: T[] } {
  if (!starred) return { favorites: list, rest: [] }
  return { favorites: list.filter(macro => macro.favorite), rest: list.filter(macro => !macro.favorite) }
}

// startMacro starts a macro on a task (and one of its pull requests): the
// task's agent starts with it, or the running one gets it as its next message.
export async function startMacro(baseUrl: string, taskId: string, name: string, pr?: MacroPR): Promise<MacroStartResult> {
  const response = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/macro`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(pr ? { name, pr } : { name }),
  })
  if (!response.ok) throw await failed(response)
  return await response.json() as MacroStartResult
}

// macroNotice says what a start did, for a status line.
export function macroNotice(name: string, result: MacroStartResult): string {
  return result.started ? `Started the agent with ${name}.` : `${name} will be the agent's next message.`
}

// useMacros reads the macros again each time refresh changes (a page
// loading, a menu opening): they change without a restart. null reads
// nothing; a failure leaves the list empty.
export function useMacros(baseUrl: string, refresh: string | null): MacroList {
  const [list, setList] = useState<MacroList>({ macros: [], suggested: defaultSuggestions, tiers: {} })
  useEffect(() => {
    if (refresh === null) return
    let current = true
    fetchMacroList(baseUrl).then(next => { if (current) setList(next) }, () => { if (current) setList({ macros: [], suggested: defaultSuggestions, tiers: {} }) })
    return () => { current = false }
  }, [baseUrl, refresh])
  return list
}

// The fields a macro is saved with: its message, and what it is offered on,
// the agent it starts and that agent's tier, each empty for the default.
export interface MacroFields {
  message: string
  applies_to?: "" | "task" | "pr"
  agent?: string
  tier?: string
}

async function macroWrite(baseUrl: string, path: string, method: string, body?: unknown): Promise<void> {
  const response = await apiFetch(baseUrl, path, {
    method,
    ...(body === undefined ? {} : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }),
  })
  if (!response.ok) throw await failed(response)
}

const macroPath = (name: string) => `/api/macros/${encodeURIComponent(name)}`

// saveMacro saves a new macro, an edit, or an edit of a built-in.
export function saveMacro(baseUrl: string, name: string, fields: MacroFields): Promise<void> {
  const body: Record<string, string> = { message: fields.message }
  for (const key of ["applies_to", "agent", "tier"] as const) if (fields[key]) body[key] = fields[key] as string
  return macroWrite(baseUrl, macroPath(name), "PUT", body)
}

// setMacroFavorite stars a macro, or takes its star away.
export function setMacroFavorite(baseUrl: string, name: string, favorite: boolean): Promise<void> {
  return macroWrite(baseUrl, macroPath(name), "PUT", { favorite })
}

// deleteMacro deletes one of the operator's own macros.
export function deleteMacro(baseUrl: string, name: string): Promise<void> {
  return macroWrite(baseUrl, macroPath(name), "DELETE")
}

// resetMacro puts an edited built-in back as shipped.
export function resetMacro(baseUrl: string, name: string): Promise<void> {
  return macroWrite(baseUrl, `${macroPath(name)}/reset`, "POST")
}

// renameMacro gives one of the operator's own macros a new name.
export function renameMacro(baseUrl: string, name: string, to: string): Promise<void> {
  return macroWrite(baseUrl, `${macroPath(name)}/rename`, "POST", { name: to })
}

// appliesLabel says what a macro is offered on.
export function appliesLabel(macro: Pick<Macro, "applies_to">): string {
  return macro.applies_to === "task" ? "Tasks" : macro.applies_to === "pr" ? "Pull requests" : "Tasks and pull requests"
}

// macroAgentLine names the agent a macro starts and its tier, when it sets
// either; "" when it leaves both to the defaults.
export function macroAgentLine(macro: Pick<Macro, "agent" | "tier">, label: (kind: string) => string): string {
  const tier = macro.tier ? `${macro.tier} tier` : ""
  if (!macro.agent) return tier
  return tier ? `${label(macro.agent)} · ${tier}` : label(macro.agent)
}

// macroMessage is a macro's text as it is sent: a message, so a final new
// line (which once pressed Enter in a terminal) is dropped.
export function macroMessage(keys: string): string {
  return keys.replace(/\n+$/, "")
}
