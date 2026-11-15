// Browser-only fixture. No live Legato API or agent process is used.
import { createRoot } from "react-dom/client"
import { NativeAgentPanel } from "../components/NativeAgentPanel"
import { useServerProvider } from "../hooks/useServer"
import { useWebSocketProvider } from "../hooks/useWebSocket"
import fixture from "../../../contracts/chimera-recovery/fixture.json"
import "@fontsource/geist-sans/latin.css"
import "@fontsource/geist-mono/latin.css"
import "../index.css"

const params = new URLSearchParams(location.search)
const recovery = params.has("truncated") ? fixture.truncated : fixture.full
window.fetch = async (input) => {
  if (!String(input).startsWith("/api/")) throw new Error(`Non-fixture request: ${input}`)
  return new Response(JSON.stringify(String(input).includes("/snapshot") ? {
    id: "scratch-recovery", status: "idle", messages: [
      { role: "user", content: "Fixture launch prompt" },
      { role: "assistant", content: recovery.summary },
      { role: "user", content: "Continue with the regression." },
      { role: "assistant", content: "I’ll run the focused test next." },
    ], toolCalls: [],
  } : []), { status: 200 })
}

export function RecoveryPreview() {
  const server = useServerProvider()
  // This is the isolated Vite origin, never the app's /ws proxy.
  const ws = useWebSocketProvider(`ws://${location.host}/scratch-recovery-ws`)
  return <server.Provider value={server}><ws.Provider value={{ ...ws, connected: true, send: () => true, subscribe: () => () => {} }}>
    <main className="flex h-dvh w-full flex-col bg-lw-ground font-lw text-lw-text">
      <NativeAgentPanel agentId="scratch-recovery" agentKind="chimera" title="Build regression" composerEnabled={false} />
    </main>
  </ws.Provider></server.Provider>
}
createRoot(document.getElementById("root")!).render(<RecoveryPreview />)
