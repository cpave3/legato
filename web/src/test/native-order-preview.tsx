// Isolated browser fixture: no live Legato backend or agent session.
import { createRoot } from "react-dom/client"
import { NativeAgentPanel } from "../components/NativeAgentPanel"
import { useServerProvider } from "../hooks/useServer"
import { type WSMessage, useWebSocketProvider } from "../hooks/useWebSocket"
import type { NativeSnapshot } from "../lib/nativeAgentSync"
import "@fontsource/geist-sans/latin.css"
import "@fontsource/geist-mono/latin.css"
import "../index.css"

let finished = new URLSearchParams(location.search).has("finished")
const listeners = new Set<(event: WSMessage) => void>()
const history = [
  { role: "assistant", content: "Earlier context" },
  { role: "assistant", content: [{ type: "tool-call" }] },
]
const calls = Array.from({ length: 40 }, (_, index) => ({
  callId: `call-${index}`, name: index % 5 === 0 ? "spawn_agent" : "read",
  args: index % 5 === 0 ? { purpose: `Earlier worker ${index}` } : { path: `file-${index}.txt` },
  result: index % 5 === 0 ? { subagent_id: `child-${index}`, reason: "stop", result: "Earlier work finished." } : { content: "Earlier file." },
  startedAt: index + 1, endedAt: index + 2,
}))
let snapshotRequests = 0
const fetchResource = window.fetch.bind(window)
window.fetch = async input => {
  if (!String(input).startsWith("/api/")) return fetchResource(input)
  if (String(input).includes("/snapshot")) document.body.dataset.snapshots = String(++snapshotRequests)
  const snapshot: NativeSnapshot = { id: "scratch-order", status: "idle", messages: [
    ...history,
    ...(finished ? [{ role: "user", content: "done?" }, { role: "assistant", content: "Implementation is done." }] : []),
  ], toolCalls: calls }
  return new Response(JSON.stringify(String(input).includes("/snapshot") ? snapshot : []), { status: 200 })
}
function emit(event: object) {
  for (const listener of listeners) listener({ type: "chimera_event", agent_id: "scratch-order", content: JSON.stringify(event) })
}
export function Preview() {
  const server = useServerProvider()
  const ws = useWebSocketProvider(`ws://${location.host}/scratch-order-ws`)
  return <server.Provider value={server}><ws.Provider value={{ ...ws, connected: true, send: () => true, subscribe: handler => {
    listeners.add(handler)
    return () => { listeners.delete(handler) }
  } }}>
    <main className="flex h-dvh w-full flex-col bg-lw-ground font-lw text-lw-text">
      <button onClick={() => {
        emit({ eventId: "question", type: "user_message", content: "done?" })
        emit({ eventId: "delta", type: "assistant_text_delta", id: "answer", delta: "Implementation is done." })
        emit({ eventId: "answer", type: "assistant_text_done", id: "answer", text: "Implementation is done." })
      }}>Stream exchange</button>
      <button onClick={() => { finished = true; emit({ eventId: "finished", type: "run_finished" }) }}>Finish and refresh</button>
      <NativeAgentPanel agentId="scratch-order" agentKind="chimera" title="Ordering regression" composerEnabled={false} />
    </main>
  </ws.Provider></server.Provider>
}
createRoot(document.getElementById("root")!).render(<Preview />)
