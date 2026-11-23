import { useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { Loader2 } from "lucide-react"
import type { AgentInfo } from "../hooks/useWebSocket"
import type { AgentContext } from "../hooks/useAgentContexts"
import { useAgentGlance } from "../hooks/useAgentGlance"
import {
  backgroundSummary,
  glanceItems,
  orderForEnsemble,
  plainText,
  type AgentConversation,
  type GlanceItem,
  type ToolState,
} from "../lib/agentGlance"
import { agentToolLabel } from "../lib/agentKind"
import { formatAgentTime, stateSeconds } from "../lib/agentList"
import { glanceSection, needsYouReason, stageLabel, unreadDone, workStages, type GlanceSection, type WorkStage } from "../lib/agentStage"
import type { Approval, ApprovalDecision } from "../lib/approvals"
import { showApproval } from "../lib/railState"
import { cn } from "../lib/utils"
import { ActivityBar, ContextMeter } from "./AgentMeters"
import { StageTrack } from "./StageTrack"

export interface AgentEnsembleProps {
  // The running agents, in any order.
  agents: AgentInfo[]
  // How full each native agent's context window is, by task ID.
  contexts?: Record<string, AgentContext>
  onOpen: (taskId: string) => void
  onSpawn: () => void
  onShowList: () => void
  // Sends a message to an agent, as its own message box would.
  onSend?: (taskId: string, content: string) => void
  // The approvals waiting for the operator, and how one is decided: a card
  // in a stage band offers Allow and Deny for its agent's.
  approvals?: Approval[]
  onDecide?: (approval: Approval, decision: ApprovalDecision) => void
}

// The header's counts, by glance group. The Reeve is left out of every
// count.
const counts: Array<{ section: GlanceSection; count: (n: number) => string; glyph: string; tone: string }> = [
  { section: "needs-you", count: (n) => `${n} ${n === 1 ? "needs" : "need"} you`, glyph: "◆", tone: "text-lw-needs" },
  { section: "your-turn", count: (n) => `${n} your turn`, glyph: "◇", tone: "text-lw-needs opacity-85" },
  { section: "working", count: (n) => `${n} working`, glyph: "●", tone: "text-lw-live" },
  { section: "finished", count: (n) => `${n} finished`, glyph: "✓", tone: "text-lw-done" },
]

// Where the viewer's choice of layout is kept on this device.
export const ENSEMBLE_LAYOUT_KEY = "legato.ensemble.layout"

type EnsembleLayout = "tiles" | "stage"

function rememberedLayout(): EnsembleLayout {
  try {
    return localStorage.getItem(ENSEMBLE_LAYOUT_KEY) === "stage" ? "stage" : "tiles"
  } catch {
    return "tiles"
  }
}

// How an agent's state is written on its tile, and the colors that go with it.
function stateOf(agent: AgentInfo): { label: string; glyph: string; chip: string; frame: string; ground: string } {
  switch (agent.activity) {
    case "waiting":
      return { label: "needs you", glyph: "◆", chip: "bg-lw-needs-soft text-lw-needs", frame: "border-[var(--theme-border,#4a3a1a)]", ground: "bg-lw-surface" }
    case "error":
      return { label: "error", glyph: "✕", chip: "bg-lw-error-soft text-lw-error", frame: "border-[var(--theme-border,#4a2a2e)]", ground: "bg-lw-surface" }
    case "working":
      return { label: "running", glyph: "●", chip: "bg-lw-live-soft text-lw-live", frame: "border-[var(--theme-border,#2d3a33)]", ground: "bg-lw-surface" }
    case "background": {
      const waitsOn = backgroundSummary(agent.background_jobs)
      return { label: waitsOn ? `background · ${waitsOn}` : "background", glyph: "◌", chip: "bg-lw-live-soft text-lw-live", frame: "border-lw-line", ground: "bg-lw-surface" }
    }
    default:
      return { label: "idle", glyph: "○", chip: "bg-lw-idle-soft text-lw-text-2", frame: "border-lw-line", ground: "bg-[var(--theme-surface,#141417)]" }
  }
}

// AgentEnsemble shows every running agent at once: a tile for each with what
// it is at, the agents that need the person first, or by stage in bands.
export function AgentEnsemble({ agents, contexts = {}, onOpen, onSpawn, onShowList, onSend, approvals = [], onDecide }: AgentEnsembleProps) {
  const ordered = orderForEnsemble(agents)
  const counted = agents.filter((agent) => !agent.reeve)
  const [layout, setLayoutState] = useState<EnsembleLayout>(rememberedLayout)
  const setLayout = (next: EnsembleLayout) => {
    setLayoutState(next)
    try { localStorage.setItem(ENSEMBLE_LAYOUT_KEY, next) } catch { /* not remembered */ }
  }
  const segment = (on: boolean) => cn("h-[26px] rounded px-2.5 text-xs", on ? "bg-lw-selected text-lw-text" : "text-lw-text-2 hover:text-lw-text")

  return (
    <section aria-label="Ensemble" className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-lw-ground font-lw text-[13px] leading-5 text-lw-text">
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-[var(--theme-border,#222228)] px-5 py-3.5">
        <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1">
          <h1 className="m-0 text-lg font-semibold leading-6">Ensemble</h1>
          <ul aria-label="Agents by state" className="m-0 flex list-none flex-wrap items-center gap-x-3 gap-y-1 p-0">
            {counts.map(({ section, count, glyph, tone }) => {
              const total = counted.filter((agent) => glanceSection(agent) === section).length
              return total > 0 && <li key={section} className={tone}><span aria-hidden="true">{glyph}</span> {count(total)}</li>
            })}
          </ul>
        </div>
        <div className="flex items-center gap-2">
          <div role="group" aria-label="Layout" className="flex rounded-md border border-lw-line bg-lw-surface p-0.5">
            <button type="button" aria-pressed={layout === "tiles"} onClick={() => setLayout("tiles")} className={segment(layout === "tiles")}>Tiles</button>
            <button type="button" aria-pressed={layout === "stage"} onClick={() => setLayout("stage")} className={segment(layout === "stage")}>By stage</button>
            <button type="button" aria-pressed="false" onClick={onShowList} className={segment(false)}>List</button>
          </div>
          <button
            type="button"
            aria-label="Spawn agent"
            onClick={onSpawn}
            className="h-8 rounded-md bg-lw-accent px-3.5 text-[13px] font-medium text-[var(--theme-canvas,#fff)] hover:brightness-110"
          >
            + Spawn agent
          </button>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5 pt-4">
        {ordered.length === 0
          ? <p className="py-12 text-center text-lw-faint">No agents are running.</p>
          : layout === "stage"
          ? (
            <div data-ensemble-bands className="flex flex-col gap-2">
              {stageRuns(ordered).map((run) => run.agents.length === 0
                ? <EmptyStages key={run.stages.join()} stages={run.stages} />
                : <StageBand key={run.stages[0]} stage={run.stages[0]} agents={run.agents} contexts={contexts} approvals={approvals} onDecide={onDecide} onOpen={onOpen} onSend={onSend} />)}
            </div>
          )
          : (
            // The rows share the height of the window, down to the least a
            // tile needs; only then does the ensemble scroll.
            <div data-ensemble-grid className="grid min-h-full auto-rows-[minmax(15rem,1fr)] gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
              {ordered.map((agent) => (
                <AgentTile key={agent.task_id} agent={agent} context={contexts[agent.task_id]} onOpen={onOpen} onSend={onSend} />
              ))}
            </div>
          )}
      </div>
    </section>
  )
}

// A run of the By stage layout: one stage with its agents, or stages in a
// row with none, which are drawn as one line.
interface StageRun {
  stages: WorkStage[]
  agents: AgentInfo[]
}

// stageRuns splits agents by stage, in stage order, merging consecutive
// stages that hold nothing. An agent with no stage (the Reeve) is in none.
function stageRuns(agents: AgentInfo[]): StageRun[] {
  const runs: StageRun[] = []
  for (const stage of workStages) {
    const at = agents.filter((agent) => agent.stage === stage)
    const last = runs[runs.length - 1]
    if (at.length === 0 && last && last.agents.length === 0) last.stages.push(stage)
    else runs.push({ stages: [stage], agents: at })
  }
  return runs
}

// EmptyStages is one thin line for stages with no agents, which still names
// them: "Exploring · Planning · none".
function EmptyStages({ stages }: { stages: WorkStage[] }) {
  return (
    <p role="note" className="m-0 flex items-center gap-2 rounded-lg border border-dashed border-lw-line px-3 py-1.5 text-xs text-lw-faint">
      {[...stages.map(stageLabel), "none"].join(" · ")}
    </p>
  )
}

// StageBand is the agents at one stage, those that need the operator
// first: a header (the track, the word, the count and the needs-you marks)
// in a column on the left of a wide screen, and the agents flowing across
// beside it. A band is as tall as its agents. On a phone the bands stack, and
// a band opens by itself when it holds an agent that needs the operator.
function StageBand({ stage, agents, contexts, approvals, onDecide, onOpen, onSend }: {
  stage: WorkStage
  agents: AgentInfo[]
  contexts: Record<string, AgentContext>
  approvals: Approval[]
  onDecide?: (approval: Approval, decision: ApprovalDecision) => void
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
}) {
  const needs = agents.filter((agent) => glanceSection(agent) === "needs-you").length
  const turn = agents.filter((agent) => glanceSection(agent) === "your-turn").length
  const [open, setOpen] = useState(needs > 0)
  // An agent coming to need the operator opens its band, as it would have
  // at the start; the operator may close it again.
  const [needed, setNeeded] = useState(needs > 0)
  if ((needs > 0) !== needed) {
    setNeeded(needs > 0)
    if (needs > 0) setOpen(true)
  }
  const label = stageLabel(stage)
  const marks = [needs > 0 && `◆ ${needs}`, turn > 0 && `◇ ${turn}`].filter(Boolean).join(" · ")
  return (
    <section aria-label={label} className="flex min-w-0 flex-col gap-2.5 rounded-xl border border-lw-line bg-[var(--theme-surface,#141417)] p-2.5 lg:grid lg:grid-cols-[190px_minmax(0,1fr)] lg:gap-3">
      <h2 className="m-0 text-[13px] font-semibold">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
          className="flex min-h-11 w-full items-center gap-2 px-1 text-left lg:pointer-events-none lg:min-h-0 lg:pt-1"
        >
          <StageTrack stage={stage} />
          {label}
          <span className="font-lw-mono text-xs font-normal text-lw-text-2">{agents.length}</span>
          {marks && <span className="ml-auto text-xs font-medium text-lw-needs lg:ml-1">{marks}</span>}
        </button>
      </h2>
      {stage === "finished"
        // Finished agents are one-line rows: there is nothing to do for them
        // but read what they left.
        ? (
          <ul className={cn("m-0 min-w-0 list-none flex-col gap-0.5 p-0", open ? "flex" : "hidden lg:flex")}>
            {agents.map((agent) => <li key={agent.task_id}><FinishedRow agent={agent} onOpen={onOpen} /></li>)}
          </ul>
        )
        : (
          <div className={cn("min-w-0 grid-cols-[repeat(auto-fill,minmax(300px,1fr))] items-start gap-2.5", open ? "grid" : "hidden lg:grid")}>
            {agents.map((agent) => (
              <StageCard
                key={agent.task_id}
                agent={agent}
                context={contexts[agent.task_id]}
                approval={approvalFor(agent, approvals)}
                onDecide={onDecide}
                onOpen={onOpen}
                onSend={onSend}
              />
            ))}
          </div>
        )}
    </section>
  )
}

// How a card in a band leads: what the agent needs from the operator, in
// the glance's word and glyph. Background work is working, in green, with
// its own glyph and word.
function glanceOf(agent: AgentInfo): { glyph: string; word: string; tone: string } {
  if (agent.activity === "error") return { glyph: "✕", word: "Error", tone: "bg-lw-error-soft text-lw-error" }
  switch (glanceSection(agent)) {
    case "needs-you":
      return { glyph: "◆", word: "Needs you", tone: "bg-lw-needs-soft text-lw-needs" }
    case "your-turn":
      return { glyph: "◇", word: "Your turn", tone: "bg-lw-needs-soft text-lw-needs" }
    case "working":
      return agent.activity === "background"
        ? { glyph: "◌", word: "Working · background", tone: "bg-lw-live-soft text-lw-live" }
        : { glyph: "●", word: "Working", tone: "bg-lw-live-soft text-lw-live" }
    case "finished":
      return { glyph: "✓", word: "Finished", tone: "bg-lw-done-soft text-lw-done" }
    default:
      return { glyph: "■", word: "Ended", tone: "bg-lw-idle-soft text-lw-text-2" }
  }
}

// approvalFor is the pending approval of the agent a card names: by the
// execution that asked, else by its task and slot (a named slot's agent is
// <task>_<slot>, its approval the task's with the slot in its details).
function approvalFor(agent: AgentInfo, approvals: Approval[]): Approval | undefined {
  const byExecution = agent.execution_id ? approvals.find((approval) => approval.execution_id === agent.execution_id) : undefined
  if (byExecution) return byExecution
  const task = agent.card_task_id || agent.task_id
  const slot = agent.slot && agent.slot !== "main" ? agent.slot : ""
  return approvals.find((approval) => {
    if (agent.execution_id && approval.execution_id) return false
    const approvalSlot = typeof approval.details.slot === "string" ? approval.details.slot : ""
    return approval.task_id === task && approvalSlot === slot
  })
}

// What an approval asks to allow, beyond its title: a command (or target)
// as code, and a plan or the resources and operations as text.
function approvalContent(approval: Approval): { command: string; text: string } {
  const d = approval.details
  const command = d.command || d.target || (d.plan ? "" : approval.summary ?? "")
  const scope = [d.credential, d.operations?.join(", "), d.resources?.join(", ")].filter(Boolean).join(" · ")
  const text = d.plan ? plainText(d.plan) : scope
  return { command: command === approval.title ? "" : command, text }
}

function couldNotRead(agent: AgentInfo): string {
  return agent.has_chimera_endpoint ? "Could not read this agent's conversation." : "Could not read this agent's terminal."
}

const cardButton = "h-[30px] shrink-0 rounded-md border border-lw-line-strong bg-lw-raised px-3 text-xs text-lw-text hover:border-[var(--theme-border,#4a4955)]"

// StageCard is an agent in a stage band, light enough to scan: the glance
// and its time, the ID and title, why it is at its stage and its tool, then
// one body (what it needs from the operator, what it is doing now, or its
// last reply) and the actions that fit. It has no fixed height: nothing it
// shows is cut by one.
function StageCard({ agent, context, approval, onDecide, onOpen, onSend }: {
  agent: AgentInfo
  context?: AgentContext
  approval?: Approval
  onDecide?: (approval: Approval, decision: ApprovalDecision) => void
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
}) {
  const { conversation, pane, failed, observe } = useAgentGlance(agent)
  const glance = glanceOf(agent)
  const section = glanceSection(agent)
  // A terminal agent has no conversation: what its terminal shows stands in.
  const paneLines = pane ? pane.split("\n").map((line) => line.trimEnd()).filter(Boolean) : []
  const unread = failed && !conversation && pane === null
  const needs = section === "needs-you"
  const working = section === "working"
  const time = formatAgentTime(stateSeconds(agent))
  const why = [agent.stage_reason, agentToolLabel(agent.agent_kind, agent.engram)].filter(Boolean).join(" · ")
  const [replying, setReplying] = useState(false)

  let body: ReactNode
  if (needs) {
    body = <NeedsBox agent={agent} conversation={conversation} approval={approval} onDecide={onDecide} onOpen={onOpen} />
  } else if (working) {
    const now = nowLine(agent, conversation) ?? (paneLines.length > 0 ? { tool: "", text: paneLines[paneLines.length - 1] } : null)
    body = unread && !now
      ? <Quiet>{couldNotRead(agent)}</Quiet>
      : (
        <p aria-label="Now" className="m-0 truncate font-lw-mono text-[11px] leading-4 text-lw-text-2">
          {now ? <>{now.tool && <span className="text-lw-link">{now.tool}</span>}{now.tool && " "}{now.text}</> : "Working…"}
        </p>
      )
  } else {
    const reply = conversation ? newestReply(conversation) : null
    if (reply) body = <blockquote className="m-0 line-clamp-4 whitespace-pre-line border-l-2 border-lw-line-strong pl-2.5 text-[13px] leading-5 text-lw-text">{reply}</blockquote>
    else if (paneLines.length > 0) body = <pre className="m-0 overflow-hidden whitespace-pre font-lw-mono text-[11px] leading-4 text-lw-text-2">{paneLines.slice(-4).join("\n")}</pre>
    else body = <Quiet>{unread ? couldNotRead(agent) : "No reply yet."}</Quiet>
  }

  return (
    <article
      ref={observe}
      aria-label={`${agent.task_id}, ${glance.word.split(" · ")[0].toLowerCase()}`}
      className={cn("flex min-w-0 flex-col gap-2 rounded-[10px] border bg-lw-surface px-3 py-2.5", needs ? "border-[var(--theme-border,#4a3a1a)]" : "border-lw-line")}
    >
      <span className={cn("self-start rounded px-1.5 text-[11px] font-medium", glance.tone)}>
        {`${glance.glyph} ${glance.word}${time ? ` · ${time}` : ""}`}
      </span>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="font-lw-mono text-[11px] text-lw-faint">{agent.task_id}</span>
        <span className="line-clamp-2 text-[13px] font-semibold leading-5">{agent.task_title || agent.task_id}</span>
        {why && <span className="truncate text-xs text-lw-text-2">{why}</span>}
      </div>
      {body}
      {!needs && (
        <div className="flex flex-col gap-2">
          {replying && onSend && <CardMessageBox agent={agent} idle={!working} onSend={onSend} />}
          <div className="flex items-center gap-2">
            {onSend && (
              <button type="button" aria-expanded={replying} onClick={() => setReplying((value) => !value)} className={cardButton}>
                <span aria-hidden="true">{working ? "Message" : "Reply"}</span>
                <span className="sr-only">{working ? `Message ${agent.task_id}` : `Reply to ${agent.task_id}`}</span>
              </button>
            )}
            <button type="button" aria-label={`Open ${agent.task_id}`} onClick={() => onOpen(agent.task_id)} className={cardButton}>Open</button>
            {context && <span className="ml-auto"><ContextMeter share={context.share} width="w-[60px]" /></span>}
          </div>
        </div>
      )}
      {needs && context && <ContextMeter share={context.share} width="w-[60px]" />}
    </article>
  )
}

// What a working agent is at now, in one line: the job it waits on in the
// background, else its newest tool call.
function nowLine(agent: AgentInfo, conversation: AgentConversation | null): { tool: string; text: string } | null {
  const waitsOn = agent.activity === "background" ? backgroundSummary(agent.background_jobs) : ""
  if (waitsOn) return { tool: "", text: waitsOn }
  const items = conversation ? glanceItems(conversation.entries, agent.activity) : []
  const call = items.findLast((item) => item.kind === "tool")
  return call && call.kind === "tool" ? { tool: call.tool, text: call.text } : null
}

// NeedsBox is what an agent waits on the operator for, with the way to
// answer: Allow and Deny for an approval, Answer for a question, Review plan
// for a plan, else Open.
function NeedsBox({ agent, conversation, approval, onDecide, onOpen }: {
  agent: AgentInfo
  conversation: AgentConversation | null
  approval?: Approval
  onDecide?: (approval: Approval, decision: ApprovalDecision) => void
  onOpen: (taskId: string) => void
}) {
  const permission = conversation?.permission
  const question = conversation?.question
  const asked = approval?.title
    || (permission ? (permission.plan ? "Asks you to approve a plan" : `Wants to run: ${permission.command || permission.reason || permission.tool}`) : "")
    || question?.text
    || (agent.plan_pending ? "Plan to approve" : needsYouReason(agent))
  // What the approval would allow, which its title does not say: the
  // command, the plan, the target.
  const shown = approval ? approvalContent(approval) : { command: "", text: "" }
  // Only an agent's own permission is decided here, and never one that must
  // be signed: the rest open the approval, where the operator sees all of it.
  const inline = approval?.source === "agent_permission" && !approval.signed
  const allow = inline ? approval?.options.find((option) => option.kind === "allow") : undefined
  const deny = inline ? approval?.options.find((option) => option.kind === "deny") : undefined
  const primary = "h-[30px] shrink-0 rounded-md bg-lw-accent px-3 text-xs font-medium text-[var(--theme-canvas,#fff)] hover:brightness-110"
  let actions: ReactNode
  if (approval && onDecide && allow && deny) {
    actions = <>
      <button type="button" onClick={() => onDecide(approval, { decision: "allow", option: allow.id })} className={primary}>Allow</button>
      <button type="button" onClick={() => onDecide(approval, { decision: "deny" })} className={cardButton}>Deny</button>
    </>
  } else if (approval) {
    actions = <button type="button" onClick={() => showApproval(approval.id)} className={primary}>Answer</button>
  } else if (agent.plan_pending) {
    actions = <button type="button" onClick={() => onOpen(agent.task_id)} className={primary}>Review plan</button>
  }
  return (
    <div role="group" aria-label="Needs you" className="flex flex-col gap-2 rounded-md border border-[var(--theme-border,#4a3a1a)] bg-lw-needs-soft/40 px-2.5 py-2">
      <p className="m-0 line-clamp-4 whitespace-pre-line text-xs leading-[18px] text-lw-text">{asked}</p>
      {shown.command && <code className="line-clamp-3 whitespace-pre-wrap break-all rounded bg-lw-ground px-2 py-1 font-lw-mono text-[11px] text-lw-needs">{shown.command}</code>}
      {shown.text && <p className="m-0 line-clamp-6 whitespace-pre-line text-xs leading-[18px] text-lw-text-2">{shown.text}</p>}
      <div className="flex flex-wrap items-center gap-2">
        {actions}
        <button type="button" aria-label={`Open ${agent.task_id}`} onClick={() => onOpen(agent.task_id)} className={cardButton}>Open</button>
      </div>
    </div>
  )
}

// A card's message box, opened in place by Reply: Enter sends what was typed
// to the agent, which reads it at its next step when it is busy.
function CardMessageBox({ agent, idle, onSend }: { agent: AgentInfo; idle: boolean; onSend: (taskId: string, content: string) => void }) {
  const [draft, setDraft] = useState("")
  const label = idle ? `Reply to ${agent.task_id}` : `Message ${agent.task_id}`
  return (
    <input
      aria-label={label}
      autoFocus
      placeholder={idle ? "Reply to continue…" : "Message…"}
      value={draft}
      onChange={(event) => setDraft(event.target.value)}
      onKeyDown={(event) => {
        if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return
        event.preventDefault()
        const content = draft.trim()
        if (!content) return
        onSend(agent.task_id, content)
        setDraft("")
      }}
      className="h-[30px] min-w-0 rounded-md border border-lw-accent bg-lw-ground px-2.5 text-xs text-lw-text outline-none placeholder:text-lw-faint"
    />
  )
}

// FinishedRow is a Finished agent on one line: unread when it left a done
// note for the operator, then its ID, title and why it finished.
function FinishedRow({ agent, onOpen }: { agent: AgentInfo; onOpen: (taskId: string) => void }) {
  const unread = unreadDone(agent)
  return (
    <button
      type="button"
      aria-label={`Open ${agent.task_id}`}
      onClick={() => onOpen(agent.task_id)}
      className="flex min-h-9 w-full min-w-0 items-center gap-2.5 rounded-md px-2 text-left text-xs hover:bg-lw-raised"
    >
      <span className="flex w-2 shrink-0 justify-center">
        {unread && <span aria-label="Unread: finished with a note for you" title="Finished with a note for you" className="size-2 rounded-full bg-lw-done" />}
      </span>
      <span className="shrink-0 font-lw-mono text-lw-faint">{agent.task_id}</span>
      <span className="min-w-0 truncate font-medium text-lw-text">{agent.task_title || agent.task_id}</span>
      {agent.stage_reason && <span className="ml-auto min-w-0 shrink truncate text-lw-text-2">{agent.stage_reason}</span>}
    </button>
  )
}

function AgentTile({ agent, context, onOpen, onSend }: {
  agent: AgentInfo
  context?: AgentContext
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
}) {
  const { conversation, pane, failed, observe } = useAgentGlance(agent)
  const state = stateOf(agent)
  const native = Boolean(agent.has_chimera_endpoint)
  const idle = state.label === "idle"
  const time = formatAgentTime(stateSeconds(agent))
  const tool = native ? agentToolLabel(agent.agent_kind, agent.engram) : `${agentToolLabel(agent.agent_kind, agent.engram)} · terminal`
  const meta = [tool, agent.parent_task_id ? agent.role : ""].filter(Boolean).join(" · ")
  const lastReply = idle && conversation ? newestReply(conversation) : null

  let shown: ReactNode
  if (lastReply) {
    shown = <LastReply text={lastReply} onRead={() => onOpen(agent.task_id)} />
  } else if (conversation) {
    shown = <Newest><ConversationGlance conversation={conversation} activity={agent.activity} /></Newest>
  } else if (pane !== null) {
    shown = <Newest>{pane.trim()
      ? <pre className="m-0 whitespace-pre font-lw-mono text-[11px] leading-4 text-lw-text-2">{pane}</pre>
      : <Quiet>Nothing on its terminal yet.</Quiet>}</Newest>
  } else if (failed) {
    shown = <Quiet>{native ? "Could not read this agent's conversation." : "Could not read this agent's terminal."}</Quiet>
  } else {
    shown = <Quiet>Reading…</Quiet>
  }

  return (
    <article
      ref={observe}
      aria-label={`${agent.task_id}, ${state.label}`}
      className={cn("flex min-h-0 min-w-0 flex-col overflow-hidden rounded-[10px] border", state.frame, state.ground)}
    >
      <div className="flex flex-col gap-1 border-b border-[var(--theme-border,#222228)] px-3.5 pb-2.5 pt-3">
        <div className="flex items-center justify-between gap-2">
          <span className={cn("rounded px-2 font-lw-mono text-[11px] uppercase", state.chip)}>
            <span aria-hidden="true">{state.glyph}</span> {state.label}{time && ` ${time}`}
          </span>
          {stageLabel(agent.stage) && (
            <span className={cn("flex shrink-0 items-center gap-1 text-[11px]", agent.stage === "finished" ? "text-lw-done" : "text-lw-text-2")}>
              <StageTrack stage={agent.stage} />{stageLabel(agent.stage)}
            </span>
          )}
          <span className="ml-auto truncate font-lw-mono text-xs text-lw-faint">{agent.task_id}</span>
        </div>
        <span className="truncate text-[15px] font-semibold">{agent.task_title || agent.task_id}</span>
        <span className="truncate text-xs text-lw-text-2">{meta}</span>
      </div>

      {/* Contained in size: what the agent says never makes the tile, or its
          row, taller; the newest is kept in view and the rest cut. */}
      <div data-tile-body className="flex min-h-0 flex-1 flex-col overflow-hidden px-3.5 py-2.5 [contain:size]">{shown}</div>

      <div className="flex flex-col gap-2.5 border-t border-[var(--theme-border,#222228)] px-3.5 py-2.5">
        <div className="flex min-h-3 items-center justify-between gap-2">
          <ActivityBar states={agent.state_timeline} />
          {context && <ContextMeter share={context.share} width="w-[60px]" />}
        </div>
        <TileComposer agent={agent} idle={idle} onOpen={onOpen} onSend={onSend} />
      </div>
    </article>
  )
}

// The tile's own message box: Enter sends what was typed to the agent, which
// reads it at its next step when it is busy.
function TileComposer({ agent, idle, onOpen, onSend }: {
  agent: AgentInfo
  idle: boolean
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
}) {
  const [draft, setDraft] = useState("")
  const label = idle ? `Reply to ${agent.task_id}` : `Message ${agent.task_id}`
  return (
    <div className="flex gap-2">
      {onSend && (
        <input
          aria-label={label}
          placeholder={idle ? "Reply to continue…" : "Message…"}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return
            event.preventDefault()
            const content = draft.trim()
            if (!content) return
            onSend(agent.task_id, content)
            setDraft("")
          }}
          className={cn(
            "h-[30px] min-w-0 flex-1 rounded-md border bg-lw-ground px-2.5 text-xs text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent",
            idle ? "border-lw-accent" : "border-lw-line-strong",
          )}
        />
      )}
      <button
        type="button"
        aria-label={`Open ${agent.task_id}`}
        onClick={() => onOpen(agent.task_id)}
        className={cn("h-[30px] shrink-0 rounded-md border border-lw-line-strong bg-lw-raised px-3 text-xs text-lw-text hover:border-[var(--theme-border,#4a4955)]", !onSend && "flex-1")}
      >
        Open
      </button>
    </div>
  )
}

// The newest thing an agent said, when there is one: what an idle tile shows.
function newestReply(conversation: AgentConversation): string | null {
  const said = conversation.entries.findLast((entry) => entry.kind === "assistant" && entry.text.trim())
  return said ? plainText(said.text) : null
}

function LastReply({ text, onRead }: { text: string; onRead: () => void }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <span className="text-[11px] font-semibold uppercase tracking-[1px] text-lw-faint">Last reply</span>
      <p className="m-0 min-h-0 overflow-hidden whitespace-pre-line text-[13px] leading-5 text-lw-text [display:-webkit-box] [-webkit-box-orient:vertical] [-webkit-line-clamp:9]">{text}</p>
      <button type="button" onClick={onRead} className="self-start text-xs text-lw-link hover:text-[var(--theme-link,#c8c0ff)]">Read full reply</button>
    </div>
  )
}

