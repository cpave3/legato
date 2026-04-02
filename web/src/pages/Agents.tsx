import { useState, useEffect, useCallback } from "react"
import { useWebSocket, type AgentInfo, type WSMessage, type PromptState } from "../hooks/useWebSocket"
import { AgentSidebar } from "../components/AgentSidebar"
import { TerminalPanel } from "../components/TerminalPanel"
import { PromptBar } from "../components/PromptBar"

export function AgentsPage() {
  const { send, subscribe } = useWebSocket()
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [promptState, setPromptState] = useState<PromptState | null>(null)

  // Fetch agents on mount and on agents_changed
  const fetchAgents = useCallback(async () => {
    try {
      const res = await fetch("/api/agents")
      const data: AgentInfo[] = await res.json()
      setAgents(data)
    } catch {
      // ignore fetch errors
    }
  }, [])

  useEffect(() => {
    fetchAgents()
    return subscribe((msg: WSMessage) => {
      if (msg.type === "agent_list" && msg.agents) {
        setAgents(msg.agents)
      }
      if (msg.type === "agents_changed") {
        fetchAgents()
      }
      if (msg.type === "prompt_state" && msg.agent_id === selectedId && msg.prompt) {
        setPromptState(msg.prompt)
      }
    })
  }, [fetchAgents, subscribe, selectedId])

  const handleSelect = useCallback(
    (taskId: string) => {
      if (selectedId) {
        send({ type: "unsubscribe_agent", agent_id: selectedId })
      }
      setSelectedId(taskId)
      setPromptState(null)
      send({ type: "subscribe_agent", agent_id: taskId })
    },
    [selectedId, send]
  )

  const handleSendKeys = useCallback(
    (keys: string) => {
      if (selectedId) {
        send({ type: "send_keys", agent_id: selectedId, keys })
      }
    },
    [selectedId, send]
  )

  const handleDetectPrompt = useCallback(() => {
    if (selectedId) {
      send({ type: "detect_prompt", agent_id: selectedId })
    }
  }, [selectedId, send])

  const handleDisconnect = useCallback(() => {
    if (selectedId) {
      // Unsubscribe removes this client from the stream and size calcs.
      // Setting selectedId to null unmounts TerminalPanel (stops heartbeat).
      send({ type: "unsubscribe_agent", agent_id: selectedId })
    }
    setSelectedId(null)
    setPromptState(null)
  }, [selectedId, send])

  // Mobile: use select dropdown on narrow screens
  const [isMobile, setIsMobile] = useState(false)
  useEffect(() => {
    const check = () => setIsMobile(window.innerWidth <= 768)
    check()
    window.addEventListener("resize", check)
    return () => window.removeEventListener("resize", check)
  }, [])

  const selectedAgent = agents.find((a) => a.task_id === selectedId)

  if (agents.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-zinc-500">No active agents</p>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col overflow-hidden md:flex-row">
      {isMobile ? (
        <div className="border-b border-zinc-800 bg-zinc-950 p-2">
          <select
            className="w-full rounded bg-zinc-900 px-3 py-2 text-sm text-zinc-200 border border-zinc-700"
            value={selectedId ?? ""}
            onChange={(e) => e.target.value && handleSelect(e.target.value)}
          >
            <option value="">Select agent...</option>
            {agents.map((a) => (
              <option key={a.task_id} value={a.task_id}>
                {a.task_id} — {a.task_title || a.command}
              </option>
            ))}
          </select>
        </div>
      ) : (
        <AgentSidebar
          agents={agents}
          selectedId={selectedId}
          onSelect={handleSelect}
        />
      )}

      <div className="flex min-h-0 flex-1 flex-col">
        {selectedId ? (
          <>
            <div className="relative min-h-0 flex-1">
              <TerminalPanel agentId={selectedId} />
            </div>
            <PromptBar
              promptState={promptState}
              onSendKeys={handleSendKeys}
              onDismissPrompt={() => setPromptState(null)}
              onDetectPrompt={handleDetectPrompt}
              onDisconnect={handleDisconnect}
              agentTitle={selectedAgent?.task_title}
              agentActivity={selectedAgent?.activity}
            />
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-zinc-500">Select an agent to view output</p>
          </div>
        )}
      </div>
    </div>
  )
}
