// Browser-only fixture for an agent's chat header with everything in it at
// once (LEG-705): the request, a stage with its reason, sub-agents, a studio,
// related work, artifacts, a mode and a sync, a long title and an engram,
// opened from a workflow run, under the phone's agent picker. No live Legato
// API is used.
// ?state=running draws it without the request; ?few draws an agent with
// only a stage, a pull request and related work, as most are.
import { createRoot } from "react-dom/client"
import { MemoryRouter } from "react-router-dom"
import { QueryClientProvider } from "@tanstack/react-query"
import { AgentChatHeader, type AgentChatState } from "../components/AgentChatHeader"
import { AgentPickerBar } from "../components/AgentSelect"
import { AgentSidebar } from "../components/AgentSidebar"
import { useServerProvider } from "../hooks/useServer"
import type { AgentInfo, BackgroundJob } from "../lib/agentWire"
import { createQueryClient } from "../lib/query"
import "@fontsource/geist-sans/latin.css"
import "@fontsource/geist-mono/latin.css"
import "../index.css"

const params = new URLSearchParams(location.search)
const state = (params.get("state") ?? "needs-you") as AgentChatState
const few = params.has("few")
const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString()
window.fetch = async (input) => {
  if (!String(input).startsWith("/api/")) throw new Error(`Non-fixture request: ${input}`)
  return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } })
}

const title = "Web UI agent view on mobile: the bar holding related items is overcrowded and its contents overlap"
const agent = {
  id: 1, task_id: "LEG-705", task_title: title, tmux_session: "legato-LEG-705", command: "claude", agent_kind: "claude-code",
  status: "running", activity: state === "needs-you" ? "waiting" : "working", role: "", started_at: ago(42),
  working_seconds: 1800, waiting_seconds: 60, error_seconds: 0, stage: "implementing",
} as AgentInfo
const jobs: BackgroundJob[] = [
  { kind: "subagent", id: "s1", label: "Explore: the header", state: "working", started_at: ago(5) },
  { kind: "subagent", id: "s2", label: "Explore: the picker", state: "background", started_at: ago(4) },
  { kind: "bash", id: "b1", label: "pnpm run build", parent: "s2", started_at: ago(3) },
]
const noop = () => {}

export function AgentHeaderPreview() {
  const server = useServerProvider()
  const phone = window.innerWidth <= 768
  return <server.Provider value={server}><QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={["/agents"]}>
    <main className="flex h-dvh w-full flex-col overflow-hidden bg-lw-ground font-lw text-lw-text md:flex-row">
      {phone
        ? <AgentPickerBar agents={[agent]} selectedId="LEG-705" onSelect={noop} onEnsemble={noop} onSpawn={noop} />
        : <AgentSidebar agents={[agent]} selectedId="LEG-705" onSelect={noop} onSpawn={noop} onShowEnsemble={noop} />}
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        <AgentChatHeader
          state={state}
          time="12m"
          title={few ? "Fix the header on a phone" : title}
          taskId="LEG-705"
          agentKind="claude-code"
          engram="legato-builder"
          native
          pullRequest={1456}
          stage={{ agentId: "LEG-705", stage: "implementing", reason: few ? "" : "fixing the header on a phone", source: "agent" }}
          backgroundJobs={few ? undefined : jobs}
          subagents={few ? undefined : { total: 2, running: 1, view: "inline", onView: noop }}
          panels={{ open: null, related: { count: 4, live: true }, artifacts: { count: few ? 0 : 3, fresh: few ? 0 : 1 }, onSelect: noop, studio: few ? undefined : { onOpen: noop } }}
          onShowRequest={noop}
          askReeve="LEG-705 waits for the operator"
          workflowRun={few ? undefined : "wr-1"}
          menu={[{ label: "Rename", onSelect: noop }, { label: "Stop agent", onSelect: noop, danger: true }]}
          notes={few ? undefined : <><span className="rounded bg-lw-raised px-1.5 font-lw-mono">plan</span><span className="text-lw-link">Syncing…</span></>}
        />
        <section className="flex min-h-0 flex-1 flex-col gap-2 p-4 text-sm text-lw-text-2">
          <p className="text-lw-text">The conversation</p>
        </section>
      </div>
    </main>
  </MemoryRouter></QueryClientProvider></server.Provider>
}
createRoot(document.getElementById("root")!).render(<AgentHeaderPreview />)
