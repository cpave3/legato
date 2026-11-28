import { useState, type ReactNode } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { List, Lock } from "lucide-react"
import { fetchConfigLists, removeConfigListEntry, saveConfigListEntry, type ConfigList, type ConfigListEntry } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"
import { cn } from "../../lib/utils"
import { agentLabel, entryYaml, fieldApplies, formFor, parseEntryYaml, type FormField, type ListForm } from "../../lib/listForms"
import {
  asObject, duplicate, entrySummary, entryValue, fieldValue, firstError, parsedValue, resolveField, savedNotice, setEntryField, sortedEntries, tierRows, tierRowsError, tiersOf,
  unusedName, type EntryValue, type TierRow,
} from "../../lib/listEntry"
import { reasonText } from "../../lib/nativeAgentView"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"

const fieldClass = "min-h-9 min-w-0 w-full rounded-lg border border-lw-line-strong bg-lw-ground px-2.5 py-1 text-sm text-lw-text placeholder:text-lw-faint focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-60"

// Every field input takes the field, its value, and where to send a new
// value or the reason it cannot give one back.
type FieldProps = { field: FormField; value: unknown; onChange: (next: unknown) => void; onError: (message: string) => void }

// TiersInput edits an agent's tiers as a table: name, what the conductor
// is told, launch arguments (one per line). Its rows keep what was typed,
// and it gives the tiers back only while every name is its own, so a row
// never overwrites another. Fields of a tier it does not show are kept.
function TiersInput({ value, onChange, onError }: FieldProps) {
  const [rows, setRows] = useState<TierRow[]>(() => tierRows(value))
  const put = (next: TierRow[]) => {
    setRows(next)
    const refused = tierRowsError(next)
    onError(refused)
    if (!refused) onChange(tiersOf(next))
  }
  const edit = (i: number, change: Partial<TierRow>) => put(rows.map((r, j) => j === i ? { ...r, ...change } : r))
  return <div className="flex flex-col gap-2">
    {rows.length > 0 && <table className="w-full table-fixed border-separate border-spacing-y-1 text-xs" aria-label="Tiers">
      <thead className="text-left text-[11px] text-lw-text-2"><tr><th className="w-28 font-normal">Name</th><th className="font-normal">What the conductor is told</th><th className="w-44 font-normal">Launch arguments, one per line</th><th className="w-8" /></tr></thead>
      <tbody>{rows.map((row, i) => <tr key={i} className="align-top">
        <td className="pr-1.5"><input aria-label={`Tier ${i + 1} name`} value={row.name} onChange={(e) => edit(i, { name: e.target.value })} className={fieldClass} /></td>
        <td className="pr-1.5"><input aria-label={`Tier ${i + 1} description`} value={row.description} onChange={(e) => edit(i, { description: e.target.value })} className={fieldClass} /></td>
        <td className="pr-1.5"><textarea aria-label={`Tier ${i + 1} launch arguments`} rows={2} value={row.args} placeholder={"--model\nopus"}
          onChange={(e) => edit(i, { args: e.target.value })} className={cn(fieldClass, "font-lw-mono text-xs")} /></td>
        <td><button type="button" aria-label={`Remove tier ${row.name || i + 1}`} onClick={() => put(rows.filter((_, j) => j !== i))} className="text-lw-text-2 hover:text-lw-error">×</button></td>
      </tr>)}</tbody>
    </table>}
    <button type="button" className={quietButton + " min-h-8 w-fit px-2.5 text-xs"} onClick={() => put([...rows, { name: unusedName("tier", rows.map((r) => r.name)), description: "", args: "", rest: {} }])}>Add tier</button>
  </div>
}

