import { describe, expect, it } from "vitest"
import type { PromptState } from "../agentWire"
import { initialTerminalAnswer, TERMINAL_ANSWER_REQUEST, terminalAnswerReducer, type TerminalAnswerState } from "../terminalAnswer"

const firstTab: PromptState = {
  type: "menu",
  context: "←  ☐ Colour  ☐ Size  ✔ Submit  →\nWhich colour?",
  actions: [{ label: "Red", keys: "Enter" }, { label: "Green", keys: "Down Enter" }],
}
const secondTab: PromptState = {
  type: "menu",
  context: "←  ☒ Colour  ☐ Size  ✔ Submit  →\nWhich size?",
  actions: [{ label: "Small", keys: "Enter" }, { label: "Large", keys: "Down Enter" }],
}
const idle: PromptState = { type: "free_text" }
const requestId = `${TERMINAL_ANSWER_REQUEST}1`

function answering(): TerminalAnswerState {
  const shown = terminalAnswerReducer(initialTerminalAnswer, { type: "read", prompt: firstTab })
  return terminalAnswerReducer(shown, { type: "sent", requestId })
}

describe("terminalAnswerReducer", () => {
  it("keeps the prompt up while its answer is on its way", () => {
    const state = answering()

    expect(state.prompt).toEqual(firstTab)
    expect(state.pending?.requestId).toBe(requestId)
  })

  it("closes once the screen no longer asks", () => {
    const state = terminalAnswerReducer(answering(), { type: "answered", requestId, prompt: idle })

    expect(state.prompt).toEqual(idle)
    expect(state.pending).toBeUndefined()
    expect(state.notice).toBeUndefined()
  })

  it("moves on to the next question the terminal asks", () => {
    const state = terminalAnswerReducer(answering(), { type: "answered", requestId, prompt: secondTab })

    expect(state.prompt).toEqual(secondTab)
    expect(state.pending).toBeUndefined()
  })

  it("says so when the terminal still asks the same", () => {
    const state = terminalAnswerReducer(answering(), { type: "answered", requestId, prompt: { ...firstTab } })

    expect(state.prompt).toEqual(firstTab)
    expect(state.pending).toBeUndefined()
    expect(state.notice).toMatch(/didn't take that answer/)
  })

  it("does not take a read from before the keys landed as the answer failing", () => {
    const state = terminalAnswerReducer(answering(), { type: "read", prompt: { ...firstTab } })

    expect(state.pending?.requestId).toBe(requestId)
    expect(state.notice).toBeUndefined()
  })

  it("takes a changed screen read while answering as the answer landing", () => {
    const state = terminalAnswerReducer(answering(), { type: "read", prompt: secondTab })

    expect(state.prompt).toEqual(secondTab)
    expect(state.pending).toBeUndefined()
  })

  it("shows why an answer was refused and lets it be tried again", () => {
    const state = terminalAnswerReducer(answering(), { type: "failed", requestId, error: "the keys did not reach the terminal: pane is gone" })

    expect(state.prompt).toEqual(firstTab)
    expect(state.pending).toBeUndefined()
    expect(state.notice).toBe("the keys did not reach the terminal: pane is gone")
  })

  it("shows what the screen asks now when the prompt had changed", () => {
    const state = terminalAnswerReducer(answering(), { type: "failed", requestId, error: "the terminal no longer shows that answer", prompt: secondTab })

    expect(state.prompt).toEqual(secondTab)
    expect(state.notice).toMatch(/no longer shows/)
  })

  it("ignores what answers another request", () => {
    const state = terminalAnswerReducer(answering(), { type: "answered", requestId: `${TERMINAL_ANSWER_REQUEST}0`, prompt: idle })

    expect(state.prompt).toEqual(firstTab)
    expect(state.pending?.requestId).toBe(requestId)
  })

  it("clears a notice once the screen asks something else", () => {
    const refused = terminalAnswerReducer(answering(), { type: "failed", requestId, error: "nope" })
    const state = terminalAnswerReducer(refused, { type: "read", prompt: secondTab })

    expect(state.notice).toBeUndefined()
  })

  it("starts over for another agent", () => {
    expect(terminalAnswerReducer(answering(), { type: "reset" })).toEqual(initialTerminalAnswer)
  })
})
