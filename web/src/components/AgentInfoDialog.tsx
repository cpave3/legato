import { useEffect, useId, useRef, useState, type ReactNode } from "react"
import { Check, Copy, X } from "lucide-react"

import { useServer } from "../hooks/useServer"
import type { AgentInfo } from "../lib/agentWire"
import { agentKindLabel } from "../lib/agentKind"
import { attachedResources } from "../lib/resources"
import { cn } from "../lib/utils"
import { Dialog, DialogBody, DialogClose, DialogHeader, DialogPanel, DialogTitle } from "./ui/Dialog"

type CopyState = "" | "done" | "failed"

// copyText puts text on the clipboard. A page not served over a safe
// connection has no Clipboard API; the browser's older copy command is
// tried on the selected text instead. Either way the text is left selected,
// so it can be copied by hand when neither works.
async function copyText(text: string, element: HTMLElement | null): Promise<boolean> {
  if (element) selectContents(element)
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // Refused: fall through to the copy command.
  }
  try {
    return typeof document.execCommand === "function" && document.execCommand("copy")
  } catch {
    return false
  }
}

function selectContents(element: HTMLElement) {
  const selection = window.getSelection()
  if (!selection) return
  const range = document.createRange()
  range.selectNodeContents(element)
  selection.removeAllRanges()
  selection.addRange(range)
}

