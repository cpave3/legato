import type { Ref } from "react"
import { Link } from "react-router-dom"
import type { Approval } from "../../lib/approvals"
import type { NativeQuestion, NativeQuestionOption } from "../../lib/nativeAgentSync"
import { recommended, type QuestionDraft } from "../../lib/questionDrafts"
import { chosenPreview, type FocusStep } from "../../lib/questionFocus"

const keyCap = "rounded border border-lw-line-strong px-1.5 font-lw-mono text-[11px] text-lw-text-2"

export function FocusHeader({ waiting, asking, onLeave }: { waiting: number; asking: number; onLeave: () => void }) {
  return (
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
  )
}

export function AskedBy({ current, agent, onLeave }: { current: Approval; agent: string; onLeave: () => void }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-2.5 gap-y-1 text-sm">
      <span className="font-semibold text-sky-200">{agent}</span>
      <span aria-hidden className="text-lw-faint">·</span>
      <span className="min-w-0 flex-1 text-lw-text-2">{current.details.task_title || current.title}</span>
      {current.task_id && (
        <Link to={`/agents?task=${encodeURIComponent(current.task_id)}`} onClick={onLeave} className="text-lw-link hover:underline">Open agent</Link>
      )}
    </div>
  )
}

function progressClass(at: boolean, answered: boolean): string {
  if (at) return "bg-lw-accent"
  return answered ? "bg-lw-accent/60" : "bg-lw-line-strong"
}

function Progress({ questions, q, answers }: { questions: NativeQuestion[]; q: number; answers: Record<string, string[]> }) {
  return (
    <div className="mt-6 flex items-center gap-3.5 md:mt-7">
      <span className="whitespace-nowrap font-lw-mono text-xs text-lw-text-2">Question {q + 1} of {questions.length}</span>
      <div aria-hidden className="flex flex-1 gap-1.5">
        {questions.map((one, i) => (
          <span key={one.id} className={`h-1 flex-1 rounded-sm ${progressClass(i === q, answers[one.id].length > 0)}`} />
        ))}
      </div>
    </div>
  )
}

function OptionButton({ option, index, chosen, onChoose }: { option: NativeQuestionOption; index: number; chosen: boolean; onChoose: (index: number) => void }) {
  const shown = recommended(option.label)
  return (
    <button
      aria-pressed={chosen}
      onClick={() => onChoose(index)}
      className={`flex w-full items-start gap-3.5 rounded-xl border px-4 py-3.5 text-left transition-colors md:px-[18px] md:py-4 ${chosen ? "border-lw-accent bg-lw-accent-soft" : "border-lw-line-strong bg-lw-surface hover:border-lw-faint"}`}
    >
      <span aria-hidden className={`grid h-[26px] w-[26px] shrink-0 place-items-center rounded-md border font-lw-mono text-[13px] font-semibold ${chosen ? "border-lw-accent bg-lw-accent text-[var(--theme-canvas,#fff)]" : "border-lw-line-strong text-lw-text-2"}`}>
        {index < 9 ? index + 1 : ""}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-2">
          <span className="break-words text-base font-semibold md:text-[17px]">{shown.text}</span>
          {shown.recommended && <span className="rounded bg-lw-live-soft px-1.5 py-0.5 text-[11px] font-medium tracking-wide text-lw-live">Recommended</span>}
        </span>
        {option.description && <span className="mt-1 block break-words text-sm leading-relaxed text-lw-text-2">{option.description}</span>}
      </span>
    </button>
  )
}

function OwnAnswer({ multiSelect, value, ownRef, onType }: { multiSelect: boolean; value: string; ownRef: Ref<HTMLInputElement>; onType: (value: string) => void }) {
  return (
    <>
      <label htmlFor="focus-own-answer" className="mt-5 flex items-center gap-2 text-sm text-lw-text-2">
        {multiSelect ? "Add an answer in your own words" : "Or answer in your own words"}
        <kbd aria-hidden className={`hidden md:inline ${keyCap}`}>/</kbd>
      </label>
      <input
        id="focus-own-answer"
        ref={ownRef}
        type="text"
        value={value}
        onChange={(event) => onType(event.target.value)}
        placeholder="Type an answer; Enter goes on"
        className="mt-2 min-h-12 w-full rounded-xl border border-lw-line-strong bg-lw-ground px-3.5 text-base text-lw-text placeholder:text-lw-faint focus:border-lw-accent focus:outline-none md:text-[15px]"
      />
    </>
  )
}

interface QuestionStepProps {
  questions: NativeQuestion[]
  question: NativeQuestion
  q: number
  draft: QuestionDraft
  answers: Record<string, string[]>
  ownRef: Ref<HTMLInputElement>
  onChoose: (index: number) => void
  onType: (value: string) => void
}