// PairsInput edits a map of words to words, such as Chimera's modes,
// giving it back only while every key is its own.
function PairsInput({ field, value, onChange, onError }: FieldProps) {
  const [pairs, setPairs] = useState<[string, string][]>(() => Object.entries(asObject(value)).map(([k, v]) => [k, String(v ?? "")]))
  const [keyLabel, valueLabel] = field.pairLabels ?? ["Key", "Value"]
  const put = (rows: [string, string][]) => {
    setPairs(rows)
    const twice = duplicate(rows.map(([k]) => k))
    if (twice) { onError(`Two ${keyLabel.toLowerCase()}s are ${twice}.`); return }
    onError("")
    const kept = rows.filter(([k]) => k.trim())
    onChange(kept.length ? Object.fromEntries(kept.map(([k, v]) => [k.trim(), v])) : undefined)
  }
  return <div className="flex flex-col gap-1.5">
    {pairs.map(([k, v], i) => <span key={i} className="flex items-center gap-1.5">
      <input aria-label={`${keyLabel} ${i + 1}`} value={k} onChange={(e) => put(pairs.map((p, j) => j === i ? [e.target.value, p[1]] : p))} className={fieldClass} />
      <input aria-label={`${valueLabel} ${i + 1}`} value={v} onChange={(e) => put(pairs.map((p, j) => j === i ? [p[0], e.target.value] : p))} className={fieldClass} />
      <button type="button" aria-label={`Remove ${k || i + 1}`} onClick={() => put(pairs.filter((_, j) => j !== i))} className="px-1 text-lw-text-2 hover:text-lw-error">×</button>
    </span>)}
    <button type="button" className={quietButton + " min-h-8 w-fit px-2.5 text-xs"} onClick={() => put([...pairs, ["", ""]])}>Add {keyLabel.toLowerCase()}</button>
  </div>
}

const text = (value: unknown) => typeof value === "string" ? value : ""
// orUnset is a typed value, or nothing once it is cleared.
const orUnset = (value: string) => value === "" ? undefined : value

function NumberField({ field, value, onChange }: FieldProps) {
  return <input type="number" aria-label={field.label} className={fieldClass} value={typeof value === "number" ? value : ""} onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} />
}

function SwatchesField({ field, value, onChange }: FieldProps) {
  const current = text(value)
  return <span className="flex flex-wrap items-center gap-1.5" role="radiogroup" aria-label={field.label}>
    {(field.choices ?? []).map(([hex, name]) => {
      const chosen = current.toLowerCase() === hex.toLowerCase()
      return <button key={hex} type="button" role="radio" aria-checked={chosen} aria-label={name}
        onClick={() => onChange(hex)} className={cn("h-7 w-7 rounded-full border-2", chosen ? "border-lw-text" : "border-transparent")} style={{ background: hex }} />
    })}
    <input aria-label={`${field.label}, as a hex value`} value={current} placeholder="#4A9EEF" onChange={(e) => onChange(e.target.value)} className={cn(fieldClass, "w-28 font-lw-mono text-xs")} />
  </span>
}

// ChoiceField chooses one of the field's choices. A saved value it does not
// offer stays chosen, as an option of its own.
function ChoiceField({ field, value, onChange }: FieldProps) {
  const unknown = typeof value === "string" && value && !field.choices?.some(([v]) => v === value)
  return <select aria-label={field.label} className={fieldClass} value={text(value)} onChange={(e) => onChange(orUnset(e.target.value))}>
    {field.optional ? <option value="">{field.unsetLabel ?? "Default"}</option> : value === undefined && <option value="" disabled>Choose…</option>}
    {unknown && <option value={value}>{value}</option>}
    {(field.choices ?? []).map(([v, label]) => <option key={v} value={v}>{label}</option>)}
  </select>
}

function BoolField({ field, value, onChange }: FieldProps) {
  return <select aria-label={field.label} className={fieldClass} value={typeof value === "boolean" ? String(value) : ""} onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value === "true")}>
    <option value="">{field.unsetLabel ?? "Default"}</option><option value="true">On</option><option value="false">Off</option>
  </select>
}

// ChecksField chooses several of the field's choices, kept in their order
// and matched ignoring case; none chosen unsets it.
function ChecksField({ field, value, onChange }: FieldProps) {
  const chosen = Array.isArray(value) ? (value as string[]) : []
  const choices = field.choices ?? []
  const has = (c: string) => chosen.some((x) => x.toLowerCase() === c.toLowerCase())
  const toggle = (v: string, on: boolean) => {
    const next = choices.map(([c]) => c).filter((c) => c === v ? on : has(c))
    onChange(next.length ? next : undefined)
  }
  return <span role="group" aria-label={field.label} className="flex flex-wrap gap-x-3 gap-y-1">
    {choices.map(([v, label]) => <label key={v} className="flex items-center gap-1.5 text-xs">
      <input type="checkbox" className="h-4 w-4" checked={has(v)} onChange={(e) => toggle(v, e.target.checked)} />{label}
    </label>)}
  </span>
}

