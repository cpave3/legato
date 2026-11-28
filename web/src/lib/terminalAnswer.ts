import type { PromptState } from "./agentWire"

// Request IDs of answers to a prompt read off an agent's terminal
// (answer_terminal_prompt), so the server's reply finds its way back.
export const TERMINAL_ANSWER_REQUEST = "terminal-answer:"

// What the panel knows of a prompt its agent's terminal shows, and of the
// answer it sent to it. The card stays up until the screen no longer asks
// what was answered: it closes when nothing is asked, moves on when the
// terminal asks something else (the next question of a form), and says so
// when the terminal still asks the same.
export interface TerminalAnswerState {
  prompt: PromptState | null
  pending?: { requestId: string; answered: PromptState | null }
  notice?: string
}

export type TerminalAnswerEvent =
  | { type: "reset" }
  // The screen was read (prompt_state).
  | { type: "read"; prompt: PromptState | null }
  | { type: "sent"; requestId: string }
  // The keys went in; prompt is the screen read after them.
  | { type: "answered"; requestId: string; prompt: PromptState | null }
  | { type: "failed"; requestId: string; error: string; prompt?: PromptState | null }

export const initialTerminalAnswer: TerminalAnswerState = { prompt: null }

export const TERMINAL_ANSWER_NOT_TAKEN = "The terminal didn't take that answer. Try again, or answer in the terminal."

function samePrompt(a: PromptState | null, b: PromptState | null): boolean {
  if (a === null || b === null) return a === b
  return a.type === b.type && (a.context ?? "") === (b.context ?? "") && JSON.stringify(a.actions ?? []) === JSON.stringify(b.actions ?? [])
}

export function terminalAnswerReducer(state: TerminalAnswerState, event: TerminalAnswerEvent): TerminalAnswerState {
  switch (event.type) {
  case "reset":
    return initialTerminalAnswer
  case "read":
    if (samePrompt(state.prompt, event.prompt)) return state
    // A changed screen is the answer landing, or a new prompt.
    return { prompt: event.prompt }
  case "sent":
    return { prompt: state.prompt, pending: { requestId: event.requestId, answered: state.prompt } }
  case "answered":
    if (state.pending?.requestId !== event.requestId) return state
    if (samePrompt(state.pending.answered, event.prompt)) return { prompt: state.prompt, notice: TERMINAL_ANSWER_NOT_TAKEN }
    return { prompt: event.prompt }
  case "failed":
    if (state.pending?.requestId !== event.requestId) return state
    return { prompt: event.prompt === undefined ? state.prompt : event.prompt, notice: event.error }
  }
}
