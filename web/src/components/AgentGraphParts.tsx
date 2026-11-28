import { useState, type KeyboardEvent } from "react"
import { Link } from "react-router-dom"
import { Maximize2, Workflow, X } from "lucide-react"

import type { AgentInfo } from "../lib/agentWire"
import { linkLabels, linkLines, workflowWords, type AgentGraphLayout, type GraphNode, type NodeWorkflow } from "../lib/agentGraph"
import { runState, stepSegments } from "../lib/workflowView"
import { stageLabel, type WorkStage } from "../lib/agentStage"
import { formatAgentTime, stateSeconds } from "../lib/agentList"
import { agentToolLabel } from "../lib/agentKind"
import { subagentsLabel } from "../lib/agentGlance"
import { cn } from "../lib/utils"
import { useReeveName } from "../lib/reeveName"
import { StatePill, StepBar } from "./workflow-status/WorkflowRunStatus"
import { linkStyles } from "./agent-graph/linkStyles"
import { agentLook, subagentLook } from "./agent-graph/look"
import { StageMenu } from "./StageMenu"

// The ensemble graph's toolbar and selection card. See AgentGraphView.tsx.

function LinkSwatch({ kind }: { kind: keyof typeof linkStyles }) {
  const style = linkStyles[kind]
  return <svg aria-hidden="true" width="22" height="6" className="shrink-0">
    <line x1="1" y1="3" x2="21" y2="3" stroke={style.stroke} strokeWidth={style.width} strokeDasharray={style.dash} strokeLinecap="round" />
  </svg>
}

function NeedsYouList({ needsYou, onFocus }: { needsYou: AgentInfo[]; onFocus: (id: string) => void }) {
  if (needsYou.length === 0) return null
  return (
    <ul aria-label="Needs you" className="m-0 flex min-w-0 list-none flex-nowrap items-center gap-1.5 overflow-x-auto p-0">
      {needsYou.map((agent) => (
        <li key={agent.task_id}>
          <button type="button" onClick={() => onFocus(agent.task_id)} className="flex h-7 items-center gap-1 rounded-md border border-lw-needs/50 bg-lw-needs-soft px-2 text-xs text-lw-needs hover:border-lw-needs">
            <span aria-hidden="true">◆</span><span className="font-lw-mono">{agent.task_id}</span>
          </button>
        </li>
      ))}
    </ul>
  )
}

// How many more need the operator at stages the filter leaves out.
function HiddenNeeds({ count, onShowAll }: { count: number; onShowAll?: (stage: WorkStage | null) => void }) {
  if (count === 0 || !onShowAll) return null
  return (
    <span className="shrink-0 whitespace-nowrap text-xs text-lw-needs">
      {count} more {count === 1 ? "needs" : "need"} you · <button type="button" onClick={() => onShowAll(null)} className="text-lw-link hover:underline">Show all</button>
    </span>
  )
}

function LinkLegend({ kinds }: { kinds: (keyof typeof linkStyles)[] }) {
  if (kinds.length === 0) return null
  return (
    <ul aria-label="Links" className="m-0 hidden list-none flex-nowrap items-center gap-x-3 p-0 text-[11px] text-lw-text-2 xl:flex">
      {kinds.map((kind) => <li key={kind} className="flex items-center gap-1"><LinkSwatch kind={kind} />{linkLabels[kind]}</li>)}
    </ul>
  )
}

