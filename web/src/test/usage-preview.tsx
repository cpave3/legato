// Browser-only fixture for the usage card and its phone button. No live
// Legato API, account or provider is used: /api/usage is answered here.
// ?usage=open opens the details; ?tone=over|failed shows a warning.
import { createRoot } from "react-dom/client"
import { MemoryRouter } from "react-router-dom"
import { QueryClientProvider } from "@tanstack/react-query"
import { AgentPickerBar } from "../components/AgentSelect"
import { AgentSidebar } from "../components/AgentSidebar"
import { UsageStrip } from "../components/UsageStrip"
import { useServerProvider } from "../hooks/useServer"
import type { AgentInfo } from "../hooks/useWebSocket"
import { createQueryClient } from "../lib/query"
import "@fontsource/geist-sans/latin.css"
import "@fontsource/geist-mono/latin.css"
import "../index.css"

const params = new URLSearchParams(location.search)
const tone = params.get("tone")
const now = Date.now()
const ago = (minutes: number) => new Date(now - minutes * 60_000).toISOString()
const ahead = (hours: number) => new Date(now + hours * 3_600_000).toISOString()
const meters = (provider: string) => [
  { window: "five_hour", qualified: `${provider}.five_hour`, short: "5h", long: "5-hour" },
  { window: "weekly", qualified: `${provider}.weekly`, short: "wk", long: "weekly" },
]
const usage = {
  poll_interval_seconds: 300,
  catalog: [
    { provider: "claude-work", name: "Claude Work", meters: meters("claude-work") },
    { provider: "claude", name: "Claude Default", meters: meters("claude") },
    { provider: "codex", name: "Codex", meters: meters("codex") },
  ],
  providers: [
    { provider: "claude-work", state: "ok", stale: false, as_of: ago(2), windows: [
      { meter: "five_hour", used_percent: 38, resets_at: ahead(3) },
      { meter: "weekly", used_percent: tone === "over" ? 93 : 64, resets_at: ahead(70) },
    ] },
    { provider: "claude", state: "ok", stale: false, as_of: ago(2), windows: [
      { meter: "five_hour", used_percent: 12, resets_at: ahead(4) },
      { meter: "weekly", used_percent: 41, resets_at: ahead(90) },
    ] },
    { provider: "codex", state: "ok", stale: false, as_of: ago(2), windows: [
      { meter: "five_hour", used_percent: 3, resets_at: ahead(2) },
      { meter: "weekly", used_percent: 22, resets_at: ahead(120) },
    ] },
  ],
  policy: {
    active: {
      profile: { name: "Claude Work", agent: "claude-code" }, reason: { text: "Rule: weekday" }, since: ago(90),
      ...(tone === "failed" ? { error: "claude is not installed" } : {}),
    },
    profiles: [{ name: "Claude Work", agent: "claude-code" }, { name: "Economy", agent: "codex" }], switches: [],
  },
}
window.fetch = async (input) => {
  if (!String(input).startsWith("/api/")) throw new Error(`Non-fixture request: ${input}`)
  return new Response(JSON.stringify(String(input).endsWith("/api/usage") ? usage : {}), { status: 200, headers: { "Content-Type": "application/json" } })
}

const agent = (task_id: string, task_title: string, activity: string): AgentInfo => ({
  id: task_id.length, task_id, task_title, tmux_session: `legato-${task_id}`, command: "claude", agent_kind: "claude-code",
  status: "running", activity, role: "", started_at: ago(42), working_seconds: 1800, waiting_seconds: 60, error_seconds: 0,
} as AgentInfo)
const agents = [
  agent("a1b2c3d4", "Hide subscription usage widget by default on mobile", "working"),
  agent("e5f6g7h8", "Fix flaky review-loop test", "waiting"),
  agent("i9j0k1l2", "Docs: Even G2 pairing", ""),
]

export function UsagePreview() {
  const server = useServerProvider()
  const phone = window.innerWidth <= 768
  const path = `/agents${params.get("usage") === "open" ? "?usage=open" : ""}`
  return <server.Provider value={server}><QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={[path]}>
    <main className="flex h-dvh w-full flex-col overflow-hidden bg-lw-ground font-lw text-lw-text md:flex-row">
      {phone
        ? <AgentPickerBar agents={agents} selectedId="a1b2c3d4" onSelect={() => {}} onEnsemble={() => {}} onSpawn={() => {}} usage={<UsageStrip compact />} />
        : <AgentSidebar agents={agents} selectedId="a1b2c3d4" onSelect={() => {}} onSpawn={() => {}} onShowEnsemble={() => {}} usage={<UsageStrip className="px-2 pb-2" />} />}
      <section className="flex min-h-0 flex-1 flex-col gap-2 p-4 text-sm text-lw-text-2">
        <p className="text-lw-text">The agent in view</p>
        <p>Its conversation fills the rest of the screen.</p>
      </section>
    </main>
  </MemoryRouter></QueryClientProvider></server.Provider>
}
createRoot(document.getElementById("root")!).render(<UsagePreview />)
