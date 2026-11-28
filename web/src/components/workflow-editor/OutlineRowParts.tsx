import { createElement, type ReactNode } from "react"
import { AlertTriangle, ArrowDown, ArrowUp, Code2, Copy, MoreVertical, Plus, Trash2 } from "lucide-react"
import { addressOf } from "../../lib/workflowAddress"
import { Button } from "../ui/Button"
import { iconOf, type Row } from "./outlineRows"
import type { StepValue } from "./stepForms"
import { connectorOf } from "./stepRows"
import { readersOf, type StepList, type useOutlineEditing } from "./useOutlineEditing"

function RowLabel({ row }: { row: Row }) {
  if (!row.sentence) return <>{row.label}</>
  return <>{row.sentence.map((part, i) => part.strong ? <strong key={i} className="font-semibold text-lw-text">{part.text}</strong> : <span key={i}>{part.text}</span>)}</>
}

// RowTags are what a row says at its end: its type when it has no summary,
// whether its trigger is paused or on, and whether it is edited as YAML or
// has problems.
function RowTags({ row }: { row: Row }) {
  return <>
    {row.detail && !row.summary && <span className="truncate text-xs text-lw-faint">{row.detail}</span>}
    {row.paused && <span className="rounded border border-lw-line px-1 text-[11px] text-lw-text-2">paused</span>}
    {row.on && <span className="rounded bg-lw-live-soft px-1 text-[11px] text-lw-live">on</span>}
    {row.problem && <span className="flex items-center gap-1 rounded border border-lw-line px-1 text-[11px] text-lw-text-2"><Code2 size={11} />Edited as YAML</span>}
    {row.problems > 0 && <span aria-label={`${row.problems} ${row.problems === 1 ? "problem" : "problems"}`} className="flex items-center gap-0.5 text-[11px] text-lw-danger"><AlertTriangle size={11} />{row.problems}</span>}
  </>
}

// StepIcon is a step type's icon in its row.
function StepIcon({ type }: { type: string }) {
  return createElement(iconOf(type), { size: 14, "aria-hidden": true, className: "shrink-0 text-lw-faint" })
}

/** RowButton is one row of the outline, which chooses it. */
export function RowButton({ row, selected, number, onChoose }: { row: Row; selected: boolean; number?: number; onChoose: () => void }) {
  return <button type="button" data-address={addressOf(row.selection)} aria-current={selected ? "true" : undefined}
    className={`flex min-h-10 w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm ${selected ? "bg-lw-raised text-lw-text" : "text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"}`}
    onClick={onChoose}>
    {number !== undefined && <span className="w-4 shrink-0 text-right text-xs text-lw-faint">{number}</span>}
    {row.type ? <StepIcon type={row.type} /> : null}
    <span className="flex min-w-0 flex-1 flex-col">
      <span className="truncate"><RowLabel row={row} /></span>
      {row.summary && <span className="truncate text-xs text-lw-faint">{row.summary}</span>}
    </span>
    <RowTags row={row} />
  </button>
}

type Editor = ReturnType<typeof useOutlineEditing>
export type StepRowEditor = Pick<Editor, "adding" | "setAdding" | "menu" | "setMenu" | "dragged" | "setDragged" | "moveTo" | "removeIt" | "duplicate" | "confirmRemove" | "keep">

type StepRowProps = {
  row: Row
  list: StepList
  editing: boolean
  narrow: boolean
  rowButton: (row: Row, number?: number) => ReactNode
  palette: ReactNode
  editor: StepRowEditor
}

// The line of a connector, by how the step starts.
function connectorLine(kind: string): string {
  const first = kind === "first" ? "invisible " : ""
  if (kind === "agent_ended") return `${first}border-dashed border-lw-line-strong`
  return `${first}${kind === "manual" ? "border-lw-needs" : "border-lw-line"}`
}

// StepConnector is the line before a top-level step that says how it
// starts, and inserts a step there.
function StepConnector({ placed, index, title, editing, narrow, onAdd }: { placed: StepList["placed"]; index: number; title: string; editing: boolean; narrow: boolean; onAdd: () => void }) {
  const connector = connectorOf(placed.map(p => (p ?? {}) as Record<string, unknown>), index)
  return <div className={`flex min-h-7 items-center gap-2 pl-5 text-[11px] ${connector.kind === "manual" ? "text-lw-needs" : "text-lw-faint"}`}>
    <span aria-hidden className={`h-5 border-l ${connectorLine(connector.kind)}`} />
    <span data-start={connector.kind} className="flex-1">{connector.text}</span>
    {editing && <button type="button" aria-label={`Add a step before ${title}`} className={`rounded px-1 text-lw-text-2 hover:bg-lw-raised hover:text-lw-text ${narrow ? "" : "opacity-0 focus:opacity-100 group-hover:opacity-100"}`} onClick={onAdd}><Plus size={13} /></button>}
  </div>
}