function CheckField({ field, value, onChange }: FieldProps) {
  return <input type="checkbox" aria-label={field.label} className="h-4 w-4 self-start" checked={value === true} onChange={(e) => onChange(e.target.checked ? true : undefined)} />
}

function LinesField({ field, value, onChange }: FieldProps) {
  return <textarea aria-label={field.label} rows={3} className={fieldClass} placeholder={field.placeholder}
    value={Array.isArray(value) ? (value as string[]).join("\n") : ""}
    onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value.split("\n"))} />
}

function TextareaField({ field, value, onChange }: FieldProps) {
  return <textarea aria-label={field.label} rows={4} className={fieldClass} placeholder={field.placeholder} value={text(value)}
    onChange={(e) => onChange(orUnset(e.target.value))} />
}

// TextField keeps a cleared required text as empty; an optional one is
// unset.
function TextField({ field, value, onChange }: FieldProps) {
  return <input aria-label={field.label} className={fieldClass} placeholder={field.placeholder} value={text(value)}
    onChange={(e) => onChange(field.optional ? orUnset(e.target.value) : e.target.value)} />
}

// The input of each kind of field.
const fieldInputs: Record<FormField["kind"], (props: FieldProps) => ReactNode> = {
  number: NumberField, tiers: TiersInput, pairs: PairsInput, swatches: SwatchesField, choice: ChoiceField, bool: BoolField,
  checks: ChecksField, check: CheckField, lines: LinesField, textarea: TextareaField, text: TextField,
}

// FieldInput edits one field of an entry by its kind.
function FieldInput({ field, value, onChange, onError = () => {} }: Omit<FieldProps, "onError"> & { onError?: (message: string) => void }) {
  const Input = fieldInputs[field.kind] ?? TextField
  return <Input field={field} value={value} onChange={onChange} onError={onError} />
}

function Labelled({ field, children }: { field: Pick<FormField, "label" | "help" | "optional">; children: ReactNode }) {
  return <div className="flex min-w-0 flex-col gap-1">
    <span className="flex items-baseline gap-2 text-xs text-lw-text">{field.label}{field.optional && <span className="text-[11px] text-lw-text-2">Optional</span>}</span>
    {children}
    {field.help && <span className="text-[11.5px] text-lw-text-2">{field.help}</span>}
  </div>
}

type ParsedEntry = { name: string; value: unknown }

const trimmedName = (parsed: ParsedEntry | null) => parsed && { name: parsed.name.trim(), value: parsed.value }

// readYaml reads an entry typed as YAML, saying why it cannot.
function readYaml(yaml: string | null, nameField: string | undefined, setError: (message: string) => void): ParsedEntry | null {
  try {
    return parseEntryYaml(yaml ?? "", nameField)
  } catch (reason) {
    setError(reasonText(reason))
    return null
  }
}

// useEntryDraft is an entry's name and value as edited. Untouched, it
// follows a change made elsewhere; touched (or as YAML), such a change is a
// conflict, settled by taking theirs or keeping this draft.
function useEntryDraft(form: ListForm, entry: ConfigListEntry | undefined, asYaml: boolean) {
  const value = entry?.value
  const theirs = JSON.stringify(value ?? null)
  const [name, setName] = useState(entry?.name ?? "")
  const [draft, setDraft] = useState<EntryValue>(() => entryValue(form, value))
  const [baseline, setBaseline] = useState(theirs)
  // What a field refuses to give back (two tiers of one name): Save waits.
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  // A field with rows of its own (tiers, modes) keeps a draft of them; a
  // value replaced from outside (followed, Use theirs, back from YAML)
  // starts those drafts again, and their refusals with them.
  const [generation, setGeneration] = useState(0)
  const replace = (value: EntryValue) => {
    setDraft(value)
    setGeneration((g) => g + 1)
    setFieldErrors({})
  }
  const dirty = JSON.stringify(draft) !== baseline || asYaml
  const changed = theirs !== baseline
  if (!dirty && changed) {
    setBaseline(theirs)
    replace(entryValue(form, value))
  }
  return {
    name, setName, draft, setDraft, generation,
    // A refusal holds back Save only while the form is shown.
    fieldError: asYaml ? "" : firstError(fieldErrors),
    setFieldError: (key: string, message: string) => setFieldErrors((e) => ({ ...e, [key]: message })),
    conflict: entry !== undefined && changed && dirty,
    takeTheirs: () => { replace(entryValue(form, value)); setBaseline(theirs) },
    keepMine: () => setBaseline(theirs),
    load: (parsed: ParsedEntry) => { setName(parsed.name); replace(parsedValue(form, parsed.value)) },
  }
}

