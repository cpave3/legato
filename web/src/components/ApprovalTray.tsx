import { useEffect, useRef, useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { useApprovals } from "../hooks/useApprovals"
import { useServer } from "../hooks/useServer"
import { approvalAsker, approvalKind, decideApproval, dismissApproval, type Approval, type ApprovalDecision } from "../lib/approvals"
import { NativeQuestionCard } from "./NativeQuestionCard"
import type { NativeQuestion } from "../lib/nativeAgentSync"

// The approvals waiting for the person, on every page: a column of cards at
// the bottom right (above the tab bar on a phone) while any is pending, or a
// count when the person folds it away. ?approval=<id> opens it on that one.
export function ApprovalTray() {
  const { data: approvals, refresh } = useApprovals()
  const [folded, setFolded] = useState(false)
  const [focused, setFocused] = useState<string | null>(null)
  const [params, setParams] = useSearchParams()
  const seen = useRef<Set<string>>(new Set())
  const wanted = params.get("approval")

  // An approval the person has not seen yet opens the tray again.
  useEffect(() => {
    const fresh = approvals.some((approval) => !seen.current.has(approval.id))
    approvals.forEach((approval) => seen.current.add(approval.id))
    if (fresh) setFolded(false)
  }, [approvals])

  useEffect(() => {
    if (!wanted) return
    setFocused(wanted)
    setFolded(false)
    const next = new URLSearchParams(params)
    next.delete("approval")
    setParams(next, { replace: true })
  }, [wanted, params, setParams])

  if (approvals.length === 0) return null
  const count = `${approvals.length} ${approvals.length === 1 ? "needs" : "need"} you`

  return (
    <section
      aria-label="Approvals"
      className="fixed inset-x-2 bottom-[calc(3.5rem+env(safe-area-inset-bottom)+0.5rem)] z-40 flex max-h-[70dvh] flex-col font-lw text-lw-text md:inset-x-auto md:bottom-4 md:right-4 md:w-[400px]"
    >
      {folded ? (
        <button
          onClick={() => setFolded(false)}
          className="ml-auto flex items-center gap-2 rounded-full border border-lw-needs/50 bg-lw-surface px-3 py-1.5 text-sm font-medium text-lw-needs shadow-xl"
        >
          <span aria-hidden>◆</span>{count}
        </button>
      ) : (
        <div className="flex min-h-0 flex-col overflow-hidden rounded-xl border border-lw-line-strong bg-lw-surface shadow-2xl">
          <header className="flex items-center gap-2 border-b border-lw-line px-3 py-2">
            <span aria-hidden className="text-lw-needs">◆</span>
            <h2 className="text-sm font-semibold">{count}</h2>
            <button aria-label="Hide approvals" onClick={() => setFolded(true)} className="ml-auto rounded px-2 py-0.5 text-lw-text-2 hover:bg-lw-raised">
              ⌄
            </button>
          </header>
          <div className="min-h-0 space-y-2 overflow-y-auto p-2">
            {approvals.map((approval) => (
              <ApprovalCard key={approval.id} approval={approval} focused={approval.id === focused} onDone={refresh} />
            ))}
          </div>
        </div>
      )}
    </section>
  )
}

function ApprovalCard({ approval, focused, onDone }: { approval: Approval; focused: boolean; onDone: () => Promise<void> }) {
  const { baseUrl } = useServer()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [denying, setDenying] = useState(false)
  const [note, setNote] = useState("")
  const ref = useRef<HTMLElement>(null)

  useEffect(() => {
    if (focused) ref.current?.scrollIntoView?.({ block: "nearest" })
  }, [focused])

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      await onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }
  const decide = (decision: ApprovalDecision) => run(() => decideApproval(baseUrl, approval.id, decision))
  const asker = approvalAsker(approval)
  const details = approval.details
  const allows = approval.options.filter((option) => option.kind === "allow")
  const questions = (details.questions ?? []) as NativeQuestion[]

  return (
    <article
      ref={ref}
      data-focused={focused ? "true" : undefined}
      className={`rounded-lg border bg-lw-raised p-3 ${focused ? "border-lw-accent ring-1 ring-lw-accent-ring" : "border-lw-line"}`}
    >
      <div className="flex items-center gap-2 text-[11px] font-medium uppercase tracking-wide text-lw-needs">
        <span>{approvalKind(approval)}</span>
        {approval.blocking && <span className="text-lw-faint">· waiting</span>}
        {approval.task_id && (
          <Link to={`/agents?task=${encodeURIComponent(approval.task_id)}`} className="ml-auto normal-case tracking-normal text-lw-link hover:underline">
            Open agent
          </Link>
        )}
      </div>
      <h3 className="mt-1 break-words text-sm font-semibold">{approval.title}</h3>
      {asker && <p className="text-xs text-lw-text-2">{asker}</p>}

      {questions.length > 0 ? (
        <div className="-mx-4 mt-1">
          <NativeQuestionCard
            question={{ requestId: approval.id, questions, status: busy ? "submitting" : "pending", error: error ?? undefined }}
            askedBy={details.agent_kind || "The agent"}
            onAnswer={(_, answers) => decide({ decision: "answer", answers })}
            onDismiss={() => run(() => dismissApproval(baseUrl, approval.id))}
          />
        </div>
      ) : (
        <>
          {(details.command || approval.summary) && !details.plan && (
            <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded bg-lw-ground p-2 font-lw-mono text-xs">{details.command || approval.summary}</pre>
          )}
          {details.plan && (
            <details className="mt-2 text-xs">
              <summary className="cursor-pointer text-lw-text-2">Show the plan</summary>
              <pre className="mt-1 max-h-60 overflow-auto whitespace-pre-wrap break-words rounded bg-lw-ground p-2 font-lw-mono">{details.plan}</pre>
            </details>
          )}
          {details.reason && <p className="mt-1 break-words text-xs text-lw-text-2">{details.reason}</p>}
          {details.cwd && <p className="mt-1 truncate font-lw-mono text-[11px] text-lw-faint">{details.cwd}</p>}

          {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
          {denying ? (
            <div className="mt-2 space-y-2">
              <textarea
                aria-label="Note for the agent"
                value={note}
                onChange={(event) => setNote(event.target.value)}
                rows={2}
                placeholder="Tell the agent why, or what to do instead (optional)"
                className="w-full rounded border border-lw-line bg-lw-ground px-2 py-1 text-xs placeholder:text-lw-faint focus:border-lw-accent focus:outline-none"
              />
              <div className="flex gap-2">
                <button disabled={busy} onClick={() => decide({ decision: "deny", feedback: note.trim() })} className="rounded border border-lw-danger-line px-2.5 py-1 text-xs font-medium text-lw-danger hover:bg-lw-error-soft disabled:opacity-50">
                  Deny
                </button>
                <button disabled={busy} onClick={() => setDenying(false)} className="rounded px-2.5 py-1 text-xs text-lw-text-2 hover:bg-lw-selected">Back</button>
              </div>
            </div>
          ) : (
            <div className="mt-2 flex flex-wrap gap-2">
              {allows.map((option, index) => (
                <button
                  key={option.id}
                  disabled={busy}
                  title={option.description}
                  onClick={() => decide({ decision: "allow", option: option.id })}
                  className={index === 0
                    ? "rounded bg-lw-accent px-2.5 py-1 text-xs font-medium text-white hover:brightness-110 disabled:opacity-50"
                    : "rounded border border-lw-line-strong px-2.5 py-1 text-xs text-lw-text hover:bg-lw-selected disabled:opacity-50"}
                >
                  {option.label}
                </button>
              ))}
              <button disabled={busy} onClick={() => setDenying(true)} className="rounded px-2.5 py-1 text-xs text-lw-text-2 hover:bg-lw-selected disabled:opacity-50">
                Deny…
              </button>
            </div>
          )}
        </>
      )}
    </article>
  )
}
