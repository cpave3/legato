// Isolated rendering fixture: every API and WebSocket operation stays local.
import { createRoot } from "react-dom/client"
import { NativeAgentPanel } from "../components/NativeAgentPanel"
import { useServerProvider } from "../hooks/useServer"
import { useWebSocketProvider } from "../hooks/useWebSocket"
import { watchBatchBody, watchRequests } from "./watchBatch"
import "@fontsource/geist-sans/latin.css"
import "@fontsource/geist-mono/latin.css"
import "../index.css"

const fetchResource = window.fetch.bind(window)
window.fetch = async input => {
  if (!String(input).startsWith("/api/")) return fetchResource(input)
  return new Response(JSON.stringify(String(input).includes("/snapshot") ? {
    id: "watch-preview", status: "idle", toolCalls: [], ...watchRequests,
    messages: [
      { role: "system", content: "" },
      { role: "user", content: "Check the build and docs agents." },
      { role: "assistant", content: "I will check their replies before I propose the next step." },
      { role: "user", content: '[legato agent] An agent you watch:\n- T-3 (Tests) added a note: "Focused checks passed."' },
      { role: "user", content: `[legato agent] ${watchBatchBody}` },
    ],
  } : []), { status: 200 })
}
export function Preview() {
  const server = useServerProvider()
  // This isolated Vite path is not the app's /ws proxy.
  const ws = useWebSocketProvider(`ws://${location.host}/scratch-watch-ws`)
  return <server.Provider value={server}><ws.Provider value={{ ...ws, connected: true, send: () => true, subscribe: () => () => {} }}>
    <main className="flex h-dvh w-full flex-col bg-lw-ground font-lw text-lw-text">
      <NativeAgentPanel agentId="watch-preview" agentKind="chimera" title="The Reeve · watched agents" composerEnabled={false} />
    </main>
  </ws.Provider></server.Provider>
}
createRoot(document.getElementById("root")!).render(<Preview />)
