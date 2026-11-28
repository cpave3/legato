import { useCallback, useEffect, useState } from "react"
import type { PromptState, WSMessage } from "../lib/agentWire"
import { terminalAsks } from "../lib/nativeAgentView"

// How often the panel asks what the agent's terminal is showing, for prompts
// that never reach Legato as requests of their own.
const TERMINAL_PROMPT_POLL_MS = 2000

// useTerminalPrompt is what the agent's terminal is asking, as read off its
// screen, and whether the panel shows it.
export function useTerminalPrompt(awaitingRequest: boolean) {
  const [prompt, setPrompt] = useState<PromptState | null>(null)
  return { prompt, setPrompt, asking: terminalAsks(prompt, awaitingRequest) }
}

// useTerminalPromptPolling keeps asking what the terminal shows, and answers
// a prompt with keys. A prompt can be up in the terminal without the agent
// having raised anything: an update notice, a trust question. The server
// reads the terminal's screen when asked.
export function useTerminalPromptPolling(agentId: string, connected: boolean, send: (message: WSMessage) => unknown, setPrompt: (prompt: PromptState | null) => void) {
  useEffect(() => {
    setPrompt(null)
    if (!connected) return
    const detect = () => send({ type: "detect_prompt", agent_id: agentId })
    detect()
    const timer = window.setInterval(detect, TERMINAL_PROMPT_POLL_MS)
    return () => window.clearInterval(timer)
  }, [agentId, connected, send, setPrompt])

  return useCallback((keys: string) => {
    send({ type: "send_keys", agent_id: agentId, keys })
    // The prompt is answered; what the terminal shows next is read at once.
    setPrompt(null)
    window.setTimeout(() => send({ type: "detect_prompt", agent_id: agentId }), 400)
  }, [agentId, send, setPrompt])
}
