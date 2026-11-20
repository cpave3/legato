import { useEffect, useState } from "react"
import { invoke } from "@tauri-apps/api/core"
import { listen } from "@tauri-apps/api/event"
import { canMarkDone, itemKind, type WidgetItem } from "./items"
import type { FeedEntry } from "./Feed"

// What agents posted for the person in the Legato widget's feed: each
// server's operator items, one line each, opening to what it blocks, with
// Open (the main window on the item) and Done. Rust lists them (voice_items,
// voice-items) and keeps the widget shown while any waits.

const keyOf = (w: WidgetItem) => `${w.server}/${w.item.id}`

export function useItemEntries(): FeedEntry[] {
  const [list, setList] = useState<WidgetItem[]>([])
  useEffect(() => {
    let live = true
    invoke<WidgetItem[]>("voice_items").then((l) => { if (live && l) setList(l) }).catch(() => {})
    const off = listen<WidgetItem[]>("voice-items", (e) => setList(e.payload ?? []))
    return () => { live = false; off.then((f) => f()) }
  }, [])
  const servers = new Set(list.map((w) => w.server))
  return list.map((w) => ({
    kind: "item",
    key: keyOf(w),
    at: Date.parse((w.item.updated_at ?? "").replace(" ", "T")) || 0,
    waiting: true,
    render: ({ open, onToggle }) => <Row item={w} server={servers.size > 1 ? w.server_name : ""} open={open} onToggle={onToggle} />,
  }))
}

function Row({ item, server, open, onToggle }: { item: WidgetItem; server: string; open: boolean; onToggle: () => void }) {
  const it = item.item
  const [phase, setPhase] = useState<"idle" | "busy" | "done" | "failed">("idle")
  const [error, setError] = useState("")
  const meta = [it.task_id, server].filter(Boolean).join(" · ")
  const btn = "h-9 rounded-xl px-3 text-[13px] font-semibold disabled:opacity-50"
  const openInMain = () => { invoke("voice_item_open", { server: item.server, kind: it.navigation.kind, target: it.navigation.target }).catch(() => {}) }
  const markDone = () => {
    setPhase("busy")
    invoke("voice_item_done", { server: item.server, item: it.id, revision: it.source_revision })
      .then(() => setPhase("done"))
      .catch((e) => { setPhase("failed"); setError(typeof e === "string" ? e : String(e)) })
  }
  const question = it.signal_kind === "operator_question"
  return (
    <div className={`rounded-xl ${open ? "bg-[var(--theme-canvas,#0D0D12)]" : ""}`}>
      <button type="button" onClick={onToggle} aria-expanded={open}
        className="flex w-full items-center gap-2.5 rounded-xl bg-transparent px-2.5 py-2 text-left text-[var(--voice-text)] hover:bg-[var(--theme-surface,#15141C)]">
        <span aria-hidden="true" className="text-[var(--voice-amber)]">◇</span>
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-[13px] leading-snug"><span className="text-[var(--voice-dim)]">{itemKind(it)}:</span> <span>{it.title}</span></span>
          <span className="voice-mono truncate text-[11px] text-[var(--voice-faint)]">from an agent{meta && ` · ${meta}`}</span>
        </span>
        {phase === "done"
          ? <span className="voice-mono shrink-0 text-[11px] text-[var(--theme-success,#3ECF8E)]">Done</span>
          : <span aria-hidden="true" className="voice-mono shrink-0 text-[11px] text-[var(--voice-faint)]">{open ? "▴" : "▾"}</span>}
      </button>
      {open && (
        <div className="flex flex-col gap-2.5 px-2.5 pb-3 pt-0.5">
          {it.reason && <p className="m-0 text-[13px] text-[var(--theme-text,#C9C8D1)]">{it.reason}</p>}
          {phase === "failed" && <p role="alert" className="m-0 text-[12px] text-[var(--theme-error,#F87171)]">{error}</p>}
          {phase !== "done" && (
            <div className="flex flex-wrap gap-2">
              {question
                ? <button type="button" onClick={openInMain} className={`voice-primary flex-1 ${btn}`}>Answer in Legato</button>
                : <button type="button" onClick={openInMain} className={`voice-secondary ${btn}`}>Open</button>}
              {canMarkDone(it) && <button type="button" disabled={phase === "busy"} onClick={markDone} className={`voice-primary ${btn}`}>Done</button>}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
