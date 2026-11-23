import { useContext, useEffect, useId, useRef, useState } from "react"
import { ChevronDown } from "lucide-react"
import { useMediaQuery } from "../../hooks/useMediaQuery"
import { Button } from "../ui/Button"
import { fieldClass } from "./builderStyles"
import { PendingDrafts } from "./pendingDrafts"
import type { ValueGroup } from "./valuePicker"

// The value picker (LEG-510): the values a field can read, by where they
// come from, inserted at the cursor as plain ${{ … }}.

export function ValuePicker({ label, groups, onPick, onClose, pinned }: { label: string; groups: ValueGroup[]; onPick: (expression: string) => void; onClose: () => void; pinned?: string }) {
  const narrow = useMediaQuery("(max-width: 767px)")
  const first = useRef<HTMLButtonElement>(null)
  useEffect(() => { first.current?.focus() }, [])
  return <div role="dialog" aria-label={`Values for ${label}`} onKeyDown={event => { if (event.key === "Escape") { event.stopPropagation(); onClose() } }}
    className={narrow
      ? "fixed inset-x-0 bottom-0 z-50 flex max-h-[80vh] flex-col gap-2 rounded-t-xl border-t border-lw-line bg-lw-surface p-3 shadow-2xl"
      : "z-30 flex max-h-80 flex-col gap-1 rounded-lg border border-lw-line bg-lw-surface p-1 shadow-xl"}>
    {narrow && pinned !== undefined && <p className="rounded-md border border-lw-line bg-lw-ground p-2 font-lw-mono text-xs text-lw-text-2">{pinned || label}</p>}
    <div className="min-h-0 overflow-y-auto">
      {groups.length === 0 && <p className="px-2 py-3 text-sm text-lw-text-2">Nothing can be read here yet: add an input, or a step before this one.</p>}
      {groups.map(({ group, entries }, g) => <section key={group} aria-label={group} className="flex flex-col py-1">
        <h3 className="px-2 py-1 text-[11px] font-semibold uppercase tracking-wide text-lw-faint">{group}</h3>
        {entries.map((entry, i) => <button key={entry.expression} ref={g === 0 && i === 0 ? first : undefined} type="button" title={entry.expression}
          className={`flex min-h-9 min-w-0 flex-col items-start rounded-md px-2 py-1 text-left hover:bg-lw-raised focus-visible:bg-lw-raised focus-visible:outline-none ${entry.wrongType ? "opacity-50" : ""}`}
          onClick={() => onPick(entry.expression)}>
          <span className="flex w-full min-w-0 items-baseline gap-2 text-sm"><span className="truncate">{entry.name}</span>{entry.type && <span className="text-xs text-lw-faint">{entry.type}</span>}</span>
          {(entry.wrongType || entry.description) && <span className="w-full truncate text-xs text-lw-text-2">{entry.wrongType ? `Not this type: ${entry.wrongType}` : entry.description}</span>}
        </button>)}
      </section>)}
    </div>
    {narrow && <button type="button" className="min-h-9 rounded-md text-sm text-lw-text-2" onClick={onClose}>Cancel</button>}
  </div>
}

const EXPRESSIONS = /\$\{\{\s*([^}]*?)\s*\}\}/g

/** ExpressionText is a text field that takes values from earlier steps:
 * "Insert ⌄", or typing ${{, opens the picker, and the value chosen goes in
 * at the cursor and is written at once. Below it, the field's expressions,
 * each marked with what is wrong with it. It writes on blur or Enter (not
 * in a multi-line field), as CommitField does. */
