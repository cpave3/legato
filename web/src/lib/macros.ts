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

export async function fetchMacros(baseUrl: string): Promise<Macro[]> {
  const response = await apiFetch(baseUrl, "/api/macros")
  if (!response.ok) throw await failed(response)
  const body = await response.json() as { macros?: Macro[] }
  return Array.isArray(body.macros) ? body.macros : []
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

// useMacros reads the macros each time open becomes true (a menu opening):
// they change without a restart. A failure leaves the list empty.
export function useMacros(baseUrl: string, open: boolean): Macro[] {
  const [macros, setMacros] = useState<Macro[]>([])
  useEffect(() => {
    if (!open) return
    let current = true
    fetchMacros(baseUrl).then(list => { if (current) setMacros(list) }, () => { if (current) setMacros([]) })
    return () => { current = false }
  }, [baseUrl, open])
  return macros
}
