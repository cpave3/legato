import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react"
import { decideApproval, dismissApproval, type Approval } from "../../lib/approvals"
import type { NativeQuestion } from "../../lib/nativeAgentSync"
import type { QuestionDraft } from "../../lib/questionDrafts"
import {
  backStep, chooseOption, dialogKeyAction, missingMessage, movedIndex, nextStep, ownKeyAction, withDraft,
  type FocusDrafts, type FocusStep, type FocusVerb,
} from "../../lib/questionFocus"

const FIRST: FocusStep = { screen: "question", q: 0 }

// useFocusState holds where focus mode stands, every approval's drafts, and
// the dialog and the own-answer box it moves the keyboard between.
export function useFocusState(currentId: string | undefined) {
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

  return { step, setStep, drafts, setDrafts, busy, setBusy, error, setError, dialog, own }
}

export type FocusState = ReturnType<typeof useFocusState>

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// runBusy runs one request to the server, busy while it runs, its failure
// shown as the error.
async function runBusy(setBusy: Dispatch<SetStateAction<boolean>>, setError: Dispatch<SetStateAction<string | null>>, request: () => Promise<void>) {
  setBusy(true)
  try {
    await request()
  } catch (err) {
    setError(errorText(err))
  } finally {
    setBusy(false)
  }
}

interface Answering {
  baseUrl: string
  current: Approval
  questions: NativeQuestion[]
  answers: Record<string, string[]>
  state: FocusState
  onDone: () => Promise<void>
}

// useFocusAnswering is focus mode's actions on one approval: moving between
// its questions, sending its answers once all are given, and dismissing it.
export function useFocusAnswering({ baseUrl, current, questions, answers, state, onDone }: Answering) {
  const { step, setStep, setBusy, setError } = state
  const send = () => {
    const missing = questions.findIndex((one) => answers[one.id].length === 0)
    if (missing >= 0) {
      setStep({ screen: "question", q: missing })
      setError(missingMessage(questions.length))
      return
    }
    setError(null)
    void runBusy(setBusy, setError, async () => {
      await decideApproval(baseUrl, current.id, { decision: "answer", answers })
      await onDone()
    })
  }
  const next = () => {
    setError(null)
    const to = nextStep(step, questions.length)
    if (to === "send") send()
    else setStep(to)
  }
  const back = () => {
    setError(null)
    const to = backStep(step, questions.length)
    if (to) setStep(to)
  }
  const dismiss = () => runBusy(setBusy, setError, async () => {
    await dismissApproval(baseUrl, current.id)
    await onDone()
  })
  return { send, next, back, dismiss }
}

interface Choosing {
  current: Approval
  question: NativeQuestion
  state: FocusState
}

// useFocusChoice is how the question on screen is answered: an option chosen
// by index, and the box for an answer in the person's own words.
export function useFocusChoice({ current, question, state }: Choosing) {
  const setDraft = (update: (draft: QuestionDraft) => QuestionDraft) =>
    state.setDrafts((all) => withDraft(all, current.id, question.id, update))
  const choose = (index: number) => {
    const option = question.options[index]
    if (option) setDraft((d) => chooseOption(question, d, option.label))
  }
  const type = (other: string) => setDraft((d) => ({ ...d, other }))
  return { choose, type }
}

type Verbs = Record<FocusVerb, (arg: number) => void>

interface Keys {
  question: NativeQuestion
  draft: QuestionDraft
  state: FocusState
  actions: { send: () => void; next: () => void; back: () => void; choose: (index: number) => void }
  onLeave: () => void
}

// useFocusKeys is the dialog's keyboard handler: on a computer every step of
// focus mode has a key.
export function useFocusKeys({ question, draft, state, actions, onLeave }: Keys) {
  const { step, busy, dialog, own } = state
  const verbs: Verbs = {
    ...actions,
    leave: onLeave,
    release: () => dialog.current?.focus(),
    type: () => own.current?.focus(),
    move: (by) => actions.choose(movedIndex(question, draft, by)),
  }
  return (event: React.KeyboardEvent) => {
    if (busy) return
    const action = event.target === own.current
      ? ownKeyAction(event.key)
      : dialogKeyAction({
        key: event.key,
        mod: event.metaKey || event.ctrlKey,
        onButton: event.target instanceof HTMLButtonElement,
        screen: step.screen,
        multiSelect: Boolean(question.multiSelect),
        canType: Boolean(own.current),
      })
    if (!action) return
    event.preventDefault()
    verbs[action.do](action.arg ?? 0)
  }
}