// One line, whatever needs the operator: a toolbar that wrapped would move
// the whole graph down on a change of state.
export function GraphToolbar({ counted, stageFilter, onStageFilter, phone, needsYou, hiddenNeeds, runId, kinds, onFocus, onFit }: {
  counted: AgentInfo[]
  stageFilter: WorkStage | null
  onStageFilter?: (stage: WorkStage | null) => void
  phone: boolean
  needsYou: AgentInfo[]
  hiddenNeeds: number
  // The run the address came from, to link back to.
  runId?: string
  kinds: (keyof typeof linkStyles)[]
  onFocus: (id: string) => void
  onFit: () => void
}) {
  return (
    <div className="flex h-11 shrink-0 flex-nowrap items-center gap-x-3 border-b border-[var(--theme-border,#222228)] px-5">
      {onStageFilter && <StageMenu agents={counted} filter={stageFilter} onFilter={onStageFilter} touch={phone} />}
      <NeedsYouList needsYou={needsYou} onFocus={onFocus} />
      <HiddenNeeds count={hiddenNeeds} onShowAll={onStageFilter} />
      <div className="ml-auto flex shrink-0 items-center gap-3">
        {runId && <Link to={`/workflows/runs/${encodeURIComponent(runId)}`} className="text-xs text-lw-link hover:underline">Back to workflow</Link>}
        <LinkLegend kinds={kinds} />
        <button type="button" aria-label="Fit the graph" title="Fit the graph" onClick={onFit} className="flex h-7 w-7 items-center justify-center rounded-md border border-lw-line text-lw-text-2 hover:text-lw-text">
          <Maximize2 size={14} aria-hidden="true" />
        </button>
      </div>
    </div>
  )
}

// The selected sub-agent: what it does, or waits on.
function SubagentLine({ node }: { node: GraphNode }) {
  if (node.kind !== "subagent") return null
  const look = subagentLook(node.state)
  return <span className="rounded-md bg-lw-raised px-2 py-1 text-xs"><span className={look.text}>{look.glyph} {node.waitsOn ?? look.label}</span> · sub-agent {node.job?.label}</span>
}

function NodeBadges({ badges }: { badges: GraphNode["badges"] }) {
  if (!badges?.length) return null
  return (
    <span className="flex flex-wrap gap-1">
      {badges.map((badge) => <span key={badge.kind} className="rounded bg-lw-accent-soft px-1.5 text-xs text-lw-accent-text">{badge.label}{badge.detail && ` · ${badge.detail}`}</span>)}
    </span>
  )
}

// Who the agent is linked to, after what its sub-agents do.
function AgentLinks({ subagents, lines }: { subagents: string; lines: string[] }) {
  if (lines.length === 0 && !subagents) return null
  return (
    <ul aria-label="Links of this agent" className="m-0 flex list-none flex-col gap-0.5 p-0 text-xs text-lw-text-2">
      {subagents && <li>{subagents}</li>}
      {lines.map((line) => <li key={line}>{line}</li>)}
    </ul>
  )
}

// A message box (when the graph can send) and Open. Enter sends a message
// that says something; Shift+Enter and composing do not.
function MessageAndOpen({ taskId, draft, setDraft, onOpen, onSend }: { taskId: string; draft: string; setDraft: (draft: string) => void; onOpen: (taskId: string) => void; onSend?: (taskId: string, content: string) => void }) {
  const send = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return
    event.preventDefault()
    const content = draft.trim()
    if (!content || !onSend) return
    onSend(taskId, content)
    setDraft("")
  }
  return (
    <div className="flex gap-2">
      {onSend && (
        <input
          aria-label={`Message ${taskId}`}
          placeholder="Message…"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={send}
          className="h-[30px] min-w-0 flex-1 rounded-md border border-lw-line-strong bg-lw-ground px-2.5 text-xs text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent"
        />
      )}
      <button type="button" aria-label={`Open ${taskId}`} onClick={() => onOpen(taskId)} className={cn("h-[30px] shrink-0 rounded-md bg-lw-accent px-3 text-xs font-medium text-[var(--theme-canvas,#fff)] hover:brightness-110", !onSend && "flex-1")}>
        Open
      </button>
    </div>
  )
}

