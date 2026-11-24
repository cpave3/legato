// Browser-only fixture of the agents graph (LEG-615): the ensemble's Graph
// layout on a fixed fleet, for screenshots. No live Legato API or agent
// process is used. ?fleet=busy draws the busier fleet; ?waiting=<id> sets
// one agent waiting, a state-only change.
import { useEffect, useState } from "react"
import { createRoot } from "react-dom/client"
import { QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import { AgentEnsemble } from "../components/AgentEnsemble"
import { useServerProvider } from "../hooks/useServer"
import { useWebSocketProvider } from "../hooks/useWebSocket"
import { createQueryClient } from "../lib/query"
import { busyFleet, screenshotFleet } from "./agentGraphFleet"
import "@fontsource/geist-sans/latin.css"
import "@fontsource/geist-mono/latin.css"
import "../index.css"

const params = new URLSearchParams(location.search)
const fleet = params.get("fleet") === "busy" ? busyFleet(Date.now()) : screenshotFleet()
window.fetch = async (input) => {
  if (!String(input).startsWith("/api/")) throw new Error(`Non-fixture request: ${input}`)
  if (String(input).startsWith("/api/agents/graph")) return new Response(JSON.stringify(fleet.data), { status: 200 })
  return new Response("not in the fixture", { status: 404 })
}
const queries = createQueryClient()

export function AgentGraphPreview() {
  const server = useServerProvider()
  // This is the isolated Vite origin, never the app's /ws proxy.
  const ws = useWebSocketProvider(`ws://${location.host}/scratch-graph-ws`)
  const [agents, setAgents] = useState(fleet.agents)
  // window.flip(id) sets an agent waiting, or working again: a state-only
  // change for the screenshots.
  useEffect(() => {
    (window as unknown as { flip: (id: string) => void }).flip = (id) => setAgents((list) => list.map((agent) => agent.task_id === id ? { ...agent, activity: agent.activity === "waiting" ? "working" : "waiting" } : agent))
  }, [])
  return <server.Provider value={server}><ws.Provider value={{ ...ws, connected: true, send: () => true, subscribe: () => () => {} }}>
    <QueryClientProvider client={queries}><MemoryRouter>
      <main className="flex h-dvh w-full flex-col bg-lw-ground font-lw text-lw-text">
        <AgentEnsemble agents={agents} layout="graph" onOpen={() => {}} onSpawn={() => {}} onShowList={() => {}} onSend={() => {}} />
      </main>
    </MemoryRouter></QueryClientProvider>
  </ws.Provider></server.Provider>
}
createRoot(document.getElementById("root")!).render(<AgentGraphPreview />)
