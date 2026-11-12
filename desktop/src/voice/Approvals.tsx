import { useEffect, useRef, useState } from "react"
import { invoke } from "@tauri-apps/api/core"
import { listen } from "@tauri-apps/api/event"
import { VoiceRing } from "@web-voice/VoiceRing"
import { approvalLine, detailOf, howToDecide, type ApprovalOption, type WidgetApproval } from "./approvals"

// The approvals waiting on every paired server, above the capsule or the
// card: one line each, opening to what the tray shows, decided with a
// click or signed with the desktop's key. Rust lists them (voice_approvals,
// voice-approvals) and keeps the widget shown while any waits. The widget
// never takes the keyboard, so what needs typing (a note, an answer, a
// secret) is done in the main window.

// What prepare says while the server knows a key and none is plugged in
// (NO_KEY_PLUGGED in src-tauri/src/app.rs), as the sheet waits on it.
const NO_KEY_PLUGGED = "Plug in a key this server knows."
// How often it asks again while waiting for a key, as the sheet does.
export const PLUG_IN_EVERY = 1_500
// How long a line stays to say how it was decided, once the server no
// longer lists it.
export const DECIDED_FOR = 3_000

const keyOf = (w: WidgetApproval) => `${w.server}/${w.approval.id}`
// An error as the person reads it: another decision that came first (the
// server's 409, ErrApprovalDecided) is said as the tray says it.
const text = (e: unknown) => {
  const said = typeof e === "string" ? e : e instanceof Error ? e.message : String(e)
  return said.includes("no longer pending") ? "Already decided somewhere else." : said
}

// compact: the card is up too, so the list keeps to less height (the
// window is at most 420 high).
export function WidgetApprovals({ compact = false }: { compact?: boolean }) {
  const [list, setList] = useState<WidgetApproval[]>([])
  const [open, setOpen] = useState<string | null>(null)
  // Lines decided here, kept a moment after the server stops listing them.
  const [decided, setDecided] = useState<Record<string, { item: WidgetApproval; at: number }>>({})

  useEffect(() => {
    let live = true
    invoke<WidgetApproval[]>("voice_approvals").then((l) => { if (live && l) setList(l) }).catch(() => {})
    const off = listen<WidgetApproval[]>("voice-approvals", (e) => setList(e.payload ?? []))
    return () => { live = false; off.then((f) => f()) }
  }, [])

  const listed = new Set(list.map(keyOf))
  const kept = Object.entries(decided).filter(([k]) => !listed.has(k)).map(([, d]) => d)
  useEffect(() => {
    if (!kept.length) return
    const next = Math.min(...kept.map((d) => d.at)) + DECIDED_FOR - Date.now()
    const timer = setTimeout(() => setDecided((all) => Object.fromEntries(Object.entries(all).filter(([, d]) => Date.now() - d.at < DECIDED_FOR))), Math.max(0, next))
    return () => clearTimeout(timer)
  })

  const rows = [...list, ...kept.map((d) => d.item)]
  if (!rows.length) return null
  const servers = new Set(rows.map((w) => w.server))
  const count = list.length
  return (
    <section
      data-testid="widget-approvals"
      data-no-drag
      aria-label="Approvals"
      // A press never moves the focus, so typing elsewhere carries on.
      onMouseDown={(e) => e.preventDefault()}
      className="voice-ui mb-2 flex w-[412px] max-w-full flex-col overflow-hidden rounded-[20px] border border-[#E8B53A]/40 shadow-[0_18px_48px_rgba(0,0,0,0.5)]"
      style={{ background: "var(--voice-surface)" }}
    >
      <header className="flex items-center gap-2 border-b border-[#26262D] px-[18px] py-2.5">
        <span className="text-[#E8B53A]" aria-hidden="true">◆</span>
        <span className="voice-kicker !text-[#E8B53A]">{count ? `${count} ${count === 1 ? "needs" : "need"} you` : "decided"}</span>
      </header>
      <ul className={`m-0 flex list-none flex-col overflow-y-auto p-1.5 ${compact ? "max-h-[110px]" : "max-h-[290px]"}`}>
        {rows.map((w) => {
          const k = keyOf(w)
          return (
            <Row
              key={k}
              item={w}
              server={servers.size > 1 ? w.server_name : ""}
              open={open === k}
              onToggle={() => setOpen((o) => (o === k ? null : k))}
              onDecided={() => setDecided((all) => ({ ...all, [k]: { item: w, at: Date.now() } }))}
            />
          )
        })}
      </ul>
    </section>
  )
}

