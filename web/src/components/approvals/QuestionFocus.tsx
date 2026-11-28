import { useServer } from "../../hooks/useServer"
import type { Approval } from "../../lib/approvals"
import { approvalQuestions } from "../../lib/questionDrafts"
import { answersOf, draftIn, nextLabel } from "../../lib/questionFocus"
import { AskedBy, FocusActions, FocusHeader, KeyLegend, QuestionStep, ReviewStep } from "./QuestionFocusParts"
import { useFocusAnswering, useFocusChoice, useFocusKeys, useFocusState, type FocusState } from "./useQuestionFocus"

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
  const state = useFocusState(current?.id)
  if (!current) return null
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
  const { choose, type } = useFocusChoice({ current, question, state })
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
      <FocusHeader waiting={waiting} asking={asking} onLeave={onLeave} />

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex min-h-full w-full max-w-[760px] flex-col px-4 pt-6 md:pb-10 md:pt-14">
          <AskedBy current={current} agent={agent} onLeave={onLeave} />

          {step.screen === "question" ? (
            <QuestionStep questions={questions} question={question} q={step.q} draft={draft} answers={answers} ownRef={own} onChoose={choose} onType={type} />
          ) : (
            <ReviewStep questions={questions} answers={answers} agent={agent} onChange={(q) => setStep({ screen: "question", q })} />
          )}

          {error && <p role="alert" className="mt-4 text-sm text-lw-error">{error}</p>}

          <FocusActions step={step} busy={busy} label={nextLabel(step, questions.length, agent)} onBack={back} onDismiss={() => void dismiss()} onNext={next} />
        </div>
      </div>

      <KeyLegend />
    </div>
  )
}
