// RunGraphPanel is an agent's workflow run drawn on the agents graph: the
// run page's own workflow graph, with the same live states, the agent's step
// selected. Beside the fleet on a wide screen; a sheet over it on a phone.
import { useEffect, useRef, useState } from "react"
import { Link } from "react-router-dom"
import { ExternalLink, X } from "lucide-react"
import { useModalLayer } from "../../hooks/useModalLayer"
import { useWorkflowRun } from "../../hooks/useWorkflow"
import { useNow } from "../../hooks/useNow"
import { useRunGraphExtras } from "../../hooks/useRunGraph"
import { runState, runWithAttention, stepPosition, type StepLike } from "../../lib/workflowView"
import { cn } from "../../lib/utils"
import { LazyWorkflowGraph } from "../workflow-graph/LazyWorkflowGraph"
import { StatePill } from "../WorkflowRunStatus"
import { LoadingState } from "../ui/LoadingState"

export interface RunGraphPanelProps {
  runId: string
  // The run's title, shown before the run has loaded.
  title: string
  // The agent's top-level step; its node is step:<stepId>.
  stepId?: string
  phone: boolean
  onClose: () => void
}

export function RunGraphPanel({ runId, title, stepId, phone, onClose }: RunGraphPanelProps) {
  const { data, loading, error } = useWorkflowRun(runId)
  const now = useNow(15_000)
  const { graph, extras } = useRunGraphExtras(runId, data, now)
  const stepNode = stepId ? `step:${stepId}` : null
  const [selected, setSelected] = useState<string | null>(stepNode)
  const [centerOn, setCenterOn] = useState<{ id: string; n: number } | null>(stepNode ? { id: stepNode, n: 1 } : null)
  useEffect(() => {
    setSelected(stepNode)
    setCenterOn((current) => stepNode ? { id: stepNode, n: (current?.n ?? 0) + 1 } : null)
  }, [stepNode])
  const close = useRef<HTMLButtonElement>(null)
  // On a phone the sheet covers the graph, so it is modal: the graph behind
  // it is inert, Tab stays in it and Escape closes it. Beside the fleet it
  // is not, and the graph's own Escape closes it.
  const sheet = useRef<HTMLElement>(null)
  useModalLayer({ open: phone, onClose, containerRef: sheet })
  useEffect(() => { close.current?.focus() }, [])

  const run = data?.run ? runWithAttention(data.run, data.attempts ?? []) : null
  const state = run ? runState(run) : null
  const steps: StepLike[] = (data?.steps ?? []).map((step) => ({ step_id: step.step_id, title: step.title, status: step.status }))
  const position = run ? stepPosition(run, steps) : null
  const name = data?.run?.workflow_title || title
  return (
    <section
      ref={sheet}
      aria-label={`Run graph: ${name}`}
      {...(phone ? { role: "dialog", "aria-modal": true } : {})}
      className={cn(
        "absolute z-20 flex flex-col overflow-hidden border border-lw-line-strong bg-lw-surface font-lw text-[13px] text-lw-text shadow-lg",
        phone ? "inset-0" : "bottom-4 left-4 right-[22rem] h-[45%] min-h-[16rem] rounded-xl",
      )}
    >
      <div className="flex shrink-0 flex-wrap items-center gap-x-2 gap-y-1 border-b border-lw-line px-3.5 py-2">
        <span className="min-w-0 truncate text-[14px] font-semibold">{name}</span>
        {state && <StatePill tone={state.tone} label={state.label} />}
        {position && <span className="text-xs text-lw-text-2">step {position.number} of {position.total} · {position.title}</span>}
        <span className="ml-auto flex items-center gap-1">
          <Link to={`/workflows/runs/${encodeURIComponent(runId)}`} className="flex h-7 items-center gap-1 rounded-md px-2 text-xs text-lw-link hover:underline">
            Open run page<ExternalLink size={12} aria-hidden="true" />
          </Link>
          <button ref={close} type="button" aria-label="Close the run graph" onClick={onClose} className="flex h-7 w-7 items-center justify-center rounded text-lw-text-2 hover:text-lw-text">
            <X size={14} aria-hidden="true" />
          </button>
        </span>
      </div>
      <div className="relative min-h-0 flex-1">
        {graph
          ? <LazyWorkflowGraph graph={graph} extras={extras} run selectedId={selected} onSelect={setSelected} centerOn={centerOn} direction={phone ? "TB" : "LR"} className="min-h-0" />
          : loading
          ? <LoadingState label="Reading the run…" size="inline" />
          : <p role="alert" className="p-4 text-lw-error">{error || "This run has no graph."}</p>}
      </div>
    </section>
  )
}