// useEntrySave saves once at a time, keeping the form with the server's
// reason when it refuses.
function useEntrySave(onSave: (name: string, value: unknown) => Promise<void>, setError: (message: string) => void) {
  const [saving, setSaving] = useState(false)
  const save = async (blocked: boolean, read: () => ParsedEntry | null) => {
    if (saving || blocked) return
    const out = read()
    if (!out) return
    setError("")
    setSaving(true)
    try {
      await onSave(out.name, out.value)
    } catch (reason) {
      setError(reasonText(reason))
    } finally {
      setSaving(false)
    }
  }
  return { saving, save }
}

// EntryForm edits one entry with its list's form, or as YAML. Fields the
// form does not know are kept as they are.
export function EntryForm({ list, form, entry, onSave, onCancel, lists = [], meters = [] }: { lists?: ConfigList[]; list: ConfigList; form: ListForm; entry?: ConfigListEntry; meters?: string[]; onSave: (name: string, value: unknown) => Promise<void>; onCancel: () => void }) {
  const [yaml, setYaml] = useState<string | null>(null)
  const [error, setError] = useState("")
  const edit = useEntryDraft(form, entry, yaml !== null)
  const { saving, save } = useEntrySave(onSave, setError)
  const fieldError = edit.fieldError
  const message = error || fieldError
  const { read, toYaml, backToForm } = yamlActions(yaml, setYaml, list.name_field, edit, setError)
  return (
    <fieldset disabled={saving} aria-label={entry ? `Edit ${entry.name}` : `New entry of ${list.title}`} className="mt-2 flex min-w-0 flex-col gap-3 rounded-lg border border-lw-line bg-lw-ground/40 p-3">
      {yaml === null
        ? <EntryFields list={list} form={form} entry={entry} edit={edit} lists={lists} meters={meters} onEdited={() => setError("")} />
        : <Labelled field={{ label: "As in config.yaml", help: "The whole entry, fields the form shows and those it does not." }}>
          <textarea aria-label={`${list.title} entry as YAML`} rows={8} value={yaml} onChange={(e) => { setYaml(e.target.value); setError("") }} className={cn(fieldClass, "font-lw-mono text-xs")} spellCheck={false} />
        </Labelled>}
      {edit.conflict && <div className="flex flex-wrap items-center gap-2 text-xs text-lw-needs">
        <p role="status">This entry changed elsewhere while you edited it.</p>
        <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={() => { edit.takeTheirs(); setYaml(null) }}>Use theirs</button>
        <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={edit.keepMine}>Keep mine</button>
      </div>}
      {message && <p role="alert" className="text-xs text-lw-error">{message}</p>}
      <EntryActions saving={saving} blocked={(!edit.name.trim() && yaml === null) || edit.conflict || Boolean(fieldError)} asYaml={yaml !== null}
        onSave={() => void save(edit.conflict, read)} onCancel={onCancel} onYaml={toYaml} onForm={backToForm} />
    </fieldset>
  )
}

// yamlActions reads the entry to save, as YAML or from the form, and moves
// between the two; YAML that does not parse stays, with its error.
function yamlActions(yaml: string | null, setYaml: (yaml: string | null) => void, nameField: string | undefined,
  edit: ReturnType<typeof useEntryDraft>, setError: (message: string) => void) {
  const fromYaml = () => readYaml(yaml, nameField, setError)
  return {
    read: () => yaml === null ? { name: edit.name.trim(), value: edit.draft } : trimmedName(fromYaml()),
    toYaml: () => { setYaml(entryYaml(edit.name, edit.draft, nameField)); setError("") },
    backToForm: () => {
      const parsed = fromYaml()
      if (!parsed) return
      edit.load(parsed)
      setYaml(null)
    },
  }
}

// EntryActions is Save, Cancel, and the switch between the form and YAML.
function EntryActions({ saving, blocked, asYaml, onSave, onCancel, onYaml, onForm }: {
  saving: boolean; blocked: boolean; asYaml: boolean; onSave: () => void; onCancel: () => void; onYaml: () => void; onForm: () => void
}) {
  return <div className="flex flex-wrap items-center gap-2">
    <button type="button" className={primaryButton + " min-h-9"} onClick={onSave} disabled={saving || blocked}>{saving ? "Saving…" : "Save"}</button>
    <button type="button" className={quietButton + " min-h-9"} onClick={onCancel}>Cancel</button>
    <button type="button" className="ml-auto text-xs text-lw-link hover:underline" onClick={asYaml ? onForm : onYaml}>{asYaml ? "Back to the form" : "Edit as YAML"}</button>
  </div>
}