// Newest keeps the end of its content in view: what does not fit is cut at
// the top, where the oldest is.
function Newest({ children }: { children: ReactNode }) {
  const box = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    if (box.current) box.current.scrollTop = box.current.scrollHeight
  })
  return <div ref={box} className="min-h-0 flex-1 overflow-hidden">{children}</div>
}

function Quiet({ children }: { children: ReactNode }) {
  return <p className="m-0 text-xs text-lw-faint">{children}</p>
}

// The end of a native agent's conversation: what was asked, said and done,
// and after it what the agent waits on the person for.
function ConversationGlance({ conversation, activity }: { conversation: AgentConversation; activity: string }) {
  const items = glanceItems(conversation.entries, activity)
  const { permission, question } = conversation
  if (items.length === 0 && !permission && !question) return <Quiet>No messages yet.</Quiet>

  return (
    <div className="flex flex-col gap-1.5 text-xs leading-[18px]">
      {items.map((item, index) => <GlanceRow key={index} item={item} />)}
      {permission && (
        <div className="flex flex-col gap-1">
          <p className="m-0 text-lw-text">
            {permission.plan ? "Asks you to approve a plan:" : !permission.tool || permission.tool === "bash" ? "Wants to run:" : `Wants to use ${permission.tool}:`}
          </p>
          {permission.plan
            ? <p className="m-0 line-clamp-3 whitespace-pre-line pl-4 text-lw-text-2">{plainText(permission.plan)}</p>
            : <code className="line-clamp-3 rounded bg-lw-ground px-2 py-1 font-lw-mono text-lw-needs">{permission.command || permission.reason}</code>}
        </div>
      )}
      {question && (
        <div className="flex flex-col">
          <p className="m-0 text-lw-text">Asks:</p>
          <p className="m-0 line-clamp-3 pl-4 text-lw-needs">{question.text}</p>
          {question.options.length > 0 && <p className="m-0 truncate pl-4 text-lw-text-2">{question.options.join(" · ")}</p>}
        </div>
      )}
    </div>
  )
}

