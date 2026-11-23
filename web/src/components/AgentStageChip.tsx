import { useEffect, useRef, useState } from "react"
import { useServer } from "../hooks/useServer"
import { cn } from "../lib/utils"
import { fetchStageHistory, setAgentStage, stageLabel, stageSourceLabel, workStages, type StageEvent, type WorkStage } from "../lib/agentStage"
import { StageTrack } from "./StageTrack"

// An agent's stage as its chat header shows it.
export interface AgentStageInfo {
  agentId: string
  stage?: string
  reason?: string
  source?: string
}

function eventTime(at: string | undefined): string {
  if (!at) return ""
  const date = new Date(at)
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

// AgentStageChip is the agent's stage beside its state: the track, the stage
// and why, and "set by you" when the operator set it. It opens the stage
// panel: the stage history and the stages the operator may set.
export function AgentStageChip({ info, open, onOpenChange }: { info: AgentStageInfo; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { baseUrl } = useServer()
  const box = useRef<HTMLDivElement>(null)
  const [history, setHistory] = useState<StageEvent[] | null>(null)
  const [error, setError] = useState("")
  const label = stageLabel(info.stage)
  const by = stageSourceLabel(info.source)
  const why = [info.reason, by].filter(Boolean).join(" · ")

  useEffect(() => {
    if (!open) return
    let live = true
    fetchStageHistory(baseUrl, info.agentId)
      .then((events) => { if (live) setHistory(events) })
      .catch((err: Error) => { if (live) setError(err.message) })
    return () => { live = false }
  }, [open, baseUrl, info.agentId, info.stage, info.reason])

  useEffect(() => {
    if (!open) return
    const close = (event: Event) => {
      if (event instanceof KeyboardEvent ? event.key === "Escape" : !box.current?.contains(event.target as Node)) onOpenChange(false)
    }
    document.addEventListener("mousedown", close)
    document.addEventListener("keydown", close)
    return () => {
      document.removeEventListener("mousedown", close)
      document.removeEventListener("keydown", close)
    }
  }, [open, onOpenChange])

  const choose = (stage: WorkStage) => {
    setError("")
    setAgentStage(baseUrl, info.agentId, stage).catch((err: Error) => setError(err.message))
  }

  return (
    <div ref={box} className="relative min-w-0 shrink">
      <button
        type="button"
        aria-label={`Stage: ${label || "none"}${why ? `, ${why}` : ""}`}
        aria-expanded={open}
        onClick={() => onOpenChange(!open)}
        className="flex h-6 min-w-0 max-w-[22rem] items-center gap-1.5 rounded-full bg-lw-raised px-2.5 text-xs text-lw-text hover:bg-lw-selected"
      >
        <StageTrack stage={info.stage} label="" />
        <span className={cn("shrink-0", info.stage === "finished" && "text-lw-done")}>{label || "No stage"}</span>
        {why && <span className="min-w-0 truncate text-lw-text-2">· {why}</span>}
      </button>
      {open && (
        <div role="dialog" aria-label="Stage" className="absolute left-0 top-full z-30 mt-1 w-72 max-w-[calc(100vw-2rem)] rounded-md border border-lw-line-strong bg-lw-surface p-3 text-xs shadow-xl">
          <h3 className="m-0 mb-2 text-[11px] font-semibold uppercase tracking-[1px] text-lw-text-2">Stage history</h3>
          {history === null && !error && <p className="m-0 text-lw-text-2">Loading…</p>}
          {history !== null && history.length === 0 && <p className="m-0 text-lw-text-2">No changes yet.</p>}
          {history !== null && history.length > 0 && (
            <ol className="m-0 flex max-h-48 list-none flex-col gap-2 overflow-y-auto p-0">
              {history.map((event, index) => (
                <li key={index} className="flex gap-2">
                  <span className="w-10 shrink-0 font-lw-mono text-lw-text-2">{eventTime(event.at)}</span>
                  <span className="flex min-w-0 flex-col">
                    <span className="flex items-center gap-1.5"><StageTrack stage={event.stage} label="" />{stageLabel(event.stage)}</span>
                    {(event.reason || stageSourceLabel(event.source)) && (
                      <span className="text-lw-text-2">{[event.reason, stageSourceLabel(event.source)].filter(Boolean).join(" · ")}</span>
                    )}
                  </span>
                </li>
              ))}
            </ol>
          )}
          <h3 className="m-0 mb-1.5 mt-3 text-[11px] font-semibold uppercase tracking-[1px] text-lw-text-2">Set stage</h3>
          <div role="radiogroup" aria-label="Set stage" className="flex flex-col">
            {workStages.map((stage) => (
              <button
                key={stage}
                type="button"
                role="radio"
                aria-checked={info.stage === stage}
                onClick={() => choose(stage)}
                className={cn("flex items-center gap-2 rounded px-1.5 py-1 text-left hover:bg-lw-raised", info.stage === stage ? "text-lw-link" : "text-lw-text")}
              >
                <StageTrack stage={stage} label="" />{stageLabel(stage)}
              </button>
            ))}
          </div>
          <p className="m-0 mt-2 text-lw-text-2">A stage you set shows “set by you” until the agent or a milestone moves it.</p>
          {error && <p role="alert" className="m-0 mt-2 text-lw-error">{error}</p>}
        </div>
      )}
    </div>
  )
}
