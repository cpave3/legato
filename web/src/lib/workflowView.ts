// How the workflow screens say what a run is doing: its state in the board's
// five tones, where it is among its steps, and a sentence about it.

export type Tone = "live" | "needs" | "done" | "error" | "idle"
export type Segment = Tone | "ok" | "empty"

export interface RunLike { status: string; needs_attention?: boolean }
export interface StepLike { step_id: string; title: string; status: string; trigger?: string }

const finished = new Set(["succeeded", "skipped"])

export function runState(run: RunLike): { tone: Tone; label: string } {
  switch (run.status) {
    case "succeeded": return { tone: "done", label: "Done" }
    case "failed": return { tone: "error", label: "Failed" }
    case "cancelled": return { tone: "idle", label: "Cancelled" }
  }
  if (run.needs_attention) return { tone: "needs", label: "Needs you" }
  return { tone: "live", label: "Running" }
}

function currentIndex(steps: StepLike[]): number {
  return steps.findIndex(step => !finished.has(step.status))
}

// The step a run is on: the first that has not finished.
export function currentStep<T extends StepLike>(steps: T[]): T | undefined {
  return steps[currentIndex(steps)]
}

// The glyph and tone of one step in a run's list of steps. A pending step
// has no tone of its own.
export function stepMark(step: StepLike, run: RunLike): { glyph: string; tone: Tone | null } {
  switch (step.status) {
    case "succeeded": return { glyph: "✓", tone: "done" }
    case "skipped": return { glyph: "–", tone: null }
    case "failed": return { glyph: "✕", tone: "error" }
    case "cancelled": return { glyph: "–", tone: "idle" }
    case "pending": return { glyph: "○", tone: null }
  }
  if (step.status === "waiting_manual" && step.trigger === "agent_ended" && !run.needs_attention) return { glyph: "●", tone: "live" }
  return run.needs_attention || step.status === "waiting_manual" ? { glyph: "◆", tone: "needs" } : { glyph: "●", tone: "live" }
}

// One segment per step: finished steps "ok", the step the run is on in the
// run's tone, the steps after it "empty". A done run is all "done".
export function stepSegments(run: RunLike, steps: StepLike[]): Segment[] {
  const { tone } = runState(run)
  if (tone === "done") return steps.map(() => "done")
  const current = currentIndex(steps)
  return steps.map((_, index) => current === -1 || index < current ? "ok" : index === current ? tone : "empty")
}

export function stepPosition(run: RunLike, steps: StepLike[]): { number: number; total: number; title: string } | null {
  if (run.status === "succeeded") return null
  const current = currentIndex(steps)
  if (current === -1) return null
  return { number: current + 1, total: steps.length, title: steps[current].title }
}

export interface RunSummaryInput extends RunLike {
  attention_reason?: string
  error?: string
  started_at?: string
  completed_at?: string
  steps?: StepLike[] | null
}

// What the service's attention reasons say to the operator: a heading for the
// run page's "Now" panel and a sentence for the runs list. The step is the one
// the run is on.
const agentReasons: Partial<Record<string, string>> = {
  "agent idle before step completion": "The agent went idle before finishing",
  "agent waiting for input": "The agent is waiting for an answer",
  "agent error before step completion": "The agent hit an error",
  "agent session ended before step completion": "The agent ended before finishing",
}
const manualPrefix = "manual action required: "

export function attentionCopy(reason: string, step: string): { heading: string; sentence: string } {
  if (reason.startsWith(manualPrefix)) {
    const title = reason.slice(manualPrefix.length) || step
    return { heading: `${title} waits for you`, sentence: `“${title}” waits for you to run it` }
  }
  const heading = agentReasons[reason]
  if (!heading) return { heading: reason || "Needs you", sentence: reason || "Needs you" }
  const sentence = reason === "agent waiting for input" ? `${heading} in “${step}”`
    : reason === "agent error before step completion" ? `The agent hit an error before finishing “${step}”`
    : `${heading} “${step}”`
  return { heading, sentence }
}

// The sentence under a run's title: what it is doing, what waits for the
// operator, why it failed, or how long it took. tone colours the sentence
// when it is about something the operator should act on.
export function runSummary(run: RunSummaryInput): { text: string; tone?: Tone } {
  const steps = run.steps ?? []
  const current = steps[currentIndex(steps)]
  switch (run.status) {
    case "succeeded": {
      const took = run.started_at && run.completed_at ? elapsed(run.started_at, run.completed_at) : ""
      return { text: took ? `Took ${took}` : "Done" }
    }
    case "failed": {
      const failed = steps.find(step => step.status === "failed")
      const error = run.error || "Failed"
      return { text: failed ? `${failed.title}: ${error}` : error, tone: "error" }
    }
    case "cancelled":
      return { text: run.error || "Cancelled" }
  }
  if (run.needs_attention) return { text: attentionCopy(run.attention_reason ?? "", current?.title ?? "").sentence, tone: "needs" }
  if (!current) return { text: "" }
  if (current.status === "waiting_agent") return { text: `An agent is working on “${current.title}”` }
  if (current.status === "waiting_manual" && current.trigger === "agent_ended") return { text: `“${current.title}” starts when the agent ends` }
  return { text: `Running “${current.title}”` }
}

function parseObject(raw: string | undefined): Record<string, unknown> | null {
  try {
    const value: unknown = JSON.parse(raw ?? "")
    return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null
  } catch {
    return null
  }
}

