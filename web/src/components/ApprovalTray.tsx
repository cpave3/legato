import { ChangeSetCard } from "./ChangeSetCard"
import { SecretRequestForm } from "./SecretRequestForm"
import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { Link, useSearchParams } from "react-router-dom"
import { useApprovals } from "../hooks/useApprovals"
import { useServer } from "../hooks/useServer"
import { approvalAsker, approvalKind, decideApproval, dismissApproval, type Approval, type ApprovalDecision } from "../lib/approvals"
import { NativeQuestionCard } from "./NativeQuestionCard"
import { onShellApproval, shellApprovals } from "../lib/mobileShell"
import { usePasskeyDevice } from "../hooks/usePasskeyDevice"
import { signWithPasskey } from "../lib/passkeys"
import type { NativeQuestion } from "../lib/nativeAgentSync"
import { QuestionFocus } from "./QuestionFocus"
import { approvalQuestions } from "../lib/questionDrafts"
import { setFocusMode, useFocusMode } from "../lib/focusMode"

// Shows something while open, and for a moment after, so it can fade in and
// out: mounted while it is on screen at all, visible while it is meant to be.
function useFade(open: boolean, ms = 300) {
  const [mounted, setMounted] = useState(open)
  const [visible, setVisible] = useState(false)
  useEffect(() => {
    if (open) {
      setMounted(true)
      const frame = requestAnimationFrame(() => setVisible(true))
      return () => cancelAnimationFrame(frame)
    }
    setVisible(false)
    const timer = setTimeout(() => setMounted(false), ms)
    return () => clearTimeout(timer)
  }, [open, ms])
  return { mounted, visible }
}

