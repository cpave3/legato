import { useCallback, useEffect, useReducer, useRef } from "react"
import type { WSMessage } from "../lib/agentWire"
import { initialTerminalAnswer, TERMINAL_ANSWER_REQUEST, terminalAnswerReducer } from "../lib/terminalAnswer"

// How often the panel asks what the agent's terminal is showing, for prompts
// that never reach Legato as requests of their own.
const TERMINAL_PROMPT_POLL_MS = 2000

// How long an answer may go without Legato saying whether it landed.
const TERMINAL_ANSWER_TIMEOUT_MS = 10000

let nextAnswer = 0

// useTerminalPrompt keeps asking what the terminal shows, and answers a
// prompt with keys read off it. A prompt can be up in the terminal without
// the agent having raised anything: an update notice, a trust question, a
// question asked before Legato could take it natively. The server reads the
// screen again and presses only keys the prompt on it offers
// (answer_terminal_prompt); the card stays until the screen moves on.
export function useTerminalPrompt(agentId: string, connected: boolean, send: (message: WSMessage) => unknown) {
  const [state, dispatch] = useReducer(terminalAnswerReducer, initialTerminalAnswer)
  const timers = useRef<number[]>([])
  // The prompt on the card, sent with an answer: keys move from the cursor,
  // so they mean the option chosen only on the screen it was chosen from.
  const shown = useRef(state.prompt)
  useEffect(() => { shown.current = state.prompt }, [state.prompt])

  useEffect(() => {
    dispatch({ type: "reset" })
    if (!connected) return
    const detect = () => send({ type: "detect_prompt", agent_id: agentId })
    detect()
    const timer = window.setInterval(detect, TERMINAL_PROMPT_POLL_MS)
    return () => window.clearInterval(timer)
  }, [agentId, connected, send])

  useEffect(() => () => timers.current.forEach((timer) => window.clearTimeout(timer)), [])

  // What the server said about this agent's terminal; the feed routes
  // prompt_state, terminal_prompt_answered and the errors of answers here.
  const onMessage = useCallback((msg: WSMessage) => {
    if (msg.type === "prompt_state") {
      dispatch({ type: "read", prompt: msg.prompt ?? null })
    } else if (msg.type === "terminal_prompt_answered" && msg.request_id) {
      dispatch({ type: "answered", requestId: msg.request_id, prompt: msg.prompt ?? null })
    } else if (msg.type === "error" && msg.request_id?.startsWith(TERMINAL_ANSWER_REQUEST)) {
      dispatch({ type: "failed", requestId: msg.request_id, error: msg.error || "The answer could not be sent", ...(msg.prompt !== undefined ? { prompt: msg.prompt } : {}) })
    }
  }, [])

  const choose = useCallback((keys: string, text?: string) => {
    const requestId = `${TERMINAL_ANSWER_REQUEST}${++nextAnswer}`
    dispatch({ type: "sent", requestId })
    send({ type: "answer_terminal_prompt", agent_id: agentId, request_id: requestId, keys, prompt: shown.current, ...(text ? { content: text } : {}) })
    timers.current.push(window.setTimeout(() => dispatch({
      type: "failed", requestId, error: "Legato did not say whether the answer landed. Try again, or answer in the terminal.",
    }), TERMINAL_ANSWER_TIMEOUT_MS))
  }, [agentId, send])

  return { ...state, pending: state.pending !== undefined, choose, onMessage }
}
