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

export interface MacroList {
  macros: Macro[]
  suggested: MacroSuggestions
}

export async function fetchMacroList(baseUrl: string): Promise<MacroList> {
  const response = await apiFetch(baseUrl, "/api/macros")
  if (!response.ok) throw await failed(response)
  const body = await response.json() as { macros?: Macro[]; suggested?: MacroSuggestions }
  return { macros: Array.isArray(body.macros) ? body.macros : [], suggested: body.suggested ?? defaultSuggestions }
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
  const [list, setList] = useState<MacroList>({ macros: [], suggested: defaultSuggestions })
  useEffect(() => {
    if (refresh === null) return
    let current = true
    fetchMacroList(baseUrl).then(next => { if (current) setList(next) }, () => { if (current) setList({ macros: [], suggested: defaultSuggestions }) })
    return () => { current = false }
  }, [baseUrl, refresh])
  return list
}