type Phase =
  | { kind: "idle" }
  | { kind: "busy" }
  | { kind: "plug-in" }
  | { kind: "touch"; touchId: boolean }
  | { kind: "done"; said: string; signed?: boolean }
  | { kind: "failed"; error: string }

function Row({ item, server, open, onToggle, onDecided }: { item: WidgetApproval; server: string; open: boolean; onToggle: () => void; onDecided: () => void }) {
  const a = item.approval
  const { who, what } = approvalLine(a)
  const how = howToDecide(a)
  const [phase, setPhase] = useState<Phase>({ kind: "idle" })
  const alive = useRef(true)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => { alive.current = false; clearTimeout(timer.current) }, [])
  const set = (p: Phase) => { if (alive.current) setPhase(p) }
  const target = { server: item.server, approval: a.id }

  const done = (said: string, signed?: boolean) => { set({ kind: "done", said, signed }); onDecided() }
  const allowed = (o: ApprovalOption, allows: ApprovalOption[]) => (allows.length > 1 ? `Allowed: ${o.label}` : "Allowed")

  const decide = (decision: "allow" | "deny", option: ApprovalOption | null, allows: ApprovalOption[] = []) => {
    set({ kind: "busy" })
    invoke("voice_approval_decide", { ...target, decision, option: option?.id ?? null })
      .then(() => done(option ? allowed(option, allows) : "Denied"))
      .catch((e) => set({ kind: "failed", error: text(e) }))
  }

  // Signs as the main window's sheet does: prepare (waiting while no key
  // the server knows is plugged in), then the touch or the fingerprint.
  const sign = (option: ApprovalOption) => {
    set({ kind: "busy" })
    const attempt = () => {
      invoke<{ with: "security-key" | "touch-id" }>("prepare", target).then(
        (p) => {
          set({ kind: "touch", touchId: p.with === "touch-id" })
          invoke("sign", { ...target, option: option.id })
            .then(() => done("Signed", true))
            .catch((e) => set({ kind: "failed", error: text(e) }))
        },
        (e) => {
          if (text(e).startsWith(NO_KEY_PLUGGED)) {
            set({ kind: "plug-in" })
            timer.current = setTimeout(() => { if (alive.current) attempt() }, PLUG_IN_EVERY)
          } else {
            set({ kind: "failed", error: text(e) })
          }
        },
      )
    }
    attempt()
  }

  const openInMain = () => { invoke("voice_approval_open", target).catch(() => {}) }
  const openSheet = () => { invoke("voice_approval_sheet", target).catch(() => {}) }
  const busy = phase.kind === "busy" || phase.kind === "plug-in" || phase.kind === "touch"
  const meta = [a.details.task_title, server].filter(Boolean).join(" · ")
  const btn = "h-9 rounded-xl px-3 text-[13px] font-semibold disabled:opacity-50"

  return (
    <li className={`rounded-xl ${open ? "bg-[#0D0D12]" : ""}`} data-testid={`widget-approval-${a.id}`}>
      <button type="button" onClick={onToggle} aria-expanded={open}
        className="flex w-full items-center gap-2.5 rounded-xl bg-transparent px-2.5 py-2 text-left text-[#E6E5EB] hover:bg-[#15141C]">
        <span aria-hidden="true" className={a.signed ? "text-[#6D5EF6]" : "text-[#E8B53A]"}>{a.signed ? "◈" : "◆"}</span>
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-[13px] leading-snug"><span className="text-[#9A99A5]">{who}:</span> <span>{what}</span></span>
          {meta && <span className="voice-mono truncate text-[11px] text-[#6E6D79]">{meta}</span>}
        </span>
        {phase.kind === "done"
          ? <span className="voice-mono shrink-0 text-[11px] text-[#3ECF8E]">{phase.said}</span>
          : <span aria-hidden="true" className="voice-mono shrink-0 text-[11px] text-[#6E6D79]">{open ? "▴" : "▾"}</span>}
      </button>
      {open && (
        <div className="flex flex-col gap-2.5 px-2.5 pb-3 pt-0.5">
          <Detail item={item} />
          {phase.kind === "failed" && <p role="alert" className="m-0 text-[12px] text-[#F87171]">{phase.error}</p>}
          {phase.kind === "plug-in" && <Waiting label="Plug in your key" />}
          {phase.kind === "touch" && <Waiting label={phase.touchId ? "Use Touch ID" : "Touch your key"} />}
          {phase.kind === "done" ? (
            // The line says how it went; a signature also gets the seal, green.
            phase.signed ? <VoiceRing mode="speaking" size={32} label="Signed" /> : null
          ) : how.kind === "open" ? (
            <div className="flex gap-2">
              <button type="button" onClick={openInMain} className={`voice-primary flex-1 ${btn}`}>{how.label}</button>
            </div>
          ) : (
            <div className="flex flex-wrap gap-2">
              {how.allows.map((o, i) => (
                <button key={o.id} type="button" disabled={busy} title={o.description}
                  onClick={() => (how.kind === "sign" ? sign(o) : decide("allow", o, how.allows))}
                  className={`${i === 0 ? "voice-primary" : "voice-secondary"} ${btn}`}>
                  {how.kind === "sign" ? `◈ ${o.label}` : o.label}
                </button>
              ))}
              <button type="button" disabled={busy} onClick={() => decide("deny", null)} className={`voice-secondary ${btn}`}>Deny</button>
              <button type="button" onClick={openInMain} className={`${btn} bg-transparent text-[#9A99A5] hover:text-[#E6E5EB]`}>Deny with a note…</button>
              {how.kind === "sign" && (
                <button type="button" onClick={openSheet} className={`${btn} bg-transparent text-[#A99CFF] hover:text-[#C9C0FF]`}>Open the sheet</button>
              )}
            </div>
          )}
        </div>
      )}
    </li>
  )
}

function Detail({ item }: { item: WidgetApproval }) {
  const lines = detailOf(item.approval)
  if (!lines.length) return null
  return (
    <dl className="m-0 flex flex-col gap-1.5">
      {lines.map((l, i) => (
        <div key={i} className="flex min-w-0 flex-col gap-0.5">
          <dt className={`voice-mono text-[10px] uppercase tracking-[1.5px] ${l.warn ? "text-[#F87171]" : "text-[#6E6D79]"}`}>{l.label}</dt>
          <dd className={`m-0 break-words ${l.code ? "voice-mono max-h-24 overflow-y-auto whitespace-pre-wrap rounded-lg bg-[#111114] px-2 py-1 text-[12px]" : "text-[13px] text-[#C9C8D1]"}`}>{l.value}</dd>
          {l.note && <dd className="m-0 text-[12px] text-[#9A99A5]">{l.note}</dd>}
        </div>
      ))}
    </dl>
  )
}

// The seal waiting on the key: the voice ring, thinking.
function Waiting({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2.5">
      <VoiceRing mode="thinking" size={32} label={label} />
      <span className="text-[13px] text-[#A99CFF]">{label}</span>
    </div>
  )
}
