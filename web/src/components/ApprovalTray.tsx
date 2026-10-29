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
      className="fixed inset-x-2 bottom-[calc(3.5rem+env(safe-area-inset-bottom)+0.5rem)] z-40 flex max-h-[85dvh] flex-col font-lw text-lw-text md:inset-x-auto md:bottom-4 md:right-4 md:max-h-[70dvh] md:w-[400px]"
    >
      {folded ? (
        <button
          onClick={() => setFolded(false)}
          className="ml-auto flex min-h-12 items-center gap-2 rounded-full border border-lw-needs/50 bg-lw-surface px-5 text-base font-medium text-lw-needs shadow-xl md:min-h-0 md:px-3 md:py-1.5 md:text-sm"
        >
          <span aria-hidden>◆</span>{count}
        </button>
      ) : (
        <div className="flex min-h-0 flex-col overflow-hidden rounded-xl border border-lw-line-strong bg-lw-surface shadow-2xl">
          <header className="flex items-center gap-2 border-b border-lw-line px-4 py-2 md:px-3">
            <span aria-hidden className="text-lw-needs">◆</span>
            <h2 className="text-base font-semibold md:text-sm">{count}</h2>
            <button aria-label="Hide approvals" onClick={() => setFolded(true)} className="ml-auto flex h-11 w-11 items-center justify-center rounded-lg text-xl text-lw-text-2 hover:bg-lw-raised md:h-auto md:w-auto md:px-2 md:py-0.5 md:text-base">
              ⌄
            </button>
          </header>
          <div className="min-h-0 space-y-3 overflow-y-auto p-2 md:space-y-2">
            {approvals.map((approval) => (
              <ApprovalCard key={approval.id} approval={approval} focused={approval.id === focused} onDone={refresh} />
            ))}
          </div>
        </div>
      )}
    </section>
  )
}

