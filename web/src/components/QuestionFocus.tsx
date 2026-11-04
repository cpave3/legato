import { useEffect, useRef, useState } from "react"
import { Link } from "react-router-dom"
import { useServer } from "../hooks/useServer"
import { decideApproval, dismissApproval, type Approval } from "../lib/approvals"
import { answerOf, approvalQuestions, emptyDraft, recommended, type QuestionDraft } from "../lib/questionDrafts"

type Props = {
  // The approvals that ask questions, the first answered first.
  approvals: Approval[]
  // How many approvals need the person in all, questions or not.
  waiting: number
  onLeave: () => void
  onDone: () => Promise<void>
}

type Drafts = Record<string, Record<string, QuestionDraft>>

const keyCap = "rounded border border-lw-line-strong px-1.5 font-lw-mono text-[11px] text-lw-text-2"

// Focus mode: the questions agents ask, one at a time, in the middle of the
// screen with nothing else on it. On a computer every step has a key (the
// legend at the foot); on a phone it fills the screen, with Back and Next
// where a thumb is. One approval's questions are reviewed and sent together;
// an approval with one question sends from that question.
export function QuestionFocus({ approvals, waiting, onLeave, onDone }: Props) {
  const { baseUrl } = useServer()
  const current = approvals[0]
  const questions = current ? approvalQuestions(current) : []
  const [q, setQ] = useState(0)
  const [screen, setScreen] = useState<"question" | "review">("question")
  const [drafts, setDrafts] = useState<Drafts>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dialog = useRef<HTMLDivElement>(null)
  const own = useRef<HTMLInputElement>(null)

  // A new approval starts at its first question.
  const currentId = current?.id
  useEffect(() => {
    setQ(0)
    setScreen("question")
    setError(null)
  }, [currentId])

  // Keys go to the dialog whenever the person is not typing.
  useEffect(() => {
    if (document.activeElement !== own.current) dialog.current?.focus()
  }, [currentId, q, screen])

  if (!current) return null
  const question = questions[Math.min(q, questions.length - 1)]
  const draftOf = (id: string) => drafts[current.id]?.[id] ?? emptyDraft
  const setDraft = (id: string, update: (draft: QuestionDraft) => QuestionDraft) =>
    setDrafts((all) => ({ ...all, [current.id]: { ...all[current.id], [id]: update(all[current.id]?.[id] ?? emptyDraft) } }))
  const answers = Object.fromEntries(questions.map((one) => [one.id, answerOf(one, draftOf(one.id))]))
  const missing = questions.findIndex((one) => answers[one.id].length === 0)
  const draft = draftOf(question.id)
  const typed = draft.other.trim() !== ""
  const last = q === questions.length - 1
  const single = questions.length === 1

  const choose = (index: number) => {
    const option = question.options[index]
    if (!option) return
    setDraft(question.id, (d) => question.multiSelect
      ? { ...d, chosen: d.chosen.includes(option.label) ? d.chosen.filter((l) => l !== option.label) : [...d.chosen, option.label] }
      : { chosen: [option.label], other: "" })
  }
  const move = (by: number) => {
    const n = question.options.length
    const at = question.options.findIndex((o) => draft.chosen.includes(o.label))
    choose(at < 0 ? (by > 0 ? 0 : n - 1) : (at + by + n) % n)
  }
  const send = async () => {
    if (missing >= 0) {
      setScreen("question")
      setQ(missing)
      setError(single ? "Choose an answer first." : "This one still needs an answer.")
      return
    }
    setBusy(true)
    setError(null)
    try {
      await decideApproval(baseUrl, current.id, { decision: "answer", answers })
      await onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }
  const next = () => {
    setError(null)
    if (screen === "review") void send()
    else if (!last) setQ(q + 1)
    else if (single) void send()
    else setScreen("review")
  }
  const back = () => {
    setError(null)
    if (screen === "review") { setScreen("question"); setQ(questions.length - 1) }
    else if (q > 0) setQ(q - 1)
  }
  const dismiss = async () => {
    setBusy(true)
    try { await dismissApproval(baseUrl, current.id); await onDone() }
    catch (err) { setError(err instanceof Error ? err.message : String(err)) }
    finally { setBusy(false) }
  }

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (busy) return
    const key = event.key
    if (event.target === own.current) {
      if (key === "Enter") { event.preventDefault(); next() }
      else if (key === "Escape") { event.preventDefault(); dialog.current?.focus() }
      return
    }
    if (key === "Escape") { event.preventDefault(); onLeave(); return }
    if ((event.metaKey || event.ctrlKey) && key === "Enter") { event.preventDefault(); void send(); return }
    if (screen === "question" && /^[1-9]$/.test(key)) { event.preventDefault(); choose(Number(key) - 1); return }
    if (screen === "question" && !question.multiSelect && (key === "ArrowDown" || key === "ArrowUp")) { event.preventDefault(); move(key === "ArrowDown" ? 1 : -1); return }
    if (key === "Enter" && !(event.target instanceof HTMLButtonElement)) { event.preventDefault(); next(); return }
    if (key === "ArrowLeft") { event.preventDefault(); back(); return }
    if (key === "/" && screen === "question" && own.current) { event.preventDefault(); own.current.focus() }
  }

  const agent = current.details.agent_kind || "The agent"
  const task = current.details.task_title || current.title
  const nextLabel = screen === "review" ? `Send answers to ${agent}` : !last ? "Next question" : single ? "Send answer" : "Review answers"

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
          {approvals.length === 1 ? "1 approval asks" : `${approvals.length} approvals ask`}
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
            <span className="min-w-0 flex-1 text-lw-text-2">{task}</span>
            {current.task_id && (
              <Link to={`/agents?task=${encodeURIComponent(current.task_id)}`} onClick={onLeave} className="text-lw-link hover:underline">Open agent</Link>
            )}
          </div>

          {screen === "question" ? (
            <section aria-label={question.question} className="flex flex-col">
              {!single && (
                <div className="mt-6 flex items-center gap-3.5 md:mt-7">
                  <span className="whitespace-nowrap font-lw-mono text-xs text-lw-text-2">Question {q + 1} of {questions.length}</span>
                  <div aria-hidden className="flex flex-1 gap-1.5">
                    {questions.map((one, i) => (
                      <span key={one.id} className={`h-1 flex-1 rounded-sm ${i === q ? "bg-lw-accent" : answers[one.id].length ? "bg-lw-accent/60" : "bg-lw-line-strong"}`} />
                    ))}
                  </div>
                </div>
              )}
              {question.header && <span className="mt-6 self-start rounded-md bg-sky-950/70 px-2 py-0.5 text-xs text-sky-300">{question.header}</span>}
              <h1 className="mt-3.5 max-w-[68ch] whitespace-pre-wrap break-words text-xl font-medium leading-relaxed md:text-[23px]">{question.question}</h1>
              {question.multiSelect && <p className="mt-2 text-sm text-lw-text-2">Choose any that apply.</p>}

              <div role="group" aria-label="Answers" className="mt-6 flex flex-col gap-2.5 md:mt-7">
                {question.options.map((option, i) => {
                  const chosen = !(typed && !question.multiSelect) && draft.chosen.includes(option.label)
                  const shown = recommended(option.label)
                  return (
                    <button
                      key={option.label}
                      aria-pressed={chosen}
                      onClick={() => choose(i)}
                      className={`flex w-full items-start gap-3.5 rounded-xl border px-4 py-3.5 text-left transition-colors md:px-[18px] md:py-4 ${chosen ? "border-lw-accent bg-lw-accent-soft" : "border-lw-line-strong bg-lw-surface hover:border-lw-faint"}`}
                    >
                      <span aria-hidden className={`grid h-[26px] w-[26px] shrink-0 place-items-center rounded-md border font-lw-mono text-[13px] font-semibold ${chosen ? "border-lw-accent bg-lw-accent text-white" : "border-lw-line-strong text-lw-text-2"}`}>
                        {i < 9 ? i + 1 : ""}
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
                })}
              </div>
              {question.options.find((o) => o.preview && draft.chosen.includes(o.label))?.preview && (
                <pre className="mt-3 max-h-56 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-lw-ground p-3 font-lw-mono text-xs text-lw-text">
                  {question.options.find((o) => o.preview && draft.chosen.includes(o.label))?.preview}
                </pre>
              )}

              {question.allowOther !== false && (
                <>
                  <label htmlFor="focus-own-answer" className="mt-5 flex items-center gap-2 text-sm text-lw-text-2">
                    {question.multiSelect ? "Add an answer in your own words" : "Or answer in your own words"}
                    <kbd aria-hidden className={`hidden md:inline ${keyCap}`}>/</kbd>
                  </label>
                  <input
                    id="focus-own-answer"
                    ref={own}
                    type="text"
                    value={draft.other}
                    onChange={(event) => setDraft(question.id, (d) => ({ ...d, other: event.target.value }))}
                    placeholder="Type an answer; Enter goes on"
                    className="mt-2 min-h-12 w-full rounded-xl border border-lw-line-strong bg-lw-ground px-3.5 text-base text-lw-text placeholder:text-lw-faint focus:border-lw-accent focus:outline-none md:text-[15px]"
                  />
                </>
              )}
            </section>
          ) : (
            <section aria-label="Review your answers" className="flex flex-col">
              <h1 className="mt-6 text-xl font-semibold md:mt-7 md:text-[23px]">Review your answers</h1>
              <p className="mt-1.5 text-sm text-lw-text-2">{agent} gets them all at once, when you send.</p>
              <ol className="mt-6 flex flex-col gap-2.5">
                {questions.map((one, i) => (
                  <li key={one.id} className="flex items-start gap-3.5 rounded-xl border border-lw-line bg-lw-surface px-4 py-3.5">
                    <span className="min-w-0 flex-1">
                      {one.header && <span className="mb-1.5 inline-block rounded-md bg-sky-950/70 px-2 py-0.5 text-xs text-sky-300">{one.header}</span>}
                      <span className="block break-words text-sm leading-relaxed text-lw-text-2">{one.question}</span>
                      <span className={`mt-1.5 block break-words text-base font-semibold ${answers[one.id].length ? "text-lw-accent-text" : "text-lw-needs"}`}>
                        {answers[one.id].length ? answers[one.id].map((answer) => recommended(answer).text).join(", ") : "Not answered"}
                      </span>
                    </span>
                    <button onClick={() => { setScreen("question"); setQ(i) }} className="min-h-11 shrink-0 rounded-lg border border-lw-line-strong px-3 text-sm hover:bg-lw-raised md:min-h-9">
                      Change
                    </button>
                  </li>
                ))}
              </ol>
            </section>
          )}

          {error && <p role="alert" className="mt-4 text-sm text-lw-error">{error}</p>}

          <div className="sticky bottom-0 -mx-4 mt-auto flex items-center gap-2.5 border-t border-lw-surface bg-lw-rail px-4 pb-[calc(env(safe-area-inset-bottom)+0.75rem)] pt-3 md:mx-0 md:mt-8 md:px-0 md:pb-4 md:pt-4">
            <button
              onClick={back}
              disabled={busy || (screen === "question" && q === 0)}
              className="flex min-h-12 items-center gap-2 rounded-xl border border-lw-line-strong px-4 text-sm disabled:opacity-40 md:min-h-11"
            >
              <span aria-hidden className="font-lw-mono">←</span> Back
            </button>
            <button onClick={() => void dismiss()} disabled={busy} className="hidden min-h-11 rounded-xl px-3 text-sm text-lw-text-2 hover:bg-lw-surface disabled:opacity-40 md:block">
              Dismiss
            </button>
            <span className="flex-1" />
            {busy && <span role="status" className="text-sm text-lw-text-2">Sending…</span>}
            <button
              onClick={next}
              disabled={busy}
              className="flex min-h-12 flex-[2] items-center justify-center gap-2.5 rounded-xl bg-lw-accent px-5 text-sm font-semibold text-white hover:brightness-110 disabled:opacity-50 md:min-h-11 md:flex-none"
            >
              {nextLabel}
              <kbd aria-hidden className="hidden rounded bg-white/20 px-1.5 font-lw-mono text-xs md:inline">{screen === "review" ? "⌘↵" : "↵"}</kbd>
            </button>
          </div>
        </div>
      </div>

      <footer aria-label="Keys" className="hidden flex-wrap justify-center gap-x-5 gap-y-2 border-t border-lw-surface px-6 py-4 text-xs text-lw-faint md:flex">
        {[["1–9", "choose"], ["↑ ↓", "move"], ["↵", "next"], ["←", "back"], ["/", "type your own"], ["⌘↵", "send"], ["Esc", "leave focus"]].map(([keys, does]) => (
          <span key={keys} className="flex items-center gap-1.5"><kbd className={keyCap}>{keys}</kbd>{does}</span>
        ))}
      </footer>
    </div>
  )
}
