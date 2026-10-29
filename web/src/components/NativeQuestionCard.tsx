import { useState } from "react"
import type { NativeQuestion, NativeQuestionItem } from "../lib/nativeAgentSync"

interface NativeQuestionCardProps {
  question: NativeQuestionItem
  // Whose question it is, as the heading names them: "Claude Code".
  askedBy: string
  onAnswer: (requestId: string, answers: Record<string, string[]>) => void
  onDismiss: (requestId: string) => void
  // touch makes the card's choices large on a phone (below the md
  // breakpoint), as the approval tray shows it. The chat keeps it compact.
  touch?: boolean
}

// What the person has entered for one question so far.
interface Draft {
  chosen: string[]
  other: string
}

const emptyDraft: Draft = { chosen: [], other: "" }

// One choice: a typed answer replaces the option. Several: it joins them.
function answerOf(question: NativeQuestion, draft: Draft): string[] {
  const other = draft.other.trim()
  if (question.multiSelect) return other ? [...draft.chosen, other] : draft.chosen
  return other ? [other] : draft.chosen
}

const settledLabel: Partial<Record<NativeQuestionItem["status"], string>> = {
  answered: "Answered",
  dismissed: "Dismissed",
  expired: "Closed elsewhere",
}

// A question an agent put to the person: options to pick from, an answer of
// their own where the agent takes one, and what was answered once settled.
// It knows nothing of the agent that asked beyond its name.
export function NativeQuestionCard({ question, askedBy, onAnswer, onDismiss, touch = false }: NativeQuestionCardProps) {
  // On a phone in the tray: larger text and targets a thumb hits easily.
  // Every class is written out whole, as Tailwind finds them in the source.
  const t = (inTray: string, compact: string) => (touch ? inTray : compact)
  const [drafts, setDrafts] = useState<Record<string, Draft>>({})
  const submitting = question.status === "submitting"
  const open = question.status === "pending" || submitting
  const draftOf = (id: string) => drafts[id] ?? emptyDraft
  const setDraft = (id: string, update: (draft: Draft) => Draft) =>
    setDrafts((current) => ({ ...current, [id]: update(current[id] ?? emptyDraft) }))
  const answers = Object.fromEntries(question.questions.map((one) => [one.id, answerOf(one, draftOf(one.id))]))
  const complete = question.questions.every((one) => answers[one.id].length > 0)

  return (
    <section aria-label={`Question from ${askedBy}`} className="mx-4 my-2 min-w-0 rounded-lg border border-sky-600/40 bg-sky-900/10 p-3">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <div className="text-sm font-semibold text-sky-200">{askedBy} asks</div>
        {!open && <span className="ml-auto text-xs font-medium text-zinc-300">{settledLabel[question.status]}</span>}
      </div>

      {open ? (
        <div className="mt-2 space-y-3">
          {question.questions.map((one) => {
            const draft = draftOf(one.id)
            const preview = one.options.find((option) => option.preview && draft.chosen.includes(option.label))?.preview
            return (
              <fieldset key={one.id} aria-label={one.question} className="min-w-0 space-y-1.5">
                <legend className={`flex min-w-0 flex-wrap items-center gap-2 text-zinc-100 ${t("mb-1 text-base md:mb-0 md:text-sm", "mb-0 text-sm")}`}>
                  {one.header && <span className="rounded bg-sky-950/70 px-1.5 py-0.5 text-[11px] text-sky-300">{one.header}</span>}
                  <span className="min-w-0 break-words">{one.question}</span>
                </legend>
                {one.options.map((option) => (
                  <label key={option.label} className={`flex min-w-0 cursor-pointer items-start border border-zinc-700/70 bg-zinc-900/50 text-zinc-200 has-[:checked]:border-sky-500/70 has-[:checked]:bg-sky-950/40 ${t("min-h-12 gap-3 rounded-xl px-3 py-3 text-base md:min-h-0 md:gap-2 md:rounded md:px-2 md:py-1.5 md:text-sm", "min-h-0 gap-2 rounded px-2 py-1.5 text-sm")}`}>
                    <input
                      type={one.multiSelect ? "checkbox" : "radio"}
                      name={`${question.requestId}-${one.id}`}
                      className={`shrink-0 accent-sky-500 ${t("mt-1 h-5 w-5 md:mt-0.5 md:h-auto md:w-auto", "mt-0.5 h-auto w-auto")}`}
                      disabled={submitting}
                      checked={draft.chosen.includes(option.label)}
                      onChange={(event) => setDraft(one.id, (current) => ({
                        ...current,
                        chosen: one.multiSelect
                          ? (event.target.checked ? [...current.chosen, option.label] : current.chosen.filter((label) => label !== option.label))
                          : [option.label],
                      }))}
                    />
                    <span className="min-w-0">
                      <span className="break-words">{option.label}</span>
                      {option.description && <span className={`block break-words text-zinc-400 ${t("text-sm md:text-xs", "text-xs")}`}>{option.description}</span>}
                    </span>
                  </label>
                ))}
                {preview && <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-black/30 p-2 font-mono text-xs text-zinc-200">{preview}</pre>}
                {one.allowOther !== false && (
                  <input
                    type="text"
                    aria-label="Your own answer"
                    placeholder={one.multiSelect ? "Add an answer of your own" : "Or type an answer of your own"}
                    value={draft.other}
                    disabled={submitting}
                    onChange={(event) => setDraft(one.id, (current) => ({ ...current, other: event.target.value }))}
                    className={`w-full border border-zinc-700 bg-zinc-950 text-zinc-200 placeholder:text-zinc-600 focus:border-sky-500 focus:outline-none ${t("min-h-12 rounded-xl px-3 text-base md:min-h-0 md:rounded md:px-2 md:py-1.5 md:text-sm", "min-h-0 rounded px-2 py-1.5 text-sm")}`}
                  />
                )}
              </fieldset>
            )
          })}
          {question.error && <p role="alert" className="text-xs text-red-400">{question.error}</p>}
          <div className={`flex gap-2 ${t("flex-col md:flex-row md:flex-wrap md:items-center", "flex-row flex-wrap items-center")}`}>
            <button
              disabled={submitting || !complete}
              className={`bg-sky-700/70 text-white hover:bg-sky-600/70 disabled:cursor-not-allowed disabled:opacity-40 ${t("min-h-12 w-full rounded-xl px-4 text-base font-semibold md:min-h-0 md:w-auto md:rounded md:px-2 md:py-1 md:text-xs md:font-normal", "min-h-0 w-auto rounded px-2 py-1 text-xs font-normal")}`}
              onClick={() => onAnswer(question.requestId, answers)}
            >
              {question.questions.length === 1 ? "Send answer" : "Send answers"}
            </button>
            <button
              disabled={submitting}
              className={`text-zinc-300 hover:bg-zinc-800 disabled:cursor-wait disabled:opacity-50 ${t("min-h-12 w-full rounded-xl border border-zinc-700 px-4 text-base md:min-h-0 md:w-auto md:rounded md:border-transparent md:px-2 md:py-1 md:text-xs", "min-h-0 w-auto rounded border-transparent px-2 py-1 text-xs")}`}
              onClick={() => onDismiss(question.requestId)}
            >
              Dismiss
            </button>
            {submitting && <span role="status" className="text-xs text-sky-300">Sending…</span>}
          </div>
        </div>
      ) : (
        <dl className="mt-2 space-y-1 text-xs">
          {question.questions.map((one) => (
            <div key={one.id} className="flex min-w-0 flex-wrap gap-x-2">
              <dt className="min-w-0 break-words text-zinc-400">{one.question}</dt>
              {question.answers?.[one.id]?.length ? <dd className="min-w-0 break-words text-zinc-100">{question.answers[one.id].join(", ")}</dd> : null}
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}