// A choice on a phone is a full-width button a thumb hits without aiming;
// from the md breakpoint it is the compact button of a desktop tray.
const choiceSize = "flex min-h-12 w-full items-center justify-center rounded-xl px-4 text-base md:min-h-0 md:w-auto md:rounded md:px-2.5 md:py-1 md:text-xs"

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
      className={`rounded-xl border bg-lw-raised p-4 md:rounded-lg md:p-3 ${focused ? "border-lw-accent ring-1 ring-lw-accent-ring" : "border-lw-line"}`}
    >
      <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-lw-needs md:text-[11px]">
        <span>{approvalKind(approval)}</span>
        {approval.blocking && <span className="text-lw-faint">· waiting</span>}
        {approval.task_id && (
          <Link to={`/agents?task=${encodeURIComponent(approval.task_id)}`} className="-my-2 ml-auto flex min-h-11 items-center px-1 text-sm normal-case tracking-normal text-lw-link hover:underline md:my-0 md:min-h-0 md:px-0 md:text-[11px]">
            Open agent
          </Link>
        )}
      </div>
      <h3 className="mt-1 break-words text-lg font-semibold leading-snug md:text-sm">{approval.title}</h3>
      {asker && <p className="text-sm text-lw-text-2 md:text-xs">{asker}</p>}

      {approval.source === "credential" && <CredentialScope approval={approval} />}

      {questions.length > 0 ? (
        <div className="-mx-4 mt-1">
          <NativeQuestionCard
            question={{ requestId: approval.id, questions, status: busy ? "submitting" : "pending", error: error ?? undefined }}
            touch
            askedBy={details.agent_kind || "The agent"}
            onAnswer={(_, answers) => decide({ decision: "answer", answers })}
            onDismiss={() => run(() => dismissApproval(baseUrl, approval.id))}
          />
        </div>
      ) : (
        <>
          {approval.source !== "credential" && (details.command || approval.summary) && !details.plan && (
            <pre className="mt-3 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-lw-ground p-3 font-lw-mono text-sm md:mt-2 md:max-h-40 md:rounded md:p-2 md:text-xs">{details.command || approval.summary}</pre>
          )}
          {details.plan && (
            <details className="mt-3 text-sm md:mt-2 md:text-xs">
              <summary className="flex min-h-11 cursor-pointer items-center text-lw-text-2 md:min-h-0">Show the plan</summary>
              <pre className="mt-1 max-h-60 overflow-auto whitespace-pre-wrap break-words rounded bg-lw-ground p-2 font-lw-mono">{details.plan}</pre>
            </details>
          )}
          {details.reason && approval.source !== "credential" && <p className="mt-2 break-words text-sm text-lw-text-2 md:mt-1 md:text-xs">{details.reason}</p>}
          {details.cwd && <p className="mt-1 truncate font-lw-mono text-xs text-lw-faint md:text-[11px]">{details.cwd}</p>}

          {error && <p role="alert" className="mt-2 text-sm text-lw-error md:text-xs">{error}</p>}
          {denying ? (
            <div className="mt-3 space-y-2 md:mt-2">
              <textarea
                aria-label="Note for the agent"
                value={note}
                onChange={(event) => setNote(event.target.value)}
                rows={3}
                placeholder="Tell the agent why, or what to do instead (optional)"
                className="w-full rounded-lg border border-lw-line bg-lw-ground px-3 py-2 text-base placeholder:text-lw-faint focus:border-lw-accent focus:outline-none md:rounded md:px-2 md:py-1 md:text-xs"
              />
              <div className="flex flex-col gap-2 md:flex-row">
                <button disabled={busy} onClick={() => decide({ decision: "deny", feedback: note.trim() })} className={`${choiceSize} border border-lw-danger-line font-medium text-lw-danger hover:bg-lw-error-soft disabled:opacity-50`}>
                  Deny
                </button>
                <button disabled={busy} onClick={() => setDenying(false)} className={`${choiceSize} text-lw-text-2 hover:bg-lw-selected`}>Back</button>
              </div>
            </div>
          ) : (
            <div className="mt-4 flex flex-col gap-2 md:mt-2 md:flex-row md:flex-wrap">
              {allows.map((option, index) => (
                <button
                  key={option.id}
                  disabled={busy}
                  title={option.description}
                  onClick={() => decide({ decision: "allow", option: option.id })}
                  className={index === 0
                    ? `${choiceSize} bg-lw-accent font-semibold text-white hover:brightness-110 disabled:opacity-50`
                    : `${choiceSize} border border-lw-line-strong font-medium text-lw-text hover:bg-lw-selected disabled:opacity-50`}
                >
                  {option.label}
                </button>
              ))}
              <button disabled={busy} onClick={() => setDenying(true)} className={`${choiceSize} border border-lw-line text-lw-text-2 hover:bg-lw-selected disabled:opacity-50 md:border-transparent`}>
                Deny…
              </button>
            </div>
          )}
        </>
      )}
    </article>
  )
}

// What a credential request asks for: the operations, each resource (a
// pattern marked broader), and the agent's reason in its own words.
function CredentialScope({ approval }: { approval: Approval }) {
  const details = approval.details
  const resources = details.resources ?? []
  return (
    <div className="mt-3 space-y-2 text-sm md:mt-2 md:space-y-1.5 md:text-xs">
      <div className="flex flex-wrap items-center gap-1.5">
        {(details.operations ?? []).map((operation) => (
          <span key={operation} className="rounded bg-lw-accent-soft px-1.5 py-0.5 font-lw-mono text-lw-accent-text">{operation}</span>
        ))}
        {details.broader && <span className="rounded bg-lw-needs-soft px-1.5 py-0.5 font-medium text-lw-needs">Broader scope</span>}
        {details.signed && <span className="text-lw-faint">needs a signed decision (not enforced yet)</span>}
      </div>
      {resources.length > 0 ? (
        <ul className="space-y-0.5">
          {resources.map((resource) => (
            <li key={resource} className="break-all rounded-lg bg-lw-ground px-3 py-2 font-lw-mono md:rounded md:px-2 md:py-1">{resource}</li>
          ))}
        </ul>
      ) : (
        <p className="text-lw-text-2">All of {details.credential}</p>
      )}
      {details.reason && (
        <figure>
          <figcaption className="text-xs text-lw-faint md:text-[11px]">The agent's reason</figcaption>
          <blockquote className="break-words text-lw-text-2">“{details.reason}”</blockquote>
        </figure>
      )}
    </div>
  )
}
