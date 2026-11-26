import { getTextWidth } from "@evenrealities/pretext"
import { whole, type Dictation, type TextPage } from "./agent"
import { fitItem } from "./glance"
import { BORDER, CANVAS_WIDTH, COUNT_WIDTH, FRAME_WIDTH, TITLE_WIDTH, TITLE_WIDTH_COUNTED, center, fitLines, inner, right } from "./layout"
import type { Approval } from "./legato"

// Answering agents' questions on the glasses. An agent's question request
// (AskUserQuestion) reaches Legato as an approval of source agent_question,
// with one to four questions in details.questions; they are answered
// together, with POST /api/approvals/{id}/decision {decision: "answer",
// answers: {<question id>: [...]}}.
//
// The glasses show one question per screen, every question of every request
// in a row, and inside a question one option per screen. Each box that takes
// the touchpad holds text that fits it: a captured text box that fits sends
// an event on every swipe (SCROLL_BOTTOM down, SCROLL_TOP up), which moves
// the app's own cursor, while one that overflows scrolls itself and says
// nothing. The firmware's list cannot say which item it highlights, so it
// cannot show the description of the option under the cursor.

export type Question = {
  id: string
  header?: string
  question: string
  options: { label: string; description?: string }[]
  multiSelect?: boolean
  // Absent means allowed, as on the web.
  allowOther?: boolean
}

// One question waiting, in its request: the `part`th (from 0) of `parts`.
export type Asked = { approval: Approval; question: Question; part: number; parts: number }

// What the operator has given a question so far: the options chosen, and
// an answer in their own words (web/src/lib/questionDrafts.ts).
export type Draft = { chosen: string[]; other: string }
// A request's drafts, by question ID.
export type Drafts = Record<string, Draft>

// A card in a question's deck, one per screen.
export type Card = { kind: "option"; index: number } | { kind: "other" } | { kind: "done" }

// Sending a request's answers: confirmed with a second tap, then sent.
export type Sending = { step: "confirm" } | { step: "sending" } | { step: "sent"; next?: Asked } | { step: "problem"; problem: string }

// A question card: the header row with where it is among those waiting, the
// question framed, its facts dim under it.
export type QuestionPage = { badge: string; title: string; count: string; question: string; facts: string; hint: string }
// An option card: the question dim above, the option's label framed with its
// place, its description under it.
export type OptionPage = { badge: string; title: string; ask: string; option: string; description: string; hint: string }

// How many lines each box holds (app.ts sizes them to these).
export const QUESTION_LINES = 6
export const ASK_LINES = 2
export const DESCRIPTION_LINES = 3

export const isQuestion = (a: Approval) => a.source === "agent_question"

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v)
const text = (v: unknown) => (typeof v === "string" ? v : undefined)

// A request's questions that the glasses can answer: each with an ID, its
// text and options that have labels.
export function questionsOf(a: Approval): Question[] {
  const raw = a.details?.questions
  if (!Array.isArray(raw)) return []
  const out: Question[] = []
  for (const q of raw) {
    if (!isObject(q) || !text(q.id) || !text(q.question) || !Array.isArray(q.options)) continue
    if (!q.options.every((o) => isObject(o) && typeof o.label === "string")) continue
    out.push({
      id: q.id as string,
      header: text(q.header)?.trim() || undefined,
      question: q.question as string,
      options: (q.options as Record<string, unknown>[]).map((o) => ({ label: o.label as string, description: text(o.description) })),
      multiSelect: q.multiSelect === true,
      allowOther: q.allowOther !== false,
    })
  }
  return out
}

// Every question waiting, request by request, in the order Legato listed them.
export function askedQuestions(approvals: Approval[]): Asked[] {
  return approvals.filter(isQuestion).flatMap((approval) => {
    const questions = questionsOf(approval)
    return questions.map((question, part) => ({ approval, question, part, parts: questions.length }))
  })
}