// StepMenu is a step's menu: duplicate, move up or down, delete.
function StepMenu({ id, step, index, last, editor }: { id: string; step: StepValue; index: number; last: number; editor: StepRowEditor }) {
  const then = (act: () => void) => () => { editor.setMenu(null); act() }
  return <div role="menu" aria-label={`Step ${id}`} className="absolute right-0 top-full z-20 flex w-44 flex-col rounded-md border border-lw-line bg-lw-surface p-1 text-sm shadow-xl" onKeyDown={event => { if (event.key === "Escape") editor.setMenu(null) }}>
    <button type="button" role="menuitem" className="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-lw-raised" onClick={then(() => editor.duplicate(step, index))}><Copy size={13} />Duplicate</button>
    <button type="button" role="menuitem" disabled={index === 0} className="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-lw-raised disabled:opacity-40" onClick={then(() => editor.moveTo(id, index - 1))}><ArrowUp size={13} />Move up</button>
    <button type="button" role="menuitem" disabled={index === last} className="flex items-center gap-2 rounded px-2 py-1.5 text-left hover:bg-lw-raised disabled:opacity-40" onClick={then(() => editor.moveTo(id, index + 1))}><ArrowDown size={13} />Move down</button>
    <button type="button" role="menuitem" className="flex items-center gap-2 rounded px-2 py-1.5 text-left text-lw-danger hover:bg-lw-raised" onClick={then(() => editor.removeIt(id))}><Trash2 size={13} />Delete</button>
  </div>
}

/** ReadersConfirm asks before removing a step its readers read. */
export function ReadersConfirm({ id, readers, className, onRemove, onKeep }: { id: string; readers: string[]; className: string; onRemove: () => void; onKeep: () => void }) {
  return <div role="alertdialog" aria-label={`Remove step ${id}`} className={className}>
    <span>{readers.join(", ")} {readers.length === 1 ? "reads" : "read"} this step's outputs.</span>
    <Button type="button" size="sm" variant="destructive" onClick={onRemove}>Remove anyway</Button>
    <Button type="button" size="sm" variant="ghost" onClick={onKeep}>Keep it</Button>
  </div>
}

// stepIndexOf is a top-level step's place: a step with no ID is #<index>,
// its place (stepNodes).
const stepIndexOf = (placed: StepList["placed"], id: string) => /^#\d+$/.test(id) ? Number(id.slice(1)) : placed.findIndex(step => step?.id === id)

// StepLine is a top-level step's row button, with its menu.
function StepLine({ row, id, index, list, editing, rowButton, editor }: Omit<StepRowProps, "narrow" | "palette"> & { id: string; index: number }) {
  const { placed } = list
  const { menu } = editor
  return <div className="relative flex min-w-0 items-center">
    {rowButton(row, index + 1)}
    {editing && <button type="button" aria-label={`Actions for step ${id}`} aria-expanded={menu === id} className="ml-1 rounded p-1 text-lw-text-2 hover:bg-lw-raised" onClick={() => editor.setMenu(menu === id ? null : id)}><MoreVertical size={14} /></button>}
    {menu === id && <StepMenu id={id} step={(placed[index] ?? {}) as StepValue} index={index} last={placed.length - 1} editor={editor} />}
  </div>
}

// TopStepRow is a top-level step's row: the connector before it, the
// picker when a step is being added there, its line, and the readers'
// confirm; it is dragged onto another's place to move it there.
function TopStepRow(props: StepRowProps & { id: string }) {
  const { row, id, list, editing, narrow, palette, editor } = props
  const index = stepIndexOf(list.placed, id)
  const { adding, dragged } = editor
  const onDrop = (event: React.DragEvent) => {
    event.preventDefault()
    if (dragged && dragged !== id) editor.moveTo(dragged, index)
    editor.setDragged(null)
  }
  return <li className="group flex min-w-0 flex-col"
    draggable={editing && !narrow} onDragStart={() => editor.setDragged(id)} onDragOver={event => event.preventDefault()} onDrop={onDrop}>
    <StepConnector placed={list.placed} index={index} title={row.label} editing={editing} narrow={narrow} onAdd={() => editor.setAdding({ kind: "step", at: index })} />
    {adding?.kind === "step" && adding.at === index && palette}
    <StepLine {...props} index={index} />
    {editor.confirmRemove === id && <ReadersConfirm id={id} readers={readersOf(list.steps, id).map(other => String(other.id))} className="mx-2 my-1 flex flex-wrap items-center gap-2 rounded border border-lw-needs p-2 text-xs" onRemove={() => editor.removeIt(id)} onKeep={editor.keep} />}
  </li>
}

/** StepRow is a step's row: a top-level step's with the connector before
 * it that says how it starts and inserts a step there, its menu, and
 * dragging it to another's place. Nested steps are plain rows. */
export function StepRow(props: StepRowProps) {
  const { row } = props
  if (row.selection.kind !== "step" || (row.depth ?? 0) > 0) return <li className="pl-6">{props.rowButton(row)}</li>
  return <TopStepRow {...props} id={row.selection.path} />
}
