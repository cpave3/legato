import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { useModalLayer } from "../ui/useModalLayer"
import { useApprovals } from "../../hooks/useApprovals"
import type { PeerApproval } from "../../lib/reeve"
import { useServer } from "../../hooks/useServer"
import type { Approval } from "../../lib/approvals"
import { usePasskeyDevice } from "../../hooks/usePasskeyDevice"
import { QuestionFocus } from "./QuestionFocus"
import { approvalQuestions } from "../../lib/questionDrafts"
import { setFocusMode, useFocusMode } from "../../lib/focusMode"
import { ApprovalDetails, ApprovalHeader, QuestionsBody, SignedChoices, SourceBody } from "./ApprovalCardParts"
import { choiceSize, choiceTone } from "./choiceStyles"
import { useApprovalDecision, useDenyNote, useScrollIntoViewWhen } from "./useApprovalCard"

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

// ApprovalFocus is focus mode: agents' questions answered one at a time over
// the whole screen (QuestionFocus), opened from the rail's header or the
// phone's tray. It stays on until the person leaves it: with no question
// left it fades out and waits, and the next question to arrive opens it
// again. Mounted once by the layout.
export function ApprovalFocus() {
  const { data: approvals, refresh } = useApprovals()
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
  const leaveFocus = () => setFocusMode(false)
  const focusRef = useRef<HTMLDivElement>(null)
  useModalLayer({ open: focusOpen && fade.mounted, onClose: leaveFocus, containerRef: focusRef })
  return (
    <>
      {fade.mounted && createPortal(
        <div
          ref={focusRef}
          tabIndex={-1}
          inert={!focusOpen}
          aria-hidden={!focusOpen}
          className={`fixed inset-0 z-50 transition-[opacity,transform] duration-300 ease-out motion-reduce:transition-none ${fade.visible ? "scale-100 opacity-100" : "pointer-events-none scale-[0.985] opacity-0"}`}
        >
          <QuestionFocus approvals={asking.length > 0 ? asking : lastAsking} waiting={approvals.length} onLeave={leaveFocus} onDone={refresh} />
        </div>,
        document.body,
      )}
    </>
  )
}

// FocusButton turns focus mode on: agents' questions one at a time.
export function FocusButton({ className = "" }: { className?: string }) {
  return (
    <button
      aria-label="Answer in focus mode"
      title="Focus mode: one question at a time"
      onClick={() => setFocusMode(true)}
      className={`${className} flex h-11 w-11 items-center justify-center rounded-lg border border-lw-accent-ring bg-lw-accent-soft text-lw-accent-text hover:brightness-125 md:h-7 md:w-7`}
    >
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M4 9V4h5" /><path d="M20 9V4h-5" /><path d="M4 15v5h5" /><path d="M20 15v5h-5" />
      </svg>
    </button>
  )
}

// Another instance's Reeve change set: marked with its instance, and decided
// there, with a device enrolled there.
export function PeerApprovalCard({ approval }: { approval: PeerApproval }) {
  const link = "inline-flex min-h-11 items-center justify-center rounded-lg px-3 text-sm md:min-h-0 md:py-1 md:text-xs"
  return (
    <article data-testid={`peer-approval-${approval.id}`} className="rounded-lg border border-lw-line border-l-2 border-l-[var(--theme-keyword,#8b5cf6)] bg-lw-ground p-3">
      <div className="flex items-center gap-2 text-xs text-lw-text-2">
        <span className="rounded-full border border-lw-line-strong px-2 py-0.5 text-lw-text">{approval.instance}</span>
        <span>Reeve change set</span>
      </div>
      <h3 className="mt-1 text-sm font-medium text-lw-text">{approval.title}</h3>
      {approval.summary && <p className="text-xs text-lw-text-2">{approval.summary}</p>}
      <div className="mt-2 flex flex-wrap gap-2">
        <a href={approval.url} target="_blank" rel="noreferrer" className={`${link} border border-lw-accent-ring bg-lw-accent-soft text-lw-accent-text hover:brightness-125`}>
          {approval.signed ? `◈ Approve on ${approval.instance}` : `Approve on ${approval.instance}`}
        </a>
        <a href={approval.instance_url} target="_blank" rel="noreferrer" className={`${link} text-lw-text-2 hover:bg-lw-raised`}>
          Open {approval.instance} ↗
        </a>
      </div>
    </article>
  )
}

export function ApprovalCard({ approval, focused, onDone }: { approval: Approval; focused: boolean; onDone: () => Promise<void> }) {
  const { baseUrl } = useServer()
  const decision = useApprovalDecision(baseUrl, approval, onDone)
  const { denying, setDenying, note, setNote, noteRef, denyRef } = useDenyNote()
  const ref = useScrollIntoViewWhen(focused)
  // Outside the app, a passkey made on this page can sign.
  const passkey = usePasskeyDevice()
  const { busy, error, decide } = decision
  const allows = approval.options.filter((option) => option.kind === "allow")
  const questions = approvalQuestions(approval)

  return (
    <article
      ref={ref}
      data-focused={focused ? "true" : undefined}
      className={`rounded-xl border bg-lw-raised p-4 md:rounded-lg md:p-3 ${focused ? "border-lw-accent ring-1 ring-lw-accent-ring" : "border-lw-line"}`}
    >
      <ApprovalHeader approval={approval} />
      <SourceBody approval={approval} baseUrl={baseUrl} onDone={onDone} />

      {questions.length > 0 ? (
        <QuestionsBody approval={approval} questions={questions} baseUrl={baseUrl} decision={decision} />
      ) : (
        <>
          <ApprovalDetails approval={approval} />

          {error && <p role="alert" className="mt-2 text-sm text-lw-error md:text-xs">{error}</p>}
          {denying ? (
            // The note a denial carries, with Deny and Back.
            <div className="mt-3 space-y-2 md:mt-2">
              <textarea
                ref={noteRef}
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
              {approval.signed && <SignedChoices approval={approval} allows={allows} baseUrl={baseUrl} passkey={passkey} decision={decision} />}
              {/* An unsigned approval's allow options, the first the one to take. */}
              {!approval.signed && approval.source !== "reeve_secret" && allows.map((option, index) => (
                <button
                  key={option.id}
                  disabled={busy}
                  title={option.description}
                  onClick={() => decide({ decision: "allow", option: option.id })}
                  className={`${choiceSize} ${choiceTone(index)}`}
                >
                  {option.label}
                </button>
              ))}
              <button ref={denyRef} disabled={busy} onClick={() => setDenying(true)} className={`${choiceSize} border border-lw-line text-lw-text-2 hover:bg-lw-selected disabled:opacity-50 md:border-transparent`}>
                Deny…
              </button>
            </div>
          )}
        </>
      )}
    </article>
  )
}
