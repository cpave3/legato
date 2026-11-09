import type { Approval, ChangeSetCall } from "../lib/approvals"
import { cn } from "../lib/utils"

const levelLabel: Record<string, string> = { approve: "a tap", signed: "a signature", verified: "a signature with your fingerprint or PIN" }

function show(value: unknown): string {
  if (value === undefined) return "—"
  if (typeof value === "string") return value === "" ? '""' : value
  return JSON.stringify(value)
}

// callKind is a call's kind as the card shows it: change, risky, or a host
// command run as root.
function callKind(call: ChangeSetCall): { label: string; tone: string } {
  if (call.as_root) return { label: "host as root", tone: "bg-lw-error-soft text-lw-error" }
  if (call.class === "risky") return { label: "risky", tone: "bg-lw-needs-soft text-lw-needs" }
  return { label: "change", tone: "bg-lw-raised text-lw-text-2" }
}

// ChangeSetCard is what a Reeve change set's approval shows: where it comes
// from, its ID, how many changes and the level it needs; then each change
// with its kind, its route or tool, and a before-and-after for a setting;
// and that Legato runs exactly this.
export function ChangeSetCard({ approval }: { approval: Approval }) {
  const details = approval.details
  const calls = details.calls ?? []
  return (
    <div className="mt-2 space-y-2 text-sm md:text-xs" data-testid="change-set">
      <p className="text-lw-text-2">
        The Reeve on {details.instance || "this Legato"} · <span className="font-lw-mono">{details.id}</span> · {calls.length} change{calls.length === 1 ? "" : "s"} · needs {levelLabel[details.level ?? "approve"] ?? details.level}
      </p>
      <ol className="space-y-1.5">
        {calls.map((call, i) => {
          const kind = callKind(call)
          return (
            <li key={i} className="rounded-lg border border-lw-line bg-lw-ground p-2" data-testid={`change-${i + 1}`}>
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-lw-faint">{i + 1}.</span>
                <span className={cn("rounded px-1.5 text-[11px] font-medium uppercase", kind.tone)}>{kind.label}</span>
                <span className="font-lw-mono text-lw-text">{call.kind === "restart" ? "restart Legato" : call.kind === "config_edit" ? "edit config.yaml" : `${call.method} ${call.path}`}</span>
              </div>
              <p className="mt-0.5 text-lw-text-2">{call.description}</p>
              {call.setting && (
                <p className="mt-1 font-lw-mono">
                  <span className="text-lw-error line-through">{show(call.before)}</span>
                  <span aria-hidden="true" className="px-1 text-lw-faint">→</span>
                  <span className="text-lw-live">{show(call.after)}</span>
                </p>
              )}
              {call.diff && (
                <>
                  <pre className="mt-1 max-h-48 overflow-auto font-lw-mono text-[11px]" data-testid="config-diff">
                    {call.diff.split("\n").filter(Boolean).map((line, n) => (
                      <span key={n} className={cn("block", line.startsWith("+ ") ? "text-lw-live" : line.startsWith("- ") ? "text-lw-error" : "text-lw-faint")}>{line}</span>
                    ))}
                  </pre>
                  {call.check && <p className="mt-1 text-lw-text-2">The edited config.yaml {call.check === "loads" ? "loads" : `does not load: ${call.check}`}.</p>}
                </>
              )}
              {call.applies_at_restart && <p className="mt-1 text-lw-needs">Applies at restart.</p>}
              {call.unreached && call.unreached.length > 0 && (
                <p className="mt-1 text-lw-text-2">Does not reach the agents running now: {call.unreached.join(", ")}.</p>
              )}
              {call.body !== undefined && call.body !== null && !call.setting && (
                <pre className="mt-1 max-h-32 overflow-auto whitespace-pre-wrap break-words font-lw-mono text-[11px] text-lw-text-2">{JSON.stringify(call.body, null, 2)}</pre>
              )}
            </li>
          )
        })}
      </ol>
      <p className="text-lw-faint">Legato runs exactly this, in order, stopping at the first failure.</p>
    </div>
  )
}
