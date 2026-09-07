import type { HandoffPacket } from "../../lib/handoff"

export function HandoffSources({ packet, ids }: { packet: HandoffPacket; ids: string[] }) {
  return <>{ids.map((id) => {
    const source = packet.sources.find((s) => s.id === id)
    if (!source) return <p key={id}>Source unavailable: {id}</p>
    return <details key={id} className="rounded border border-zinc-700 p-2">
      <summary className="cursor-pointer break-all">Pinned source: {source.path}:{source.line_start}–{source.line_end}</summary>
      <p className="my-2 break-all text-xs text-zinc-400">{source.side} · commit {source.commit_sha} · digest {source.digest}</p>
      <pre className="overflow-x-auto bg-black/30 p-3 text-xs"><code>{source.text}</code></pre>
      <p className="text-xs text-zinc-400">Source reference only. Existence and digest do not establish correctness.</p>
    </details>
  })}</>
}