// One question: where it stands among the approval's, its options, the
// chosen option's preview, and the box for an answer of the person's own.
export function QuestionStep({ questions, question, q, draft, answers, ownRef, onChoose, onType }: QuestionStepProps) {
  const multiSelect = Boolean(question.multiSelect)
  // A typed answer replaces the only choice, so no option shows as chosen.
  const replaced = draft.other.trim() !== "" && !multiSelect
  const preview = chosenPreview(question, draft)
  return (
    <section aria-label={question.question} className="flex flex-col">
      {questions.length > 1 && <Progress questions={questions} q={q} answers={answers} />}
      {question.header && <span className="mt-6 self-start rounded-md bg-sky-950/70 px-2 py-0.5 text-xs text-sky-300">{question.header}</span>}
      <h1 className="mt-3.5 max-w-[68ch] whitespace-pre-wrap break-words text-xl font-medium leading-relaxed md:text-[23px]">{question.question}</h1>
      {multiSelect && <p className="mt-2 text-sm text-lw-text-2">Choose any that apply.</p>}

      <div role="group" aria-label="Answers" className="mt-6 flex flex-col gap-2.5 md:mt-7">
        {question.options.map((option, i) => (
          <OptionButton key={option.label} option={option} index={i} chosen={!replaced && draft.chosen.includes(option.label)} onChoose={onChoose} />
        ))}
      </div>
      {preview && (
        <pre className="mt-3 max-h-56 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-lw-ground p-3 font-lw-mono text-xs text-lw-text">
          {preview}
        </pre>
      )}

      {question.allowOther !== false && <OwnAnswer multiSelect={multiSelect} value={draft.other} ownRef={ownRef} onType={onType} />}
    </section>
  )
}

function ReviewRow({ one, answer, onChange }: { one: NativeQuestion; answer: string[]; onChange: () => void }) {
  return (
    <li className="flex items-start gap-3.5 rounded-xl border border-lw-line bg-lw-surface px-4 py-3.5">
      <span className="min-w-0 flex-1">
        {one.header && <span className="mb-1.5 inline-block rounded-md bg-sky-950/70 px-2 py-0.5 text-xs text-sky-300">{one.header}</span>}
        <span className="block break-words text-sm leading-relaxed text-lw-text-2">{one.question}</span>
        <span className={`mt-1.5 block break-words text-base font-semibold ${answer.length ? "text-lw-accent-text" : "text-lw-needs"}`}>
          {answer.length ? answer.map((each) => recommended(each).text).join(", ") : "Not answered"}
        </span>
      </span>
      <button onClick={onChange} className="min-h-11 shrink-0 rounded-lg border border-lw-line-strong px-3 text-sm hover:bg-lw-raised md:min-h-9">
        Change
      </button>
    </li>
  )
}

// The review of one approval's answers before they are sent together.
export function ReviewStep({ questions, answers, agent, onChange }: { questions: NativeQuestion[]; answers: Record<string, string[]>; agent: string; onChange: (q: number) => void }) {
  return (
    <section aria-label="Review your answers" className="flex flex-col">
      <h1 className="mt-6 text-xl font-semibold md:mt-7 md:text-[23px]">Review your answers</h1>
      <p className="mt-1.5 text-sm text-lw-text-2">{agent} gets them all at once, when you send.</p>
      <ol className="mt-6 flex flex-col gap-2.5">
        {questions.map((one, i) => <ReviewRow key={one.id} one={one} answer={answers[one.id]} onChange={() => onChange(i)} />)}
      </ol>
    </section>
  )
}

interface FocusActionsProps {
  step: FocusStep
  busy: boolean
  label: string
  onBack: () => void
  onDismiss: () => void
  onNext: () => void
}

// Back, Dismiss and Next, at the foot where a thumb is on a phone.
export function FocusActions({ step, busy, label, onBack, onDismiss, onNext }: FocusActionsProps) {
  const review = step.screen === "review"
  return (
    <div className="sticky bottom-0 -mx-4 mt-auto flex items-center gap-2.5 border-t border-lw-surface bg-lw-rail px-4 pb-[calc(env(safe-area-inset-bottom)+0.75rem)] pt-3 md:mx-0 md:mt-8 md:px-0 md:pb-4 md:pt-4">
      <button
        onClick={onBack}
        disabled={busy || (!review && step.q === 0)}
        className="flex min-h-12 items-center gap-2 rounded-xl border border-lw-line-strong px-4 text-sm disabled:opacity-40 md:min-h-11"
      >
        <span aria-hidden className="font-lw-mono">←</span> Back
      </button>
      <button onClick={onDismiss} disabled={busy} className="hidden min-h-11 rounded-xl px-3 text-sm text-lw-text-2 hover:bg-lw-surface disabled:opacity-40 md:block">
        Dismiss
      </button>
      <span className="flex-1" />
      {busy && <span role="status" className="text-sm text-lw-text-2">Sending…</span>}
      <button
        onClick={onNext}
        disabled={busy}
        className="flex min-h-12 flex-[2] items-center justify-center gap-2.5 rounded-xl bg-lw-accent px-5 text-sm font-semibold text-[var(--theme-canvas,#fff)] hover:brightness-110 disabled:opacity-50 md:min-h-11 md:flex-none"
      >
        {label}
        <kbd aria-hidden className="hidden rounded bg-white/20 px-1.5 font-lw-mono text-xs md:inline">{review ? "⌘↵" : "↵"}</kbd>
      </button>
    </div>
  )
}

const LEGEND = [["1–9", "choose"], ["↑ ↓", "move"], ["↵", "next"], ["←", "back"], ["/", "type your own"], ["⌘↵", "send"], ["Esc", "leave focus"]]

export function KeyLegend() {
  return (
    <footer aria-label="Keys" className="hidden flex-wrap justify-center gap-x-5 gap-y-2 border-t border-lw-surface px-6 py-4 text-xs text-lw-faint md:flex">
      {LEGEND.map(([keys, does]) => (
        <span key={keys} className="flex items-center gap-1.5"><kbd className={keyCap}>{keys}</kbd>{does}</span>
      ))}
    </footer>
  )
}