const toolMarks: Record<ToolState, { mark: ReactNode; text: string }> = {
  done: { mark: <span className="text-lw-live">●</span>, text: "text-lw-text-2" },
  failed: { mark: <span className="text-lw-error">✕</span>, text: "text-lw-text-2" },
  running: { mark: <Loader2 size={10} className="animate-spin text-lw-needs motion-reduce:animate-none" />, text: "text-lw-text" },
  waiting: { mark: <span className="text-lw-needs">◆</span>, text: "text-lw-text" },
  stopped: { mark: <span className="text-lw-faint">○</span>, text: "text-lw-text-2" },
}

function GlanceRow({ item }: { item: GlanceItem }) {
  switch (item.kind) {
    case "person":
      return <p className="m-0 line-clamp-2 self-end whitespace-pre-line rounded-lg bg-lw-selected px-2.5 py-1 text-lw-text">{item.text}</p>
    case "relayed":
      return (
        <p className="m-0 line-clamp-2 self-end rounded-lg bg-lw-raised px-2.5 py-1 text-lw-text-2">
          <span className="mr-1.5 text-[10px] font-medium uppercase tracking-wide text-teal-400">{item.from}</span>
          {item.text}
        </p>
      )
    case "said":
      return (
        <p className={cn("m-0 whitespace-pre-line pl-4", item.newest ? "line-clamp-6 text-lw-text" : "line-clamp-2 text-lw-text-2")}>
          {plainText(item.text)}
        </p>
      )
    case "tool": {
      const { mark, text } = toolMarks[item.state]
      return (
        <p data-state={item.state} className={cn("m-0 flex min-w-0 items-start gap-2", text)}>
          <span aria-hidden="true" className="flex w-2.5 shrink-0 justify-center pt-[3px]">{mark}</span>
          <span className="w-[84px] shrink-0 truncate font-lw-mono text-lw-link">{item.tool}</span>
          <span className="min-w-0 line-clamp-2">{item.text}</span>
        </p>
      )
    }
    case "earlier":
      return (
        <p className="m-0 text-lw-faint">
          {item.count === null ? "earlier calls" : item.count === 1 ? "1 earlier call" : `${item.count} earlier calls`}
        </p>
      )
  }
}
