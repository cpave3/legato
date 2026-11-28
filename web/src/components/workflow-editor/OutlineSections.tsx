import type { ReactNode } from "react"
import { Plus } from "lucide-react"
import type { Item } from "../../lib/workflowDocument"
import { Button } from "../ui/Button"
import type { Row } from "./outlineRows"
import { KIND_TITLES } from "./triggerForms"
import { StepPicker } from "./StepPicker"
import type { Adding } from "./useOutlineEditing"

/** StepPalette is the step picker while a step is being added, which
 * adds the type picked where the + was. */
export function StepPalette({ adding, narrow, onPick, onClose }: { adding: Adding; narrow: boolean; onPick: (type: string, at: number) => void; onClose: () => void }) {
  if (adding?.kind !== "step") return null
  return <StepPicker narrow={narrow} onPick={type => onPick(type, adding.at)} onClose={onClose} />
}

/** OutlineSection is one section of the outline in the list: its heading
 * and body. */
export function OutlineSection({ title, children }: { title: string; children: ReactNode }) {
  return <section aria-label={title} className="flex flex-col gap-1">
    <h2 className="px-2 text-[11px] font-semibold uppercase tracking-wide text-lw-faint">{title}</h2>
    {children}
  </section>
}

/** ContainerAlerts says why a section's container (on, inputs, steps)
 * can't be shown as forms, and offers its YAML. */
export function ContainerAlerts({ containers, name, onEditYaml }: { containers: Item[]; name: string; onEditYaml: () => void }) {
  return <>{containers.filter(item => item.item.kind === "about" && item.item.key === name).map(item => <p key={name} role="alert" className="mx-2 flex flex-wrap items-center gap-2 text-xs text-lw-needs">
    {item.problem} <Button size="sm" variant="ghost" onClick={() => onEditYaml()}>Edit as YAML</Button>
  </p>)}</>
}

type TriggerKind = { kind: string; description: string }

// AddTriggerMenu lists the trigger kinds the workflow does not have yet.
function AddTriggerMenu({ missing, onAdd, onClose }: { missing: TriggerKind[]; onAdd: (kind: string) => void; onClose: () => void }) {
  return <div role="dialog" aria-label="Add a trigger" className="mx-2 flex flex-col gap-1 rounded-lg border border-lw-line bg-lw-surface p-1 shadow-xl" onKeyDown={event => { if (event.key === "Escape") onClose() }}>
    {missing.map(t => <button key={t.kind} type="button" className="flex min-h-9 flex-col items-start rounded-md px-2 py-1 text-left hover:bg-lw-raised" onClick={() => onAdd(t.kind)}>
      <span className="text-sm">{KIND_TITLES[t.kind] ?? t.kind}</span>
      <span className="text-xs text-lw-text-2">{t.description}</span>
    </button>)}
    <Button type="button" size="sm" variant="ghost" className="self-start" onClick={onClose}>Cancel</Button>
  </div>
}

type BodyProps = { rows: Row[]; containers: Item[]; editing: boolean; rowItem: (row: Row) => ReactNode; onEditYaml: () => void }

/** TriggersBody is the Triggers section: its rows, and adding a trigger of
 * a kind it lacks. */
export function TriggersBody({ rows, containers, editing, rowItem, onEditYaml, missing, adding, setAdding, onAdd }: BodyProps & { missing: TriggerKind[]; adding: Adding; setAdding: (adding: Adding) => void; onAdd: (kind: string) => void }) {
  const add = adding?.kind === "trigger"
    ? <AddTriggerMenu missing={missing} onAdd={onAdd} onClose={() => setAdding(null)} />
    : <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => setAdding({ kind: "trigger" })}><Plus size={13} />Add trigger</Button>
  return <>
    <ul className="flex flex-col">{rows.map(rowItem)}</ul>
    <ContainerAlerts containers={containers} name="on" onEditYaml={onEditYaml} />
    {editing && missing.length > 0 && add}
    <p className="px-2 text-xs text-lw-faint">You can always run it by hand.</p>
  </>
}

/** InputsBody is the Inputs section: its rows, and adding one. */
export function InputsBody({ rows, containers, editing, rowItem, onEditYaml, onAdd }: BodyProps & { onAdd: () => void }) {
  return <>
    {rows.length === 0 && <p className="px-2 text-sm text-lw-text-2">Nothing: it takes no inputs</p>}
    <ul className="flex flex-col">{rows.map(rowItem)}</ul>
    <ContainerAlerts containers={containers} name="inputs" onEditYaml={onEditYaml} />
    {editing && <Button type="button" size="sm" variant="ghost" className="self-start" onClick={onAdd}><Plus size={13} />Add input</Button>}
  </>
}

// NoSteps is the Steps section with no steps: a button to add the first
// (none while it is being added), or beside the YAML why it needs one.
function NoSteps({ editing, adding, onAdd }: { editing: boolean; adding: Adding; onAdd: () => void }) {
  if (!editing) return <p className="px-2 text-sm text-lw-text-2">No steps yet: a workflow needs at least one.</p>
  if (adding?.kind === "step") return null
  return <button type="button" className="mx-2 flex min-h-16 items-center justify-center gap-2 rounded-lg border border-dashed border-lw-line-strong text-sm text-lw-text-2 hover:bg-lw-raised hover:text-lw-text" onClick={onAdd}><Plus size={14} />Add the first step</button>
}

// AddStepAtEnd is the picker when a step is being added at the end, else
// a button to add one there once there is a step.
function AddStepAtEnd({ count, adding, palette, onAdd }: { count: number; adding: Adding; palette: ReactNode; onAdd: () => void }) {
  if (adding?.kind === "step" && adding.at >= count) return <>{palette}</>
  return <>{count > 0 && <Button type="button" size="sm" variant="ghost" className="self-start" onClick={onAdd}><Plus size={13} />Add step</Button>}</>
}

type StepsBodyProps = Omit<BodyProps, "rowItem"> & { stepRow: (row: Row) => ReactNode; count: number; warning: string; adding: Adding; setAdding: (adding: Adding) => void; palette: ReactNode }

/** StepsBody is the Steps section: the steps in order, adding one at the
 * end, and why a move was refused. */
export function StepsBody({ rows, containers, editing, onEditYaml, stepRow, count, warning, adding, setAdding, palette }: StepsBodyProps) {
  return <>
    {rows.length === 0 && <NoSteps editing={editing} adding={adding} onAdd={() => setAdding({ kind: "step", at: 0 })} />}
    {warning && <p role="alert" className="px-2 text-xs text-lw-needs">{warning}</p>}
    <ol aria-label="Steps" className="flex flex-col">{rows.map(stepRow)}</ol>
    <ContainerAlerts containers={containers} name="steps" onEditYaml={onEditYaml} />
    {editing && <AddStepAtEnd count={count} adding={adding} palette={palette} onAdd={() => setAdding({ kind: "step", at: count })} />}
  </>
}
