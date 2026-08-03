interface AgentInputHandlers {
  executeBang: (command: string) => void | Promise<void>
  sendMessage: (content: string) => void
}

export function dispatchAgentInput(content: string, handlers: AgentInputHandlers): void | Promise<void> {
  if (content.startsWith("!") && content.length > 1) {
    return handlers.executeBang(content.slice(1))
  }
  handlers.sendMessage(content)
}