// The approvals waiting for the person, on every page: a column of cards at
// the bottom right (above the tab bar on a phone) while any is pending, or a
// count when the person folds it away. ?approval=<id> opens it on that one.
//
// Focus mode, from the tray's header, answers agents' questions one at a time
// over the whole screen (QuestionFocus). It stays on until the person leaves
// it: with no question left it fades out and waits, and the next question to
// arrive opens it again.
export function ApprovalTray() {
  const { data: approvals, refresh } = useApprovals()
  const [folded, setFolded] = useState(false)
  const focusMode = useFocusMode()
  const asking = approvals.filter((approval) => approvalQuestions(approval).length > 0)
  const focusOpen = focusMode && asking.length > 0
  const fade = useFade(focusOpen)
  // While focus fades out, it keeps showing what it showed.
  const [lastAsking, setLastAsking] = useState<Approval[]>([])
  const askingIds = asking.map((approval) => `${approval.id}:${approval.revision}`).join(",")
  useEffect(() => {
    if (asking.length > 0) setLastAsking((shown) => (shown.map((a) => `${a.id}:${a.revision}`).join(",") === askingIds ? shown : asking))
    // asking is rebuilt each render; its ids say when it changed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [askingIds])
  const leaveFocus = () => {
    setFocusMode(false)
    setFolded(false)
  }
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

  const count = `${approvals.length} ${approvals.length === 1 ? "needs" : "need"} you`
  const focus = (
    <>
      {fade.mounted && createPortal(
        <div
          aria-hidden={!focusOpen}
          className={`fixed inset-0 z-50 transition-[opacity,transform] duration-300 ease-out motion-reduce:transition-none ${fade.visible ? "scale-100 opacity-100" : "pointer-events-none scale-[0.985] opacity-0"}`}
        >
          <QuestionFocus approvals={asking.length > 0 ? asking : lastAsking} waiting={approvals.length} onLeave={leaveFocus} onDone={refresh} />
        </div>,
        document.body,
      )}
      {focusMode && !focusOpen && approvals.length === 0 && createPortal(
        <div
          role="status"
          className="fixed inset-x-2 top-[calc(env(safe-area-inset-top)+4.25rem)] z-40 flex items-center gap-3 rounded-xl border border-lw-accent-ring bg-lw-surface py-2 pl-4 pr-2 font-lw text-sm text-lw-text shadow-2xl md:inset-x-auto md:right-4 md:max-w-md"
        >
          <span aria-hidden className="h-2 w-2 shrink-0 rounded-full bg-lw-link" />
          <span className="min-w-0 flex-1">Focus is on. It opens again when a question comes in.</span>
          <button onClick={leaveFocus} className="min-h-11 shrink-0 rounded-lg border border-lw-line-strong px-3 hover:bg-lw-raised md:min-h-9">Leave focus</button>
        </div>,
        document.body,
      )}
    </>
  )
  if (approvals.length === 0) return focus

  return (
    <>
    {focus}
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
            {focusMode && !focusOpen && (
              <>
                <span className="ml-auto rounded-full border border-lw-accent-ring bg-lw-accent-soft px-2 py-0.5 text-xs text-lw-accent-text">Focus is on</span>
                <button onClick={leaveFocus} className="min-h-11 rounded-lg px-2 text-sm text-lw-text-2 hover:bg-lw-raised md:min-h-0 md:py-0.5 md:text-xs">Leave focus</button>
              </>
            )}
            {asking.length > 0 && (
              <button
                aria-label="Answer in focus mode"
                title="Focus mode: one question at a time"
                onClick={() => setFocusMode(true)}
                className="ml-auto flex h-11 w-11 items-center justify-center rounded-lg border border-lw-accent-ring bg-lw-accent-soft text-lw-accent-text hover:brightness-125 md:h-7 md:w-7"
              >
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M4 9V4h5" /><path d="M20 9V4h-5" /><path d="M4 15v5h5" /><path d="M20 15v5h-5" />
                </svg>
              </button>
            )}
            <button aria-label="Hide approvals" onClick={() => setFolded(true)} className={`${asking.length > 0 || focusMode ? "" : "ml-auto "}flex h-11 w-11 items-center justify-center rounded-lg text-xl text-lw-text-2 hover:bg-lw-raised md:h-auto md:w-auto md:px-2 md:py-0.5 md:text-base`}>
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
    </>
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

  // A signed approval is decided in the Legato app's own sheet; it says how
  // that went when the sheet closes.
  useEffect(() => onShellApproval((result) => {
    if (result.id !== approval.id) return
    setBusy(false)
    if (result.status === "decided") void onDone()
    if (result.status === "error") setError(result.error || "The phone could not sign this decision.")
  }), [approval.id, onDone])
  const phone = approval.signed ? shellApprovals() : null
  // Outside the app, a passkey made on this page can sign.
  const passkey = usePasskeyDevice()

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
      {approval.source === "reeve_change_set" && <ChangeSetCard approval={approval} />}
      {approval.source === "reeve_secret" && <SecretRequestForm approval={approval} baseUrl={baseUrl} onDone={onDone} />}

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
          {approval.source !== "credential" && approval.source !== "reeve_change_set" && approval.source !== "reeve_secret" && (details.command || approval.summary) && !details.plan && (
            <pre className="mt-3 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-lw-ground p-3 font-lw-mono text-sm md:mt-2 md:max-h-40 md:rounded md:p-2 md:text-xs">{details.command || approval.summary}</pre>
          )}
          {details.plan && (
            <details className="mt-3 text-sm md:mt-2 md:text-xs">
              <summary className="flex min-h-11 cursor-pointer items-center text-lw-text-2 md:min-h-0">Show the plan</summary>
              <pre className="mt-1 max-h-60 overflow-auto whitespace-pre-wrap break-words rounded bg-lw-ground p-2 font-lw-mono">{details.plan}</pre>
            </details>
          )}
          {details.reason && approval.source !== "credential" && approval.source !== "reeve_change_set" && <p className="mt-2 break-words text-sm text-lw-text-2 md:mt-1 md:text-xs">{details.reason}</p>}
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
              {approval.signed && phone && (
                <button
                  disabled={busy}
                  onClick={() => { setError(null); setBusy(true); phone.open(approval.id) }}
                  className={`${choiceSize} gap-2 bg-lw-accent font-semibold text-white hover:brightness-110 disabled:opacity-50`}
                >
                  <span aria-hidden>◈</span> Approve and sign
                </button>
              )}
              {approval.signed && !phone && passkey && allows.map((option, index) => (
                <button
                  key={option.id}
                  disabled={busy}
                  aria-label={`Sign with passkey: ${option.label}`}
                  onClick={() => run(() => signWithPasskey(baseUrl, approval.id, passkey, option.id))}
                  className={index === 0
                    ? `${choiceSize} gap-2 bg-lw-accent font-semibold text-white hover:brightness-110 disabled:opacity-50`
                    : `${choiceSize} gap-2 border border-lw-line-strong font-medium text-lw-text hover:bg-lw-selected disabled:opacity-50`}
                >
                  <span aria-hidden>◈</span> {option.label}
                </button>
              ))}
              {approval.signed && !phone && !passkey && (
                <p className="rounded-lg border border-lw-accent/40 bg-lw-accent-soft px-3 py-2 text-sm text-lw-accent-text md:text-xs">
                  <span aria-hidden>◈ </span>Needs your phone: open this approval in the Legato app and sign it with your fingerprint or PIN, or add a passkey for this computer in Settings › Devices.
                </p>
              )}
              {!approval.signed && approval.source !== "reeve_secret" && allows.map((option, index) => (
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