// CopyField is a value shown in full, in monospace, wrapping where it must,
// with a button that copies it exactly.
function CopyField({ label, value, copyLabel }: { label: string; value: string; copyLabel?: string }) {
  const id = useId()
  const text = useRef<HTMLElement>(null)
  const [state, setState] = useState<CopyState>("")

  useEffect(() => {
    if (!state) return
    const timer = window.setTimeout(() => setState(""), state === "done" ? 1500 : 4000)
    return () => window.clearTimeout(timer)
  }, [state])

  const copy = async () => setState(await copyText(value, text.current) ? "done" : "failed")
  const name = state === "done" ? "Copied" : state === "failed" ? "Could not copy" : `Copy ${copyLabel ?? label.toLowerCase()}`

  return (
    <Row label={label} labelId={id}>
      <div className="flex items-start gap-1.5">
        <code
          ref={text}
          aria-labelledby={id}
          className="min-w-0 flex-1 select-all break-all rounded border border-lw-line bg-lw-surface px-2 py-1 font-lw-mono text-xs text-lw-text"
        >{value}</code>
        <button
          type="button"
          onClick={() => void copy()}
          aria-label={name}
          title={name}
          className={cn(
            "flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-lw-line-strong bg-lw-raised text-lw-text-2 transition-colors hover:text-lw-text",
            state === "done" && "text-lw-live",
            state === "failed" && "text-lw-error",
          )}
        >
          {state === "done" ? <Check size={14} aria-hidden="true" /> : state === "failed" ? <X size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
        </button>
      </div>
      {state === "failed" && (
        <p role="status" className="mt-1 text-[11px] text-lw-error">
          This page cannot use the clipboard. The text is selected: press Ctrl+C (⌘C) to copy it.
        </p>
      )}
    </Row>
  )
}

function Row({ label, labelId, children }: { label: string; labelId?: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[8.5rem_minmax(0,1fr)] sm:gap-3">
      <dt id={labelId} className="pt-1 text-xs text-lw-faint">{label}</dt>
      <dd className="m-0 min-w-0 text-[13px] text-lw-text">{children}</dd>
    </div>
  )
}

function PlainField({ label, value }: { label: string; value: ReactNode }) {
  const id = useId()
  return (
    <Row label={label} labelId={id}>
      <span aria-labelledby={id} className="block break-words pt-0.5">{value}</span>
    </Row>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-[1px] text-lw-faint">{title}</h3>
      <dl className="m-0 space-y-2">{children}</dl>
    </section>
  )
}

function sandboxLabel(agent: AgentInfo): string {
  if (agent.sandboxed) return "In the sandbox"
  if (agent.unsandboxed) return "Outside the sandbox (opted out)"
  return "No sandbox"
}

function activityLabel(agent: AgentInfo): string {
  if (agent.status !== "running") return agent.status
  return agent.activity ? `running, ${agent.activity}` : "running, idle"
}

function startedLabel(startedAt: string): string {
  const date = new Date(startedAt)
  return Number.isNaN(date.getTime()) ? startedAt : date.toLocaleString()
}

// What the working directory is, said under it.
function directoryNote(agent: AgentInfo): string {
  if (agent.clone) return "A job clone Legato made for this agent."
  if (agent.reeve) return "The Reeve's own folder."
  return "The directory the agent was started in."
}

// AgentInfoDialog shows what Legato knows about an agent, above all the
// directory it was started in, which can be copied. It changes nothing.
export function AgentInfoDialog({ agent, onClose }: { agent: AgentInfo; onClose: () => void }) {
  const { baseUrl } = useServer()
  const [resources, setResources] = useState<string[] | null>(null)
  const [resourcesFailed, setResourcesFailed] = useState(false)

  useEffect(() => {
    let live = true
    attachedResources(baseUrl, agent.task_id)
      .then((names) => { if (live) setResources(names) })
      .catch(() => { if (live) setResourcesFailed(true) })
    return () => { live = false }
  }, [baseUrl, agent.task_id])

  const taskId = agent.card_task_id || agent.task_id
  const slot = agent.slot && agent.slot !== "main" ? agent.slot : ""

  return (
    <Dialog open onClose={onClose}>
      <DialogPanel className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Agent info</DialogTitle>
          <DialogClose />
        </DialogHeader>
        <DialogBody className="space-y-5">
          <p className="m-0 break-words text-[15px] font-semibold text-lw-text">{agent.task_title || agent.task_id}</p>

          <Section title="Where it started">
            {agent.working_dir ? (
              <>
                <CopyField label="Working directory" value={agent.working_dir} />
                <p className="m-0 text-[11px] text-lw-faint sm:pl-[9.25rem]">{directoryNote(agent)}</p>
              </>
            ) : (
              <PlainField label="Working directory" value={<span className="text-lw-text-2">No working directory is recorded for this agent.</span>} />
            )}
            {agent.clone && <CopyField label="Repository" value={agent.clone.repo} />}
            {agent.clone?.branch && <CopyField label="Branch" value={agent.clone.branch} copyLabel="branch" />}
          </Section>

          <Section title="Agent">
            <CopyField label="Task" value={taskId} copyLabel="task ID" />
            {slot && <PlainField label="Slot" value={slot} />}
            <PlainField label="Agent tool" value={agentKindLabel(agent.agent_kind)} />
            {agent.account && <PlainField label="Account" value={agent.account_label || agent.account} />}
            {agent.engram && <PlainField label="Engram" value={agent.engram} />}
            <PlainField label="Driven" value={agent.has_chimera_endpoint ? "Native panel" : "Through its terminal"} />
            {agent.role && <PlainField label="Role" value={agent.role} />}
            {agent.group && <PlainField label="Group" value={agent.group} />}
            <PlainField label="Status" value={activityLabel(agent)} />
            <PlainField label="Started" value={startedLabel(agent.started_at)} />
          </Section>

          <Section title="Access">
            <PlainField label="Sandbox" value={sandboxLabel(agent)} />
            <PlainField
              label="Resources"
              value={resourcesFailed
                ? <span className="text-lw-text-2">Could not be read</span>
                : resources === null
                  ? <span className="text-lw-text-2">Loading…</span>
                  : resources.length === 0
                    ? <span className="text-lw-text-2">None</span>
                    : <span className="flex flex-wrap gap-1">{resources.map((name) => <span key={name} className="rounded bg-lw-raised px-1.5 font-lw-mono text-xs">{name}</span>)}</span>}
            />
          </Section>

          <Section title="Identifiers">
            {agent.execution_id && <CopyField label="Execution" value={agent.execution_id} copyLabel="execution ID" />}
            {agent.tmux_session && <CopyField label="tmux session" value={agent.tmux_session} copyLabel="tmux session" />}
            {agent.workflow_run_id && <CopyField label="Workflow run" value={agent.workflow_run_id} copyLabel="workflow run ID" />}
          </Section>
        </DialogBody>
      </DialogPanel>
    </Dialog>
  )
}
