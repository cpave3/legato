// Isolated production-component fixture: no live server, microphone or agent.
import { useState } from "react"
import { createRoot } from "react-dom/client"
import { NativeAgentPanel } from "../components/NativeAgentPanel"
import { useServerProvider } from "../hooks/useServer"
import { useWebSocketProvider } from "../hooks/useWebSocket"
import { applyAppTheme, subscribeTheme } from "../lib/appTheme"
import { VoiceCard, VoiceSheet } from "../voice/VoiceWidgets"
import { initialVoiceView, type VoiceView } from "../voice/session"
import type { RingMode } from "../voice/VoiceRing"
import "@fontsource/geist-sans/latin.css"
import "@fontsource/geist-mono/latin.css"
import "../index.css"

applyAppTheme()
subscribeTheme(applyAppTheme)
window.fetch = async (input) => {
  if (!String(input).startsWith("/api/")) throw new Error(`Non-fixture request: ${input}`)
  return new Response(JSON.stringify(String(input).includes("/snapshot") ? {
    id: "scratch-character", status: "idle", messages: [
      { role: "system", content: "" },
      { role: "user", content: "[legato coordination] The agent is ready for your next instruction." },
      { role: "user", content: "[legato peer other-agent] Shared theme roles keep the interface consistent across devices." },
      { role: "user", content: "Keep the Ocular base and restore its character." },
      { role: "assistant", content: "Purple identifies actions and agents. Green still means activity and success." },
    ], toolCalls: [], pendingPermissions: [],
  } : []), { status: 200 })
}

export function Messages() {
  const server = useServerProvider()
  const ws = useWebSocketProvider(`ws://${location.host}/scratch-character-ws`)
  return <server.Provider value={server}><ws.Provider value={{ ...ws, connected: true, send: () => true, subscribe: () => () => {} }}>
    <main className="flex h-dvh w-full flex-col bg-lw-ground font-lw text-lw-text">
      <NativeAgentPanel agentId="scratch-character" agentKind="chimera" title="Shared theme character" composerEnabled />
    </main>
  </ws.Provider></server.Provider>
}

export function Voice({ card = false }: { card?: boolean }) {
  const [mode, setMode] = useState<RingMode>("listening")
  const view: VoiceView = { ...initialVoiceView, state: mode === "blocked" ? "idle" : mode, partial: "Keep the Ocular base", pause: .4, heard: "Restore its character", activity: "checking the shared theme roles", lines: [{ id: "line", text: "The theme is ready.", url: "" }], speaking: "line" }
  const shared = { main: "erebus", view, onSend: () => {}, onCancel: () => {}, onStop: () => {}, onReply: () => {}, onClose: () => {}, onOpenSettings: () => {} }
  return <main className="min-h-dvh bg-lw-ground p-4 font-lw text-lw-text">
    <label className="mb-6 block text-sm">Fixture voice state
      <select id="voice-mode" className="ml-2 rounded border border-lw-line bg-lw-surface p-2" value={mode} onChange={e => setMode(e.target.value as RingMode)}>
        {["listening", "thinking", "speaking", "idle", ...(card ? [] : ["blocked"])].map(state => <option key={state}>{state}</option>)}
      </select>
    </label>
    <div data-contrast="voice" className="mx-auto max-w-lg">
      {card ? <VoiceCard {...shared} keyLabel="Ctrl+Alt+Shift+V" /> : <VoiceSheet {...shared}
        readiness={{ ready: mode !== "blocked", reason: "Voice is unavailable in this fixture", setting: "reeve.voice.enabled", main: "erebus" }} />}
    </div>
  </main>
}

const surface = new URLSearchParams(location.search).get("view")
createRoot(document.getElementById("root")!).render(surface === "messages" ? <Messages /> : <Voice card={surface === "card"} />)
