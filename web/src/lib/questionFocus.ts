import type { NativeQuestion } from "./nativeAgentSync"
import { answerOf, emptyDraft, type QuestionDraft } from "./questionDrafts"

// Focus mode's steps through one approval's questions, and its keys, apart
// from how they are drawn.

export type FocusScreen = "question" | "review"

// Where focus mode stands: a question, by index, or the review of them all.
export interface FocusStep {
  screen: FocusScreen
  q: number
}

// Every approval's drafts, by approval ID then question ID.
export type FocusDrafts = Record<string, Record<string, QuestionDraft>>

export function draftIn(drafts: FocusDrafts, approvalId: string, questionId: string): QuestionDraft {
  return drafts[approvalId]?.[questionId] ?? emptyDraft
}

export function withDraft(drafts: FocusDrafts, approvalId: string, questionId: string, update: (draft: QuestionDraft) => QuestionDraft): FocusDrafts {
  return { ...drafts, [approvalId]: { ...drafts[approvalId], [questionId]: update(draftIn(drafts, approvalId, questionId)) } }
}

// The answers one approval's drafts give, by question ID.
export function answersOf(questions: NativeQuestion[], drafts: FocusDrafts, approvalId: string): Record<string, string[]> {
  return Object.fromEntries(questions.map((one) => [one.id, answerOf(one, draftIn(drafts, approvalId, one.id))]))
}

// Choosing an option: one of several toggles; the only choice replaces the
// draft, a typed answer included.
export function chooseOption(question: NativeQuestion, draft: QuestionDraft, label: string): QuestionDraft {
  if (!question.multiSelect) return { chosen: [label], other: "" }
  const chosen = draft.chosen.includes(label) ? draft.chosen.filter((l) => l !== label) : [...draft.chosen, label]
  return { ...draft, chosen }
}

// The option an arrow key moves to from the one chosen, round the ends; from
// none, the first going down and the last going up.
export function movedIndex(question: NativeQuestion, draft: QuestionDraft, by: number): number {
  const n = question.options.length
  const at = question.options.findIndex((o) => draft.chosen.includes(o.label))
  if (at >= 0) return (at + by + n) % n
  return by > 0 ? 0 : n - 1
}

// The preview of the chosen option that has one.
export function chosenPreview(question: NativeQuestion, draft: QuestionDraft): string | undefined {
  return question.options.find((o) => o.preview && draft.chosen.includes(o.label))?.preview
}

// Where Next goes: on to the next question, to the review after the last of
// several, or "send" from the review or from an approval's only question.
export function nextStep({ screen, q }: FocusStep, count: number): FocusStep | "send" {
  if (screen === "review") return "send"
  if (q < count - 1) return { screen: "question", q: q + 1 }
  return count === 1 ? "send" : { screen: "review", q }
}

// Where Back goes, or null at the first question.
export function backStep({ screen, q }: FocusStep, count: number): FocusStep | null {
  if (screen === "review") return { screen: "question", q: count - 1 }
  return q > 0 ? { screen: "question", q: q - 1 } : null
}

export function nextLabel({ screen, q }: FocusStep, count: number, agent: string): string {
  if (screen === "review") return `Send answers to ${agent}`
  if (q < count - 1) return "Next question"
  return count === 1 ? "Send answer" : "Review answers"
}

export function missingMessage(count: number): string {
  return count === 1 ? "Choose an answer first." : "This one still needs an answer."
}

export type FocusVerb = "next" | "back" | "leave" | "send" | "release" | "type" | "choose" | "move"

// What a key does: a verb, with the option index for choose and the
// direction for move.
export interface FocusKeyAction {
  do: FocusVerb
  arg?: number
}

// Keys typed in the box for an answer in the person's own words: Enter goes
// on, Escape gives the keys back to the dialog.
export function ownKeyAction(key: string): FocusKeyAction | null {
  if (key === "Enter") return { do: "next" }
  if (key === "Escape") return { do: "release" }
  return null
}

// Keys that answer the question on screen: a digit chooses, an arrow moves
// through a single choice, and / goes to the box when there is one.
function questionKeyAction(key: string, multiSelect: boolean, canType: boolean): FocusKeyAction | null {
  if (/^[1-9]$/.test(key)) return { do: "choose", arg: Number(key) - 1 }
  if (!multiSelect && (key === "ArrowDown" || key === "ArrowUp")) return { do: "move", arg: key === "ArrowDown" ? 1 : -1 }
  if (key === "/" && canType) return { do: "type" }
  return null
}

export interface FocusKey {
  key: string
  // Cmd or Ctrl held.
  mod: boolean
  // The key went to a button, which Enter presses instead.
  onButton: boolean
  screen: FocusScreen
  multiSelect: boolean
  canType: boolean
}

// Keys pressed anywhere in the dialog but the box.
export function dialogKeyAction({ key, mod, onButton, screen, multiSelect, canType }: FocusKey): FocusKeyAction | null {
  if (key === "Escape") return { do: "leave" }
  if (mod && key === "Enter") return { do: "send" }
  const answer = screen === "question" ? questionKeyAction(key, multiSelect, canType) : null
  if (answer) return answer
  if (key === "Enter" && !onButton) return { do: "next" }
  if (key === "ArrowLeft") return { do: "back" }
  return null
}