// The answer a draft gives: one choice, or the operator's words in its place;
// several, with the operator's words added.
export function answerOf(q: Question, draft: Draft | undefined): string[] {
  if (!draft) return []
  const other = draft.other.trim()
  if (q.multiSelect) return other ? [...draft.chosen, other] : draft.chosen
  return other ? [other] : draft.chosen
}

// The cards of a question: its options, the operator's own answer unless the
// agent forbids one, and Done when any may be chosen.
export function cardsOf(q: Question): Card[] {
  const cards: Card[] = q.options.map((_, index) => ({ kind: "option", index }))
  if (q.allowOther !== false) cards.push({ kind: "other" })
  if (q.multiSelect) cards.push({ kind: "done" })
  return cards
}

// Agents mark the option they suggest with "(Recommended)" at the end of its
// label; the glasses show a ★ instead, and send the label whole.
export function recommended(label: string): { text: string; recommended: boolean } {
  const match = /^(.*\S)\s*\(recommended\)\s*$/i.exec(label)
  return match ? { text: match[1], recommended: true } : { text: label, recommended: false }
}

const shown = (label: string) => recommended(label).text
const flat = (t: string) => t.replace(/\s+/g, " ").trim()
const keys = (t: string) => center(t, CANVAS_WIDTH)
const titled = (who: string, width = TITLE_WIDTH) => fitItem(who, inner(width), Infinity)
// The answer as the operator reads it.
const said = (answer: string[]) => answer.map(shown).join(", ")

// A label with its place at the right of its box, on one line.
function placed(label: string, place: string): string {
  const width = inner(FRAME_WIDTH, BORDER)
  const tail = `  ${place}`
  const fitted = fitItem(label, width - getTextWidth(tail), Infinity)
  return fitted + right(place, width - getTextWidth(fitted))
}

// One question, among `of` waiting, at `at` (from 0).
export function questionPage(asked: Asked, who: string, at: number, of: number, draft?: Draft): QuestionPage {
  const q = asked.question
  const facts: string[] = []
  if (q.header) facts.push(q.header)
  if (asked.parts > 1) facts.push(`part ${asked.part + 1} of ${asked.parts}`)
  const answer = answerOf(q, draft)
  if (answer.length) facts.push(`your answer: ${said(answer)}`)
  else {
    const n = q.options.length
    facts.push(`${n} option${n === 1 ? "" : "s"}${q.allowOther !== false ? " or your own" : ""}`)
    if (q.multiSelect) facts.push("choose any")
  }
  return {
    badge: "● question",
    title: titled(who, TITLE_WIDTH_COUNTED),
    count: right(`${at + 1} of ${of}`, inner(COUNT_WIDTH)),
    question: fitLines(flat(q.question), inner(FRAME_WIDTH, BORDER), QUESTION_LINES),
    facts: fitItem(facts.join(" · "), inner(FRAME_WIDTH, BORDER), Infinity),
    hint: keys(of > 1 ? "Swipe: questions · Tap: answer · Double tap: back" : "Tap: answer · Double tap: back"),
  }
}

// The `card`th card of a question's deck.
export function optionPage(asked: Asked, who: string, card: number, draft: Draft): OptionPage {
  const q = asked.question
  const cards = cardsOf(q)
  const c = cards[card] ?? cards[0]
  const place = `${card + 1}/${cards.length}`
  const page = (option: string, description: string, hint: string) => ({
    badge: q.multiSelect ? "● choose any" : "● answer?",
    title: titled(who),
    ask: fitLines(flat(q.question), inner(FRAME_WIDTH), ASK_LINES),
    option: placed(option, place),
    description: description ? fitLines(flat(description), inner(FRAME_WIDTH), DESCRIPTION_LINES) : "",
    hint: keys(hint),
  })
  switch (c.kind) {
    case "option": {
      const { label, description } = q.options[c.index]
      const { text, recommended: star } = recommended(label)
      const chosen = draft.chosen.includes(label)
      const mark = q.multiSelect ? (chosen ? "■ " : "□ ") : chosen && !draft.other.trim() ? "● " : ""
      return page(`${mark}${star ? "★ " : ""}${text}`, description ?? "", `Swipe: options · Tap: ${q.multiSelect ? "tick" : "choose"} · Double tap: back`)
    }
    case "other": {
      const own = draft.other.trim()
      const description = own ? `Yours: “${own}”. Hold to say it again.` : "Hold the touchpad and say it. You see the words before anything is sent."
      return page("Your own answer…", description, "Swipe: options · Hold: say it · Double tap: back")
    }
    case "done": {
      const answer = answerOf(q, draft)
      if (!answer.length) return page("Done · none ticked", "Tick one first, or say your own.", "Swipe: options · Double tap: back")
      return page(`Done · ${answer.length} ticked`, said(answer), "Swipe: options · Tap: done · Double tap: back")
    }
  }
}

