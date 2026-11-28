import { useEffect, useRef, useState } from "react"
import { Link } from "react-router-dom"
import { useServer } from "../../hooks/useServer"
import type { Approval } from "../../lib/approvals"
import { approvalQuestions, type QuestionDraft } from "../../lib/questionDrafts"
import { answersOf, chooseOption, draftIn, nextLabel, withDraft, type FocusDrafts, type FocusStep } from "../../lib/questionFocus"
import { FocusActions, keyCap, QuestionStep, ReviewRow } from "./QuestionFocusParts"
import { useFocusAnswering, useFocusKeys, type FocusState } from "./useQuestionFocus"

const FIRST: FocusStep = { screen: "question", q: 0 }

const LEGEND = [["1–9", "choose"], ["↑ ↓", "move"], ["↵", "next"], ["←", "back"], ["/", "type your own"], ["⌘↵", "send"], ["Esc", "leave focus"]]

type Props = {
  // The approvals that ask questions, the first answered first.
  approvals: Approval[]
  // How many approvals need the person in all, questions or not.
  waiting: number
  onLeave: () => void
  onDone: () => Promise<void>
}

// Focus mode: the questions agents ask, one at a time, in the middle of the
// screen with nothing else on it. On a computer every step has a key (the
// legend at the foot); on a phone it fills the screen, with Back and Next
// where a thumb is. One approval's questions are reviewed and sent together;
// an approval with one question sends from that question.
export function QuestionFocus({ approvals, waiting, onLeave, onDone }: Props) {
  const current = approvals[0]
  const currentId = current?.id
  // Where focus mode stands, every approval's drafts, and the dialog and the
  // own-answer box it moves the keyboard between.
  const [step, setStep] = useState<FocusStep>(FIRST)
  const [drafts, setDrafts] = useState<FocusDrafts>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dialog = useRef<HTMLDivElement>(null)
  const own = useRef<HTMLInputElement>(null)

  // A new approval starts at its first question.
  useEffect(() => {
    setStep(FIRST)
    setError(null)
  }, [currentId])

  // Keys go to the dialog whenever the person is not typing.
  useEffect(() => {
    if (document.activeElement !== own.current) dialog.current?.focus()
  }, [currentId, step.q, step.screen])

  if (!current) return null
  const state = { step, setStep, drafts, setDrafts, busy, setBusy, error, setError, dialog, own }
  return <FocusDialog current={current} asking={approvals.length} waiting={waiting} state={state} onLeave={onLeave} onDone={onDone} />
}

interface FocusDialogProps {
  current: Approval
  asking: number
  waiting: number
  state: FocusState
  onLeave: () => void
  onDone: () => Promise<void>
}

function FocusDialog({ current, asking, waiting, state, onLeave, onDone }: FocusDialogProps) {
  const { baseUrl } = useServer()
  const { step, setStep, drafts, busy, error, dialog, own } = state
  const questions = approvalQuestions(current)
  const question = questions[Math.min(step.q, questions.length - 1)]
  const draft = draftIn(drafts, current.id, question.id)
  const answers = answersOf(questions, drafts, current.id)
  const { send, next, back, dismiss } = useFocusAnswering({ baseUrl, current, questions, answers, state, onDone })
  // The question on screen is answered by an option chosen by index, or in
  // the person's own words.
  const setDraft = (update: (draft: QuestionDraft) => QuestionDraft) =>
    state.setDrafts((all) => withDraft(all, current.id, question.id, update))
  const choose = (index: number) => {
    const option = question.options[index]
    if (option) setDraft((d) => chooseOption(question, d, option.label))
  }
  const type = (other: string) => setDraft((d) => ({ ...d, other }))
  const onKeyDown = useFocusKeys({ question, draft, state, actions: { send, next, back, choose }, onLeave })
  const agent = current.details.agent_kind || "The agent"

  return (
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-label="Focus mode"
      tabIndex={-1}
      onKeyDown={onKeyDown}
      // index.css keeps a dialog inside a phone's screen; this one is the screen.
      style={{ maxWidth: "none", maxHeight: "none" }}
      className="flex h-full flex-col bg-lw-rail font-lw text-lw-text outline-none"
    >
      <header className="flex items-center gap-3 border-b border-lw-surface px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)] md:px-7 md:py-4">
        <span aria-hidden className="text-lw-needs">◆</span>
        <span className="font-semibold">{waiting} {waiting === 1 ? "needs" : "need"} you</span>
        <span className="rounded-full border border-lw-accent-ring bg-lw-accent-soft px-2.5 py-0.5 font-lw-mono text-[11px] uppercase tracking-widest text-lw-accent-text">Focus</span>
        <span className="ml-auto hidden font-lw-mono text-xs text-lw-faint md:inline">
          {asking === 1 ? "1 approval asks" : `${asking} approvals ask`}
        </span>
        <button
          onClick={onLeave}
          className="ml-auto flex min-h-11 items-center gap-2 rounded-lg border border-lw-line-strong bg-lw-surface px-3 text-sm hover:bg-lw-raised md:ml-0 md:min-h-9"
        >
          Leave focus <kbd aria-hidden className={`hidden md:inline ${keyCap}`}>Esc</kbd>
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex min-h-full w-full max-w-[760px] flex-col px-4 pt-6 md:pb-10 md:pt-14">
          <div className="flex flex-wrap items-baseline gap-x-2.5 gap-y-1 text-sm">
            <span className="font-semibold text-sky-200">{agent}</span>
            <span aria-hidden className="text-lw-faint">·</span>
            <span className="min-w-0 flex-1 text-lw-text-2">{current.details.task_title || current.title}</span>
            {current.task_id && (
              <Link to={`/agents?task=${encodeURIComponent(current.task_id)}`} onClick={onLeave} className="text-lw-link hover:underline">Open agent</Link>
            )}
          </div>

          {step.screen === "question" ? (
            <QuestionStep questions={questions} question={question} q={step.q} draft={draft} answers={answers} ownRef={own} onChoose={choose} onType={type} />
          ) : (
            // The review of one approval's answers before they are sent together.
            <section aria-label="Review your answers" className="flex flex-col">
              <h1 className="mt-6 text-xl font-semibold md:mt-7 md:text-[23px]">Review your answers</h1>
              <p className="mt-1.5 text-sm text-lw-text-2">{agent} gets them all at once, when you send.</p>
              <ol className="mt-6 flex flex-col gap-2.5">
                {questions.map((one, i) => <ReviewRow key={one.id} one={one} answer={answers[one.id]} onChange={() => setStep({ screen: "question", q: i })} />)}
              </ol>
            </section>
          )}

          {error && <p role="alert" className="mt-4 text-sm text-lw-error">{error}</p>}

          <FocusActions step={step} busy={busy} label={nextLabel(step, questions.length, agent)} onBack={back} onDismiss={() => void dismiss()} onNext={next} />
        </div>
      </div>

      <footer aria-label="Keys" className="hidden flex-wrap justify-center gap-x-5 gap-y-2 border-t border-lw-surface px-6 py-4 text-xs text-lw-faint md:flex">
        {LEGEND.map(([keys, does]) => (
          <span key={keys} className="flex items-center gap-1.5"><kbd className={keyCap}>{keys}</kbd>{does}</span>
        ))}
      </footer>
    </div>
  )
}