export function ExpressionText({ name, label, value, onCommit, multiline = false, groups, check, invalid, list }: { name: string; label: string; value: string; onCommit: (value: string) => void; multiline?: boolean; groups: ValueGroup[]; check: (expression: string) => string | null; invalid?: boolean; list?: string }) {
  const [draft, setDraft] = useState(value)
  const [picking, setPicking] = useState(false)
  const field = useRef<HTMLInputElement & HTMLTextAreaElement>(null)
  const cursor = useRef<{ from: number; to: number } | null>(null)
  useEffect(() => setDraft(value), [value])
  const report = useContext(PendingDrafts)
  const id = useId()
  useEffect(() => { report(id, draft !== value) }, [report, id, draft, value])
  useEffect(() => () => report(id, false), [report, id])
  const commit = () => { if (draft !== value) onCommit(draft) }
  const remember = () => { const el = field.current; if (el) cursor.current = { from: el.selectionStart ?? draft.length, to: el.selectionEnd ?? draft.length } }
  const open = () => { remember(); setPicking(true) }
  const pick = (expression: string) => {
    const at = cursor.current ?? { from: draft.length, to: draft.length }
    // A ${{ typed just before the cursor becomes this one.
    const typed = draft.slice(0, at.from).endsWith("${{") ? 3 : 0
    const inserted = `\${{ ${expression} }}`
    const next = draft.slice(0, at.from - typed) + inserted + draft.slice(at.to)
    setDraft(next)
    setPicking(false)
    onCommit(next)
    const caret = at.from - typed + inserted.length
    requestAnimationFrame(() => { const el = field.current; if (el) { el.focus(); el.setSelectionRange(caret, caret) } })
  }
  const change = (next: string, el: HTMLInputElement | HTMLTextAreaElement) => {
    setDraft(next)
    const caret = el.selectionStart ?? next.length
    if (next.slice(0, caret).endsWith("${{") && !draft.slice(0, caret - 1).endsWith("${{")) { cursor.current = { from: caret, to: caret }; setPicking(true) }
  }
  const found = [...draft.matchAll(EXPRESSIONS)].map(match => ({ text: match[0], problem: check(match[1]) }))
  const common = { ref: field, "aria-label": name, "aria-invalid": invalid || found.some(f => f.problem) || undefined, value: draft, onBlur: commit, onSelect: remember, onKeyUp: remember, onClick: remember }
  return <div className="flex flex-col gap-1">
    <div className="relative flex items-start gap-1">
      {multiline
        ? <textarea {...common} className={`${fieldClass} min-h-20 py-1.5 font-lw-mono text-xs`} onChange={event => change(event.target.value, event.target)} />
        : <input {...common} list={list} className={fieldClass} onChange={event => change(event.target.value, event.target)} onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); commit() } }} />}
      <Button type="button" variant="ghost" size="sm" aria-haspopup="dialog" aria-expanded={picking} aria-label={`Insert a value into ${label}`} onMouseDown={remember} onClick={() => (picking ? setPicking(false) : open())}>Insert<ChevronDown size={12} /></Button>
    </div>
    {picking && <ValuePicker label={label} groups={groups} pinned={draft} onPick={pick} onClose={() => { setPicking(false); field.current?.focus() }} />}
    {found.length > 0 && <ul aria-label={`Values in ${label}`} className="flex flex-wrap gap-1 text-[11px]">
      {found.map((f, i) => <li key={i} title={f.problem ?? undefined} className={`rounded px-1 font-lw-mono ${f.problem ? "bg-lw-error-soft text-lw-danger" : "bg-lw-accent-soft text-lw-accent-text"}`}>{f.text}{f.problem && <span className="ml-1 font-lw">— {f.problem}</span>}</li>)}
    </ul>}
  </div>
}

/** UseValueFrom sets a number, switch or choice field to a value read from
 * elsewhere: "Use a value from…". */
export function UseValueFrom({ label, groups, onPick }: { label: string; groups: ValueGroup[]; onPick: (expression: string) => void }) {
  const [picking, setPicking] = useState(false)
  return <div className="flex flex-col gap-1">
    <Button type="button" variant="ghost" size="sm" className="self-start" aria-expanded={picking} onClick={() => setPicking(!picking)}>Use a value from…</Button>
    {picking && <ValuePicker label={label} groups={groups} onPick={expression => { setPicking(false); onPick(`\${{ ${expression} }}`) }} onClose={() => setPicking(false)} />}
  </div>
}
