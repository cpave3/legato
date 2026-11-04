import type { Approval } from "./approvals"
import type { NativeQuestion } from "./nativeAgentSync"

// The questions of an approval, as an agent asked them.
export function approvalQuestions(approval: Approval): NativeQuestion[] {
  return (approval.details.questions ?? []) as NativeQuestion[]
}

// What the person has entered for one question so far: the options chosen,
// and an answer in their own words.
export interface QuestionDraft {
  chosen: string[]
  other: string
}

export const emptyDraft: QuestionDraft = { chosen: [], other: "" }

// The answer a draft gives. One choice: a typed answer replaces the option.
// Several: it joins them.
export function answerOf(question: NativeQuestion, draft: QuestionDraft): string[] {
  const other = draft.other.trim()
  if (question.multiSelect) return other ? [...draft.chosen, other] : draft.chosen
  return other ? [other] : draft.chosen
}

// Agents mark the option they suggest with "(Recommended)" at the end of its
// label. The label sent back stays whole; this is only how it is shown.
export function recommended(label: string): { text: string; recommended: boolean } {
  const match = /^(.*\S)\s*\(recommended\)\s*$/i.exec(label)
  return match ? { text: match[1], recommended: true } : { text: label, recommended: false }
}