// EntryFields is the form itself: the entry's name, then its value or the
// fields that apply to it.
function EntryFields({ list, form, entry, edit, lists, meters, onEdited }: {
  list: ConfigList; form: ListForm; entry?: ConfigListEntry; edit: ReturnType<typeof useEntryDraft>
  lists: ConfigList[]; meters: string[]; onEdited: () => void
}) {
  const { name, draft } = edit
  const setField = (key: string, next: unknown) => {
    edit.setDraft(setEntryField(list.key, form, name, draft, key, next))
    onEdited()
  }
  const fields = form.fields.map((field) => resolveField(field, draft, lists, meters))
    .filter((field) => fieldApplies(field, name, draft as Record<string, unknown>))
  return <>
    <Labelled field={{ label: form.name.label, help: form.name.help }}>
      <input aria-label={form.name.label} value={name} disabled={Boolean(entry)} placeholder={form.name.placeholder} onChange={(e) => edit.setName(e.target.value)} className={fieldClass} />
    </Labelled>
    {form.scalar && <Labelled field={form.scalar}><FieldInput field={form.scalar} value={draft} onChange={(v) => { edit.setDraft(typeof v === "string" ? v : ""); onEdited() }} /></Labelled>}
    {fields.map((field) => <Labelled key={field.key} field={field}>
      <FieldInput key={`${field.key}:${edit.generation}`} field={field} value={fieldValue(draft, field.key)} onChange={(v) => setField(field.key, v)}
        onError={(message) => edit.setFieldError(field.key, message)} />
    </Labelled>)}
  </>
}

// useListEntries saves and removes a list's entries, saying how it went;
// saved is told once an entry is saved (not one sent for signature).
function useListEntries(baseUrl: string, list: ConfigList, saved: () => void) {
  const queryClient = useQueryClient()
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const refresh = () => queryClient.invalidateQueries({ queryKey: queryKeys.configLists(baseUrl) })
  const save = async (name: string, value: unknown) => {
    setNotice("")
    const result = await saveConfigListEntry(baseUrl, list.key, name, value)
    if (result?.status === "needs_signature") {
      setNotice("Awaiting approval: approve this rule change on your phone or with a passkey.")
      return
    }
    setNotice(savedNotice(list.mode))
    saved()
    await refresh()
  }
  const remove = async (name: string) => {
    setError("")
    try {
      const result = await removeConfigListEntry(baseUrl, list.key, name)
      setNotice(result?.status === "needs_signature" ? "Awaiting approval: approve this removal on your phone or with a passkey." : "Removed.")
      await refresh()
    } catch (reason) {
      setError(reasonText(reason))
    }
  }
  return { error, notice, setNotice, save, remove }
}

// ListCard is one list: config.yaml's entries, read-only with a lock, and
// those kept here, which can be edited and removed; and its Add.
export function ListCard({ list, baseUrl, lists = [], meters = [] }: { list: ConfigList; baseUrl: string; lists?: ConfigList[]; meters?: string[] }) {
  const form = formFor(list.key, list.name_field)
  const [editing, setEditing] = useState<string | null>(null)
  const { error, notice, setNotice, save, remove } = useListEntries(baseUrl, list, () => setEditing(null))
  const startEditing = (name: string) => { setNotice(""); setEditing(name) }
  const entryForm = (entry?: ConfigListEntry) => <EntryForm list={list} lists={lists} meters={meters} form={form} entry={entry} onSave={save} onCancel={() => setEditing(null)} />
  return (
    <SettingsCard id={`list-${list.key}`} icon={List} title={list.title} description={form.ordered}>
      {list.entries.length > 0 && <ul className={form.cards ? "flex flex-col gap-2" : "flex flex-col divide-y divide-lw-line"} aria-label={list.title}>
        {sortedEntries(list).map((entry, index) => <EntryRow key={entry.name} list={list} form={form} entry={entry} index={index} editing={editing === entry.name}
          onEdit={() => startEditing(entry.name)} onRemove={() => void remove(entry.name)}>
          {editing === entry.name && entryForm(entry)}
        </EntryRow>)}
      </ul>}
      {list.entries.length === 0 && editing !== "" && <p className="text-sm text-lw-text-2">{form.empty}</p>}
      {notice && <p role="status" className="mt-1 text-xs text-lw-text-2">{notice}</p>}
      {error && <p role="alert" className="mt-1 text-xs text-lw-error">{error}</p>}
      {editing === "" ? entryForm()
        : <button type="button" className={`${quietButton} mt-2 min-h-9`} onClick={() => startEditing("")}>{listFormsAdd(form, list)}</button>}
    </SettingsCard>
  )
}