function taskName(value: unknown): string {
  if (!value || typeof value !== "object") return typeof value === "string" ? value : ""
  const task = value as { id?: unknown; title?: unknown }
  const id = typeof task.id === "string" ? task.id : ""
  return typeof task.title === "string" && task.title ? `${id} “${task.title}”` : id
}

// What a step will do, said before the operator runs it by hand. Null for a
// step whose executor it does not know how to describe.
export function stepIntent(step: { executor_type: string; resolved_inputs_json: string }): string | null {
  const inputs = parseObject(step.resolved_inputs_json)
  if (!inputs) return null
  const task = taskName(inputs.task)
  switch (step.executor_type) {
    case "task.move": return task && typeof inputs.column === "string" ? `Moves ${task} to ${inputs.column}.` : null
    case "task.note": return task ? `Adds a note to ${task}.` : null
    case "agent.stop": return task ? `Stops the agent of ${task}.` : null
    case "chimera.session.remove": return task ? `Removes the Chimera session of ${task}.` : null
    case "worktree.remove": return typeof inputs.expected_path === "string" && inputs.expected_path ? `Removes the worktree at ${inputs.expected_path}.` : null
  }
  return null
}

export function triggerText(on: Record<string, unknown> | null | undefined): string | null {
  const entered = on?.["task.entered_column"] as { column?: unknown; confirm?: unknown } | undefined
  if (!entered || typeof entered.column !== "string") return null
  return `${entered.confirm ? "Asks to start" : "Starts"} when a task enters ${entered.column}`
}

export function sourceLabel(source: string): string {
  if (source.startsWith("builtin:")) return "built in"
  return source.split(/[\\/]/).pop() || source
}

// What a workflow asks for when it is run: "a task", or the names of its
// inputs, required ones first, four at most.
export function inputsLabel(inputs: Record<string, { type: string; required?: boolean }> | null | undefined): string {
  const entries = Object.entries(inputs ?? {})
  if (entries.length === 1 && entries[0][1].type === "task") return "a task"
  const ordered = [...entries.filter(([, input]) => input.required), ...entries.filter(([, input]) => !input.required)]
  const names = ordered.map(([name, input]) => input.type === "task" ? "a task" : name.replaceAll("_", " "))
  return names.length > 4 ? `${names.slice(0, 4).join(", ")}…` : names.join(", ")
}

// Legato's timestamps come as RFC 3339 or as SQLite's "YYYY-MM-DD HH:MM:SS",
// both in UTC.
export function parseTime(value: string): number {
  return Date.parse(/([zZ]|[+-]\d\d:?\d\d)$/.test(value) ? value : `${value.replace(" ", "T")}Z`)
}

export function formatSeconds(total: number): string {
  const seconds = Math.max(0, Math.round(total))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return seconds % 60 ? `${minutes}m ${seconds % 60}s` : `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return minutes % 60 ? `${hours}h ${minutes % 60}m` : `${hours}h`
}

export function elapsed(start: string, end: string | number): string {
  const until = typeof end === "number" ? end : parseTime(end)
  return formatSeconds((until - parseTime(start)) / 1000)
}

export function ago(time: string, now: number = Date.now()): string {
  const seconds = (now - parseTime(time)) / 1000
  if (seconds < 60) return "now"
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}

// The line beside a run's title: the task it is for, else the prompt it was
// given.
export function runSubject(run: { related_task_title?: string; inputs_json: string; definition_json?: string }): string {
  if (run.related_task_title) return run.related_task_title
  if (secretInputNames(run.definition_json).has("prompt")) return ""
  const prompt = parseObject(run.inputs_json)?.prompt
  return typeof prompt === "string" ? prompt.split("\n")[0] : ""
}

// The values agents reported for a run, for "Produced" on a finished run. A
// file output is shown as its artifact, and the outputs of Legato's own steps
// (a note's ID, a task's context) stay with their steps.
export function producedValues(steps: { title: string; executor_type: string; outputs_json: string }[]): { step: string; key: string; value: string }[] {
  return steps.filter(step => step.executor_type === "agent.run").flatMap(step => {
    const outputs = parseObject(step.outputs_json) ?? {}
    return Object.entries(outputs).flatMap(([key, value]) => {
      if (value && typeof value === "object" && !Array.isArray(value) && "artifact_id" in value) return []
      return [{ step: step.title, key, value: typeof value === "string" ? value : JSON.stringify(value) }]
    })
  })
}

// The inputs a run's definition marks secret, whose values are never shown.
export function secretInputNames(definitionJSON: string | undefined): Set<string> {
  const inputs = parseObject(definitionJSON)?.inputs
  if (!inputs || typeof inputs !== "object") return new Set()
  return new Set(Object.entries(inputs as Record<string, { secret?: unknown }>).filter(([, input]) => input?.secret === true).map(([name]) => name))
}

// A run as its page should judge it: the run endpoint marks a waiting manual
// step on the run, but an agent that needs the operator only on its attempt.
export function runWithAttention<T extends RunLike>(run: T, attempts: { needs_attention?: boolean }[]): T & { needs_attention: boolean } {
  return { ...run, needs_attention: Boolean(run.needs_attention || attempts.some(attempt => attempt.needs_attention)) }
}

// One input's value as a line of text: a task as its ID and title, a
// repository as its name, anything else as it is.
export function inputText(value: unknown): string {
  if (typeof value === "string") return value
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const record = value as Record<string, unknown>
    if (typeof record.id === "string" && typeof record.title === "string") return `${record.id} ${record.title}`.trim()
    if (typeof record.name === "string" && typeof record.repo === "string") return `${record.name} (${record.repo})`
  }
  return JSON.stringify(value)
}
