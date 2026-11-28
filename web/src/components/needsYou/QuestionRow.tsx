import { useState } from "react"
import { Link } from "react-router-dom"
import { useServer } from "../../hooks/useServer"
import { approvalAsker, dismissApproval, type Approval } from "../../lib/approvals"
import { setFocusMode } from "../../lib/focusMode"
import { shortAge } from "../../lib/needsYou"
import { approvalQuestions, recommended } from "../../lib/questionDrafts"
import { useApprovalDecision, useScrollIntoViewWhen } from "../approvals/useApprovalCard"
import { button, KindTag, quiet } from "./NeedsYouRow"

export interface QuestionRowProps {
  approval: Approval
  onDone: () => Promise<void>
  // The agent's in view: outlined and labelled "this agent".
  here?: boolean
  // The approval the page was asked to show (?approval=).
  focused?: boolean
}

// QuestionRow is an agent's question (its tool's own question prompt) in the
// needs-you rail, drawn as compact as an operator item: one button for each
// option of a single question, Reply… for an answer in the operator's own
// words, Open agent, and Focus, which opens focus mode at this question.
// Several questions, or one with several choices, are answered in focus
// mode (Answer…).
export function QuestionRow({ approval, onDone, here, focused = false }: QuestionRowProps) {
  const { baseUrl } = useServer()
  const { busy, error, run, decide } = useApprovalDecision(baseUrl, approval, onDone)
  const ref = useScrollIntoViewWhen(focused)
  const [replying, setReplying] = useState(false)
  const [text, setText] = useState("")
  const questions = approvalQuestions(approval)
  // One question with one choice is answered on the row.
  const single = questions.length === 1 && !questions[0].multiSelect ? questions[0] : null
  const asker = approvalAsker(approval)
  const focus = () => setFocusMode(true, approval.id)
  const answer = (label: string) => (single ? decide({ decision: "answer", answers: { [single.id]: [label] } }) : Promise.resolve(false))
  // The typed reply stays until it is sent, so a failed send can be retried.
  const send = async () => {
    if (!text.trim()) return
    if (await answer(text.trim())) {
      setText("")
      setReplying(false)
    }
  }
  const shown = single && single.question !== approval.title ? single.question : ""
  return (
    <article ref={ref} aria-label={approval.title} data-here={here ? "true" : undefined} data-focused={focused ? "true" : undefined}
      className={`flex flex-col gap-2 rounded-[10px] border p-3 ${here || focused ? "border-lw-accent-ring bg-lw-accent-soft/40" : "border-lw-line bg-lw-surface"}`}>
      <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-[11px] text-lw-text-2">
        <KindTag item={{ signal_kind: "operator_question" }} />
        {approval.task_id && <span className="font-lw-mono text-lw-text">{approval.task_id}</span>}
        {here && <span className="text-lw-accent-text">· this agent</span>}
        <span>· {shortAge(approval.created_at)}</span>
      </div>
      <p className="m-0 break-words text-[13px] font-semibold leading-snug text-lw-text">{approval.title}</p>
      {shown && <p className="m-0 break-words text-xs leading-snug text-lw-text">{shown}</p>}
      {asker && <p className="m-0 text-xs leading-snug text-lw-text-2">{asker} asks</p>}
      {!single && (
        <p className="m-0 text-xs text-lw-text-2">{questions.length > 1 ? `${questions.length} questions` : "Choose any"}</p>
      )}
      {error && <p role="alert" className="m-0 text-xs text-lw-error">{error}</p>}
      <div className="flex flex-wrap items-center gap-1.5">
        {single?.options.map((option) => {
          const { text: label, recommended: suggested } = recommended(option.label)
          return (
            <button key={option.label} type="button" disabled={busy} title={option.description} aria-label={label} className={button} onClick={() => void answer(option.label)}>
              {label}
              {suggested && <span className="rounded bg-lw-accent-soft px-1 text-[10px] text-lw-accent-text">Recommended</span>}
            </button>
          )
        })}
        {!single && <button type="button" disabled={busy} className={`${button} border-lw-accent bg-lw-accent text-white hover:bg-lw-accent`} onClick={focus}>Answer…</button>}
        {single && single.allowOther !== false && !replying && <button type="button" className={quiet} disabled={busy} onClick={() => setReplying(true)}>Reply…</button>}
        {approval.task_id && <Link to={`/agents?task=${encodeURIComponent(approval.task_id)}`} className={button}>Open agent</Link>}
        <button type="button" className={button} title="Answer in focus mode, starting here" onClick={focus}>Focus</button>
        <span className="flex-1" />
        <button type="button" className={quiet} disabled={busy} onClick={() => void run(() => dismissApproval(baseUrl, approval.id))}>Dismiss</button>
      </div>
      {replying && (
        <form className="flex items-center gap-1.5" onSubmit={(event) => { event.preventDefault(); void send() }}>
          <input aria-label="Reply" autoFocus value={text} onChange={(event) => setText(event.target.value)} placeholder="Your answer"
            className="min-h-8 min-w-0 flex-1 rounded-md border border-lw-line-strong/40 bg-lw-rail px-2 text-xs text-lw-text" />
          <button type="submit" className={button} disabled={busy || !text.trim()}>Send</button>
        </form>
      )}
    </article>
  )
}
