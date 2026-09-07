import type { HandoffEvidence as Evidence, HandoffPacket } from "../../lib/handoff"
import { HandoffSources } from "./HandoffSources"

export function HandoffEvidence({ packet, evidence, shownSourceIDs = [] }: { packet: HandoffPacket; evidence: Evidence; shownSourceIDs?: string[] }) {
  return <section className="space-y-2 border-l-2 border-zinc-700 pl-3">
    <h4>Agent-reported check: {evidence.description}</h4>
    <p>{evidence.outcome || "No result attached"} · reporting source (unverified): {evidence.reporter || "Not supplied"}</p>
    <p className="break-all">Command: <code>{evidence.command || "Not supplied"}</code> · revision {evidence.head_sha || "Not supplied"} · recorded {evidence.recorded_at || "Not supplied"}</p>
    {(packet.outdated || evidence.head_sha !== packet.input.head_sha) && <p className="text-amber-300">Outdated check — rerun or inspect at the reviewed revision.</p>}
    <p>Limitations: {evidence.limitations || "Not supplied; no general correctness claim."}</p>
    <HandoffSources packet={packet} ids={evidence.source_ids.filter((id) => !shownSourceIDs.includes(id))} />
  </section>
}
