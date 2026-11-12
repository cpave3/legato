// The approvals the voice widget shows: every paired server's pending
// approvals, as Rust lists them (voice_approvals, the voice-approvals event;
// src-tauri/src/widgetapprovals.rs). What a line says, what its detail
// shows and how it is decided, as the web UI's tray has them
// (web/src/components/ApprovalTray.tsx, ChangeSetCard.tsx).

export type ApprovalOption = { id: string; label: string; kind: "allow" | "deny" | string; description?: string }

// One call of a Reeve change set, as its approval carries it.
export type ChangeSetCall = {
  kind: string
  method?: string
  path?: string
  class?: string
  description?: string
  level?: string
  as_root?: boolean
  args?: string[]
  command?: string
  file?: string
  unit?: string
  action?: string
  system?: boolean
}

export type Approval = {
  id: string
  source: string
  title: string
  summary?: string
  signed?: boolean
  options: ApprovalOption[]
  task_id?: string
  details: {
    agent_kind?: string
    task_title?: string
    command?: string
    reason?: string
    cwd?: string
    plan?: string
    questions?: unknown[]
    credential?: string
    operations?: string[]
    resources?: string[]
    calls?: ChangeSetCall[]
    [key: string]: unknown
  }
}

// An approval and the server it waits on.
export type WidgetApproval = { server: string; server_name: string; tint?: string | null; approval: Approval }

const REEVE = new Set(["reeve_change_set", "reeve_rules", "reeve_secret", "reeve_seed"])

// approvalLine is the approval's one line: who asks, and what.
export function approvalLine(a: Approval): { who: string; what: string } {
  let who: string
  if (REEVE.has(a.source)) who = "Reeve"
  else if (a.source === "devices") who = "Legato"
  else who = a.details.agent_kind || a.details.task_title || "An agent"
  return { who, what: a.title }
}

export type HowToDecide =
  | { kind: "choose"; allows: ApprovalOption[] }
  | { kind: "sign"; allows: ApprovalOption[] }
  // What needs typing (an answer, a secret) or reading at length (a plan)
  // is decided in the main window, since the widget never takes the keyboard.
  | { kind: "open"; label: string }

export function howToDecide(a: Approval): HowToDecide {
  const allows = a.options.filter((o) => o.kind === "allow")
  if (a.source === "agent_question" || (a.details.questions?.length ?? 0) > 0) return { kind: "open", label: "Answer in Legato" }
  if (a.source === "reeve_secret") return { kind: "open", label: "Give it in Legato" }
  if (a.details.plan) return { kind: "open", label: "Read the plan in Legato" }
  if (allows.length === 0) return { kind: "open", label: "Open in Legato" }
  return a.signed ? { kind: "sign", allows } : { kind: "choose", allows }
}

export type DetailLine = { label: string; value: string; code?: boolean; note?: string; warn?: boolean }

function quoteArg(arg: string): string {
  return arg === "" || /[\s"'\\$`]/.test(arg) ? JSON.stringify(arg) : arg
}

// callTarget is what a change set's call runs, in full, as ChangeSetCard says it.
function callTarget(call: ChangeSetCall): string {
  switch (call.kind) {
    case "restart": return "restart Legato"
    case "config_edit": return "edit config.yaml"
    case "legato": return ["legato", ...(call.args ?? []).map(quoteArg)].join(" ")
    case "host_shell": return `$ ${call.command ?? ""}`
    case "edit_file": return `write ${call.file ?? ""}`
    case "manage_service": return `${call.action ?? ""} ${call.unit ?? ""} (${call.system ? "system" : "user"} service)`
    default: return `${call.method ?? "GET"} ${call.path ?? ""}`
  }
}

function callKind(call: ChangeSetCall): string {
  if (call.as_root) return "host as root"
  return call.class === "risky" ? "risky" : "change"
}

// detailOf is what the line opens to: what it would run, where, and why.
export function detailOf(a: Approval): DetailLine[] {
  const d = a.details
  const out: DetailLine[] = []
  if (a.source === "reeve_change_set") {
    for (const call of d.calls ?? []) {
      out.push({ label: callKind(call), value: callTarget(call), code: true, note: call.description || undefined, warn: call.as_root || call.class === "risky" })
    }
    return out
  }
  if (a.source === "credential") {
    for (const op of d.operations ?? []) out.push({ label: "operation", value: op, code: true })
    const resources = d.resources ?? []
    for (const r of resources) out.push({ label: "resource", value: r, code: true })
    if (!resources.length && d.credential) out.push({ label: "resource", value: `all of ${d.credential}` })
  } else {
    const run = d.command || a.summary
    if (run && !d.plan) out.push({ label: "command", value: run, code: true })
    if (d.cwd) out.push({ label: "in", value: d.cwd, code: true })
  }
  if (d.reason) out.push({ label: "reason", value: d.reason })
  return out
}