function minutes(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`
}

// The operator's own answer, dictated.
export function ownAnswerPage(asked: Asked, who: string, d: Dictation): TextPage {
  const page = (badge: string, body: string, hint: string) => ({ badge, title: titled(who), body: whole(body), hint: hint ? keys(hint) : "" })
  switch (d.step) {
    case "listening":
      return page("● Listening", `Say your answer to:\n${flat(asked.question.question)}\n\n${minutes(d.seconds)}`, "Release: stop · Double tap: cancel")
    case "transcribing":
      return page("Your answer", "Transcribing…", "Double tap: cancel")
    case "review":
    case "sending":
      return page("Your answer", `“${d.text}”`, "Tap: use it · Hold: say it again · Double tap: back")
    case "sent":
      return page("Your answer", "", "")
    case "problem":
      return page("▲ Problem", d.problem, "Hold: try again · Double tap: back")
  }
}

// A request's answers: asked to send, sending, sent, or not sent.
export function sendPage(approval: Approval, who: string, drafts: Drafts, s: Sending): TextPage {
  const questions = questionsOf(approval)
  const answers = questions.map((q) => answerOf(q, drafts[q.id]))
  let summary: string
  if (questions.length === 1) summary = `${said(answers[0])}\n\n${flat(questions[0].question)}`
  else {
    const lines = questions.map((q, i) => `${q.header ?? `Question ${i + 1}`}: ${said(answers[i])}`)
    const all = questions.length === 2 ? "Both answers" : `All ${questions.length} answers`
    summary = `${lines.join("\n")}\n\n${all} go to ${approval.task_id || "the agent"} together.`
  }
  const kept = questions.length === 1 ? `Your answer is kept: ${said(answers[0])}` : "Your answers are kept."
  const page = (badge: string, body: string, hint: string) => ({ badge, title: titled(who), body: whole(body), hint: hint ? keys(hint) : "" })
  switch (s.step) {
    case "confirm":
      return page("Send?", summary, "Tap: send · Double tap: back")
    case "sending":
      return page("Sending", summary, "")
    case "sent": {
      const next = s.next ? `\n\nNext, ${s.next.approval.task_id || "an agent"} asks: ${flat(s.next.question.question)}` : ""
      return page("Sent", `Answered ${approval.task_id || "the agent"}.${next}`, "")
    }
    case "problem":
      return page("▲ Not sent", `${s.problem}\n\n${kept}`, "Tap: send again · Double tap: back")
  }
}

// The question open left the list: answered elsewhere, or its agent ended.
// The glasses link passes on only what is pending, so it cannot say which.
export function gonePage(who: string, more: boolean): TextPage {
  return {
    badge: "○ gone",
    title: titled(who),
    body: "No longer waiting.\n\nIt was answered elsewhere, or its agent moved on. Nothing was sent.",
    hint: keys(more ? "Tap: next question · Double tap: back" : "Double tap: back"),
  }
}

export function noQuestionsPage(instance: string): TextPage {
  return {
    badge: "○ questions",
    title: titled(instance),
    body: "No agent is asking you anything.\n\nNew questions show here and on the Active tab.",
    hint: keys("Double tap: back"),
  }
}

// While the question an agent's feed shows is read from Legato.
export function readingPage(who: string): TextPage {
  return { badge: "● question", title: titled(who), body: "Reading the question…", hint: keys("Double tap: back") }
}