function SelectionHeading({ agent, onClose }: { agent: AgentInfo; onClose: () => void }) {
  const look = agentLook(agent, useReeveName())
  const time = formatAgentTime(stateSeconds(agent))
  return <>
    <div className="flex items-center gap-2">
      <span className={cn("font-lw-mono text-[11px] uppercase", look.text)}><span aria-hidden="true">{look.glyph}</span> {look.label}{time && ` ${time}`}</span>
      <span className="ml-auto font-lw-mono text-xs text-lw-faint">{agent.task_id}</span>
      <button type="button" aria-label="Close" onClick={onClose} className="flex h-6 w-6 items-center justify-center rounded text-lw-text-2 hover:text-lw-text"><X size={14} aria-hidden="true" /></button>
    </div>
    <span className="text-[15px] font-semibold leading-5">{agent.task_title || agent.task_id}</span>
    <span className="text-xs text-lw-text-2">{[stageLabel(agent.stage), agent.stage_reason, agentToolLabel(agent.agent_kind, agent.engram)].filter(Boolean).join(" · ")}</span>
  </>
}

// SelectionCard is the selected agent (or the agent of a selected
// sub-agent): what it is at, who it is linked to, a message box and Open.
export function SelectionCard({ node, layout, agents, phone, onOpen, onSend, onClose, onShowRun }: {
  node: GraphNode
  layout: AgentGraphLayout
  agents: AgentInfo[]
  phone: boolean
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
  onClose: () => void
  onShowRun: (runId: string, button: HTMLElement) => void
}) {
  const agent = agents.find((candidate) => candidate.task_id === node.agentId)
  // The draft stays while another agent is selected, as the card does.
  const [draft, setDraft] = useState("")
  if (!agent) return null
  return (
    <section
      aria-label={agent.task_id}
      className={cn(
        "absolute z-10 flex flex-col gap-2 border border-lw-line-strong bg-lw-surface p-3.5 font-lw text-[13px] text-lw-text shadow-lg",
        phone ? "inset-x-0 bottom-0 rounded-t-xl" : "right-4 top-4 w-80 rounded-xl",
      )}
    >
      <SelectionHeading agent={agent} onClose={onClose} />
      <SubagentLine node={node} />
      <NodeBadges badges={node.badges} />
      {node.workflows?.map((workflow) => <WorkflowSection key={workflow.run.id} workflow={workflow} onShowRun={onShowRun} />)}
      <AgentLinks subagents={subagentsLabel(agent.background_jobs)} lines={linkLines(agent.task_id, layout.edges, agents)} />
      <MessageAndOpen taskId={agent.task_id} draft={draft} setDraft={setDraft} onOpen={onOpen} onSend={onSend} />
    </section>
  )
}

// WorkflowSection is a run the selected agent works for: the run, how far
// it is, the agent's step and role, and its graph on asking.
function WorkflowSection({ workflow, onShowRun }: { workflow: NodeWorkflow; onShowRun: (runId: string, button: HTMLElement) => void }) {
  const { run } = workflow
  const words = workflowWords(workflow)
  const state = runState(run)
  return (
    <div role="group" aria-label="Workflow run" className="flex flex-col gap-1.5 rounded-lg border border-lw-line bg-lw-raised p-2.5 text-xs">
      <span className="flex min-w-0 items-center gap-1.5">
        <Workflow size={12} aria-hidden="true" className="shrink-0 text-lw-text-2" />
        <Link to={`/workflows/runs/${encodeURIComponent(run.id)}`} className="min-w-0 truncate font-medium text-lw-link hover:underline">{run.workflow_title || run.workflow_name}</Link>
        <StatePill tone={state.tone} label={state.label} className="ml-auto" />
      </span>
      <StepBar segments={stepSegments(run, run.steps)} label={`${state.label}, step ${words.number} of ${words.total}`} />
      <span className="text-lw-text">{words.role}</span>
      {words.now && <span className="text-lw-text-2">{words.now}</span>}
      <button type="button" onClick={(event) => onShowRun(run.id, event.currentTarget)} className="h-7 self-start rounded-md border border-lw-line-strong px-2.5 text-xs text-lw-text hover:border-lw-accent">
        Show run graph
      </button>
    </div>
  )
}