// EntryRow is one entry: its name, where it comes from, what it holds, and
// Edit and Remove for one kept here; its form below while it is edited.
function EntryRow({ list, form, entry, index, editing, onEdit, onRemove, children }: {
  list: ConfigList; form: ListForm; entry: ConfigListEntry; index: number; editing: boolean
  onEdit: () => void; onRemove: () => void; children: ReactNode
}) {
  const color = list.key === "workspaces" ? asObject(entry.value).color : undefined
  return <li className={form.cards ? "rounded-lg border border-lw-line px-3 py-2.5" : "py-2"} data-testid={`entry-${list.key}-${entry.name}`}>
    <div className="flex flex-wrap items-center gap-2">
      {form.ordered && <span className="w-5 font-lw-mono text-[11px] text-lw-text-2">{index + 1}</span>}
      {typeof color === "string" && <span className="h-2.5 w-2.5 rounded-full" style={{ background: color }} aria-hidden />}
      <span className={cn("text-sm text-lw-text", form.cards && "font-medium")}>{list.key === "adapters" ? agentLabel(entry.name) : entry.name}</span>
      <EntrySource entry={entry} pending={list.pending_entries?.includes(entry.name)} />
      <span className="min-w-0 truncate text-xs text-lw-text-2">{entrySummary(form, entry)}</span>
      {!entry.pinned && !editing && (
        <span className="ml-auto flex gap-1">
          <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={onEdit} aria-label={`Edit ${entry.name}`}>Edit</button>
          <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={onRemove} aria-label={`Remove ${entry.name}`}>Remove</button>
        </span>
      )}
    </div>
    {children}
  </li>
}

// EntrySource says where an entry comes from, and whether it waits for a
// restart.
function EntrySource({ entry, pending }: { entry: ConfigListEntry; pending?: boolean }) {
  return <>
    {entry.pinned
      ? <span className="inline-flex items-center gap-1 rounded border border-lw-line bg-lw-raised px-1.5 py-px text-[11px] text-lw-text-2" aria-label="Pinned by config.yaml"><Lock size={10} aria-hidden />config.yaml</span>
      : entry.source === "database" && <span className="rounded bg-lw-accent-soft px-1.5 py-px text-[11px] text-lw-accent-text">Set here</span>}
    {pending && <span className="rounded bg-lw-needs-soft px-1.5 py-px text-[11px] text-lw-needs">Restart to apply</span>}
  </>
}

function listFormsAdd(form: ListForm, list: ConfigList): string {
  return form.add === "Add" ? `Add to ${list.title.toLowerCase()}` : form.add
}

export function ListSettings({ baseUrl, lists, meters = [] }: { baseUrl: string; lists?: string[]; meters?: string[] }) {
  const query = useQuery({ queryKey: queryKeys.configLists(baseUrl), queryFn: () => fetchConfigLists(baseUrl), retry: false, enabled: lists?.length !== 0 })
  if (lists?.length === 0) return null
  return <>
    {query.isPending && <p role="status" className="text-sm text-lw-text-2">Loading lists…</p>}
    {query.isFetching && query.data && <p role="status" className="text-sm text-lw-text-2">Refreshing lists…</p>}
    {query.isError && <div role="alert" className="text-sm text-lw-error">
      <p>{query.error.message}{query.data ? " Showing previously loaded lists; they may be out of date." : ""}</p>
      <button type="button" className={quietButton} disabled={query.isFetching} onClick={() => void query.refetch()}>Retry</button>
    </div>}
    {query.data?.filter((list) => lists === undefined || lists.includes(list.key)).map((list) => <ListCard key={`${baseUrl}:${list.key}`} list={list} lists={query.data} meters={meters} baseUrl={baseUrl} />)}
  </>
}
