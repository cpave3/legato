// Compacting a native agent: the agent's tool compacts its own context
// (Claude Code's /compact, Codex's compaction, Chimera's), through
// POST /api/agents/{id}/compact. The agent's events say how it went.

import { apiFetch } from "./api"

// compactCommand reads "/compact" and "/compact <focus>" typed in the
// composer: the focus is what the summary should keep, which only Claude
// Code takes. Anything else is null.
export function compactCommand(text: string): { instructions: string } | null {
  const match = /^\/compact(?:\s+([\s\S]*))?$/.exec(text.trim())
  return match ? { instructions: (match[1] ?? "").trim() } : null
}

export type Compaction =
  | { status: "running"; byTool: boolean }
  | { status: "done"; before?: number; after?: number }
  | { status: "failed"; error: string }

type CompactionEvent = { type?: string; reason?: string; tokensBefore?: number; tokensAfter?: number; error?: string }

// nextCompaction is the compaction as an agent event leaves it.
export function nextCompaction(current: Compaction | undefined, event: CompactionEvent): Compaction | undefined {
  switch (event.type) {
    case "compaction_started":
      return { status: "running", byTool: event.reason === "threshold" || event.reason === "overflow" }
    case "compaction_finished":
      return { status: "done", before: event.tokensBefore, after: event.tokensAfter }
    case "compaction_failed":
      return { status: "failed", error: event.error || "the agent's tool did not say why" }
    case "session_started":
      // The next turn: a finished compaction's notice has been read. Codex
      // runs a compaction as a turn, so a running one stays.
      return current?.status === "running" ? current : undefined
    default:
      return current
  }
}

function tokens(count: number): string {
  if (count < 1000) return String(count)
  return `${(count / 1000).toFixed(1).replace(/\.0$/, "")}k`
}

// compactionNotice is the line the panel shows for a compaction.
export function compactionNotice(compaction: Compaction | undefined): string {
  if (!compaction) return ""
  switch (compaction.status) {
    case "running":
      return compaction.byTool ? "Compacting the conversation (the context was nearly full)…" : "Compacting the conversation…"
    case "done":
      return compaction.before !== undefined && compaction.after !== undefined
        ? `Compacted: ${tokens(compaction.before)} → ${tokens(compaction.after)} tokens`
        : "Compacted"
    case "failed":
      return `Compaction failed: ${compaction.error}`
  }
}

// compactAgent asks Legato to compact an agent. It resolves once the agent's
// tool took the request, and rejects with Legato's reason when it did not:
// the agent is working, or its tool cannot compact or take the focus.
export async function compactAgent(baseUrl: string, agentId: string, instructions: string): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(agentId)}/compact`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(instructions ? { instructions } : {}),
  })
  if (res.ok) return
  let reason = `Legato did not compact the agent (${res.status})`
  try {
    const body = await res.json()
    if (typeof body?.error === "string" && body.error) reason = body.error
  } catch {
    // The status says enough.
  }
  throw new Error(reason)
}
