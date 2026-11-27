import { useState, type ReactNode } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { List, Lock } from "lucide-react"
import { fetchConfigLists, removeConfigListEntry, saveConfigListEntry, type ConfigList, type ConfigListEntry } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"
import { cn } from "../../lib/utils"
import { agentLabel, entryYaml, fieldApplies, formFor, parseEntryYaml, type FormField, type ListForm } from "../../lib/listForms"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"

const fieldClass = "min-h-9 min-w-0 w-full rounded-lg border border-lw-line-strong bg-lw-ground px-2.5 py-1 text-sm text-lw-text placeholder:text-lw-faint focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-60"

type Value = Record<string, unknown> | string

function asObject(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function fieldValue(value: unknown, key: string): unknown {
  return key.split(".").reduce<unknown>((v, part) => asObject(v)[part], value)
}

function writeField(object: Record<string, unknown>, key: string, value: unknown): Record<string, unknown> {
  const [head, ...tail] = key.split(".")
  const next = { ...object }
  const out = tail.length ? writeField(asObject(next[head]), tail.join("."), value) : value
  if (out === undefined || (typeof out === "object" && out !== null && Object.keys(out).length === 0)) delete next[head]
  else next[head] = out
  return next
}

type Tier = { description?: string; launch_args?: string[] } & Record<string, unknown>

// unusedName is base-N for the first N no row has.
function unusedName(base: string, taken: string[]): string {
  for (let n = taken.length + 1; ; n++) if (!taken.includes(`${base}-${n}`)) return `${base}-${n}`
}

// duplicate is a name two rows share, or "".
function duplicate(names: string[]): string {
  const seen = new Set<string>()
  for (const n of names.map((x) => x.trim()).filter(Boolean)) {
    if (seen.has(n)) return n
    seen.add(n)
  }
  return ""
}

// TiersInput edits an agent's tiers as a table: name, what the conductor
// is told, launch arguments (one per line). Its rows keep what was typed,
// and it gives the tiers back only while every name is its own, so a row
// never overwrites another. Fields of a tier it does not show are kept.
function TiersInput({ value, onChange, onError }: { value: unknown; onChange: (next: unknown) => void; onError: (message: string) => void }) {
  type Row = { name: string; description: string; args: string; rest: Tier }
  const [rows, setRows] = useState<Row[]>(() => (Object.entries(asObject(value)) as [string, Tier][])
    .map(([name, tier]) => ({ name, description: tier.description ?? "", args: (tier.launch_args ?? []).join("\n"), rest: tier })))
  const put = (next: Row[]) => {
    setRows(next)
    const twice = duplicate(next.map((r) => r.name))
    if (twice) { onError(`Two tiers are named ${twice}.`); return }
    if (next.some((r) => !r.name.trim())) { onError("Give every tier a name."); return }
    onError("")
    const tiers = next.map((r): [string, Tier] => {
      const rest: Tier = { ...r.rest }
      delete rest.description
      delete rest.launch_args
      const args = r.args.split("\n").map((a) => a.trim()).filter(Boolean)
      return [r.name.trim(), { ...rest, ...(r.description.trim() ? { description: r.description } : {}), ...(args.length ? { launch_args: args } : {}) }]
    })
    onChange(tiers.length ? Object.fromEntries(tiers) : undefined)
  }
  const edit = (i: number, change: Partial<Row>) => put(rows.map((r, j) => j === i ? { ...r, ...change } : r))
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
function PairsInput({ field, value, onChange, onError }: { field: FormField; value: unknown; onChange: (next: unknown) => void; onError: (message: string) => void }) {
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

// FieldInput edits one field of an entry by its kind.
function FieldInput({ field, value, onChange, onError = () => {} }: { field: FormField; value: unknown; onChange: (next: unknown) => void; onError?: (message: string) => void }) {
  switch (field.kind) {
    case "number":
      return <input type="number" aria-label={field.label} className={fieldClass} value={typeof value === "number" ? value : ""} onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} />
    case "tiers":
      return <TiersInput value={value} onChange={onChange} onError={onError} />
    case "pairs":
      return <PairsInput field={field} value={value} onChange={onChange} onError={onError} />
    case "swatches": {
      const current = typeof value === "string" ? value : ""
      return <span className="flex flex-wrap items-center gap-1.5" role="radiogroup" aria-label={field.label}>
        {(field.choices ?? []).map(([hex, name]) => <button key={hex} type="button" role="radio" aria-checked={current.toLowerCase() === hex.toLowerCase()} aria-label={name}
          onClick={() => onChange(hex)} className={cn("h-7 w-7 rounded-full border-2", current.toLowerCase() === hex.toLowerCase() ? "border-lw-text" : "border-transparent")} style={{ background: hex }} />)}
        <input aria-label={`${field.label}, as a hex value`} value={current} placeholder="#4A9EEF" onChange={(e) => onChange(e.target.value)} className={cn(fieldClass, "w-28 font-lw-mono text-xs")} />
      </span>
    }
    case "choice":
      return <select aria-label={field.label} className={fieldClass} value={typeof value === "string" ? value : ""} onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)}>
        {field.optional ? <option value="">{field.unsetLabel ?? "Default"}</option> : value === undefined && <option value="" disabled>Choose…</option>}
        {typeof value === "string" && value && !field.choices?.some(([v]) => v === value) && <option value={value}>{value}</option>}
        {(field.choices ?? []).map(([v, label]) => <option key={v} value={v}>{label}</option>)}
      </select>
    case "bool":
      return <select aria-label={field.label} className={fieldClass} value={typeof value === "boolean" ? String(value) : ""} onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value === "true")}>
        <option value="">{field.unsetLabel ?? "Default"}</option><option value="true">On</option><option value="false">Off</option>
      </select>
    case "check":
      return <input type="checkbox" aria-label={field.label} className="h-4 w-4 self-start" checked={value === true} onChange={(e) => onChange(e.target.checked ? true : undefined)} />
    case "lines":
      return <textarea aria-label={field.label} rows={3} className={fieldClass} placeholder={field.placeholder}
        value={Array.isArray(value) ? (value as string[]).join("\n") : ""}
        onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value.split("\n"))} />
    case "textarea":
      return <textarea aria-label={field.label} rows={4} className={fieldClass} placeholder={field.placeholder} value={typeof value === "string" ? value : ""}
        onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)} />
    default:
      return <input aria-label={field.label} className={fieldClass} placeholder={field.placeholder} value={typeof value === "string" ? value : ""}
        onChange={(e) => onChange(field.optional && e.target.value === "" ? undefined : e.target.value)} />
  }
}

function Labelled({ field, children }: { field: Pick<FormField, "label" | "help" | "optional">; children: ReactNode }) {
  return <div className="flex min-w-0 flex-col gap-1">
    <span className="flex items-baseline gap-2 text-xs text-lw-text">{field.label}{field.optional && <span className="text-[11px] text-lw-text-2">Optional</span>}</span>
    {children}
    {field.help && <span className="text-[11.5px] text-lw-text-2">{field.help}</span>}
  </div>
}

// EntryForm edits one entry with its list's form, or as YAML. Fields the
// form does not know are kept as they are.
export function EntryForm({ list, form, entry, onSave, onCancel, lists = [], meters = [] }: { lists?: ConfigList[]; list: ConfigList; form: ListForm; entry?: ConfigListEntry; meters?: string[]; onSave: (name: string, value: unknown) => Promise<void>; onCancel: () => void }) {
  const initial: Value = form.scalar ? (typeof entry?.value === "string" ? entry.value : "") : asObject(entry?.value)
  const [name, setName] = useState(entry?.name ?? "")
  const [draft, setDraft] = useState<Value>(initial)
  const [baseline, setBaseline] = useState(JSON.stringify(entry?.value ?? null))
  const [yaml, setYaml] = useState<string | null>(null)
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)
  // What a field refuses to give back (two tiers of one name): Save waits.
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const fieldError = yaml === null ? Object.values(fieldErrors).find(Boolean) ?? "" : ""
  // A field with rows of its own (tiers, modes) keeps a draft of them; a
  // value replaced from outside (followed, Use theirs, back from YAML)
  // starts those drafts again, and their refusals with them.
  const [generation, setGeneration] = useState(0)
  const replace = (value: Value) => {
    setDraft(value)
    setGeneration((g) => g + 1)
    setFieldErrors({})
  }
  const theirsValue = (): Value => form.scalar ? (typeof entry?.value === "string" ? entry.value : "") : asObject(entry?.value)
  // Changed elsewhere while edited: choose theirs or keep this draft.
  const theirs = JSON.stringify(entry?.value ?? null)
  const dirty = JSON.stringify(draft) !== baseline || yaml !== null
  // Untouched, the form follows a change made elsewhere.
  if (!dirty && theirs !== baseline) {
    setBaseline(theirs)
    replace(theirsValue())
  }
  const conflict = Boolean(entry) && theirs !== baseline && dirty
  const setField = (key: string, next: unknown) => {
    let object = writeField(asObject(draft), key, next)
    if (list.key === "usage.profiles" && (key === "agent" || key === "reviewer.agent")) {
      object = writeField(object, key === "agent" ? "tier" : "reviewer.tier", undefined)
    }
    // A field that no longer applies (a draft rule's option on a rule
    // that is not draft) leaves the entry.
    for (const field of form.fields) if (field.when && !fieldApplies(field, name, object)) delete object[field.key]
    setDraft(object)
    setError("")
  }
  const resolvedFields = form.fields.map((field): FormField => {
    if (!field.optionsFrom) return field
    // The usage rule meters come from the server's usage catalog, not a
    // list of our own; profiles and tiers come from the settings lists.
    const names = field.optionsFrom === "usage_meters"
      ? meters
      : field.optionsFrom === "profiles"
        ? lists.find((l) => l.key === "usage.profiles")?.entries.map((e) => e.name) ?? []
        : Object.keys(asObject(asObject(lists.find((l) => l.key === "adapters")?.entries.find((e) => e.name === fieldValue(draft, field.agentKey ?? "agent"))?.value).tiers))
    return { ...field, choices: names.map((name) => [name, name]) }
  })
  const toYaml = () => { setYaml(entryYaml(name, draft, list.name_field)); setError("") }
  const fromYaml = (): { name: string; value: unknown } | null => {
    try {
      return parseEntryYaml(yaml ?? "", list.name_field)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      return null
    }
  }
  const backToForm = () => {
    const parsed = fromYaml()
    if (!parsed) return
    setName(parsed.name)
    replace(form.scalar ? String(parsed.value ?? "") : asObject(parsed.value))
    setYaml(null)
  }
  const save = async () => {
    if (saving || conflict) return
    let out = { name: name.trim(), value: draft as unknown }
    if (yaml !== null) {
      const parsed = fromYaml()
      if (!parsed) return
      out = { name: parsed.name.trim(), value: parsed.value }
    }
    setError("")
    setSaving(true)
    try {
      await onSave(out.name, out.value)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSaving(false)
    }
  }
  return (
    <fieldset disabled={saving} aria-label={entry ? `Edit ${entry.name}` : `New entry of ${list.title}`} className="mt-2 flex min-w-0 flex-col gap-3 rounded-lg border border-lw-line bg-lw-ground/40 p-3">
      {yaml === null ? <>
        <Labelled field={{ label: form.name.label, help: form.name.help }}>
          <input aria-label={form.name.label} value={name} disabled={Boolean(entry)} placeholder={form.name.placeholder} onChange={(e) => setName(e.target.value)} className={fieldClass} />
        </Labelled>
        {form.scalar && <Labelled field={form.scalar}><FieldInput field={form.scalar} value={draft} onChange={(v) => { setDraft(typeof v === "string" ? v : ""); setError("") }} /></Labelled>}
        {resolvedFields.filter((field) => fieldApplies(field, name, draft as Record<string, unknown>)).map((field) => <Labelled key={field.key} field={field}>
          <FieldInput key={`${field.key}:${generation}`} field={field} value={fieldValue(draft, field.key)} onChange={(v) => setField(field.key, v)}
            onError={(message) => setFieldErrors((e) => ({ ...e, [field.key]: message }))} />
        </Labelled>)}
      </> : <Labelled field={{ label: "As in config.yaml", help: "The whole entry, fields the form shows and those it does not." }}>
        <textarea aria-label={`${list.title} entry as YAML`} rows={8} value={yaml} onChange={(e) => { setYaml(e.target.value); setError("") }} className={cn(fieldClass, "font-lw-mono text-xs")} spellCheck={false} />
      </Labelled>}
      {conflict && <div className="flex flex-wrap items-center gap-2 text-xs text-lw-needs">
        <p role="status">This entry changed elsewhere while you edited it.</p>
        <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={() => { replace(theirsValue()); setBaseline(theirs); setYaml(null) }}>Use theirs</button>
        <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={() => setBaseline(theirs)}>Keep mine</button>
      </div>}
      {(error || fieldError) && <p role="alert" className="text-xs text-lw-error">{error || fieldError}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className={primaryButton + " min-h-9"} onClick={() => void save()} disabled={saving || (!name.trim() && yaml === null) || conflict || Boolean(fieldError)}>{saving ? "Saving…" : "Save"}</button>
        <button type="button" className={quietButton + " min-h-9"} onClick={onCancel}>Cancel</button>
        <button type="button" className="ml-auto text-xs text-lw-link hover:underline" onClick={yaml === null ? toYaml : backToForm}>{yaml === null ? "Edit as YAML" : "Back to the form"}</button>
      </div>
    </fieldset>
  )
}

// summary says in a few words what an entry holds.
function summary(form: ListForm, entry: ConfigListEntry): string {
  if (form.summary) return form.summary(entry.name, asObject(entry.value))
  if (form.scalar) return typeof entry.value === "string" ? entry.value : ""
  const object = asObject(entry.value)
  return form.fields.map((f) => {
    const v = fieldValue(object, f.key)
    if (v === undefined || v === "") return ""
    if (f.kind === "choice") return f.choices?.find(([c]) => c === v)?.[1] ?? String(v)
    if (f.kind === "swatches") return ""
    if (f.kind === "check") return v === true ? f.label : ""
    return Array.isArray(v) ? v.join(" ") : String(v)
  }).filter(Boolean).join(" · ")
}

// ListCard is one list: config.yaml's entries, read-only with a lock, and
// those kept here, which can be edited and removed; and its Add.
export function ListCard({ list, baseUrl, lists = [], meters = [] }: { list: ConfigList; baseUrl: string; lists?: ConfigList[]; meters?: string[] }) {
  const queryClient = useQueryClient()
  const form = formFor(list.key, list.name_field)
  const entries = list.key === "usage.rules" ? [...list.entries].sort((a, b) => Number(asObject(a.value).order) - Number(asObject(b.value).order) || a.name.localeCompare(b.name)) : list.entries
  const add = listFormsAdd(form, list)
  const [editing, setEditing] = useState<string | null>(null)
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
    setNotice(list.mode === "restart" ? "Saved. It applies when Legato restarts." : list.mode === "new_agents" ? "Saved. It applies to agents started from now on." : "Saved.")
    setEditing(null)
    await refresh()
  }
  const remove = async (name: string) => {
    setError("")
    try {
      const result = await removeConfigListEntry(baseUrl, list.key, name)
      setNotice(result?.status === "needs_signature" ? "Awaiting approval: approve this removal on your phone or with a passkey." : "Removed.")
      await refresh()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }
  return (
    <SettingsCard id={`list-${list.key}`} icon={List} title={list.title} description={form.ordered}>
      {list.entries.length > 0 && <ul className={form.cards ? "flex flex-col gap-2" : "flex flex-col divide-y divide-lw-line"} aria-label={list.title}>
        {entries.map((entry, index) => {
          const color = list.key === "workspaces" ? asObject(entry.value).color : undefined
          return <li key={entry.name} className={form.cards ? "rounded-lg border border-lw-line px-3 py-2.5" : "py-2"} data-testid={`entry-${list.key}-${entry.name}`}>
            <div className="flex flex-wrap items-center gap-2">
              {form.ordered && <span className="w-5 font-lw-mono text-[11px] text-lw-text-2">{index + 1}</span>}
              {typeof color === "string" && <span className="h-2.5 w-2.5 rounded-full" style={{ background: color }} aria-hidden />}
              <span className={cn("text-sm text-lw-text", form.cards && "font-medium")}>{list.key === "adapters" ? agentLabel(entry.name) : entry.name}</span>
              {entry.pinned
                ? <span className="inline-flex items-center gap-1 rounded border border-lw-line bg-lw-raised px-1.5 py-px text-[11px] text-lw-text-2" aria-label="Pinned by config.yaml"><Lock size={10} aria-hidden />config.yaml</span>
                : entry.source === "database" && <span className="rounded bg-lw-accent-soft px-1.5 py-px text-[11px] text-lw-accent-text">Set here</span>}
              {list.pending_entries?.includes(entry.name) && <span className="rounded bg-lw-needs-soft px-1.5 py-px text-[11px] text-lw-needs">Restart to apply</span>}
              <span className="min-w-0 truncate text-xs text-lw-text-2">{summary(form, entry)}</span>
              {!entry.pinned && editing !== entry.name && (
                <span className="ml-auto flex gap-1">
                  <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={() => { setNotice(""); setEditing(entry.name) }} aria-label={`Edit ${entry.name}`}>Edit</button>
                  <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={() => void remove(entry.name)} aria-label={`Remove ${entry.name}`}>Remove</button>
                </span>
              )}
            </div>
            {editing === entry.name && <EntryForm list={list} lists={lists} meters={meters} form={form} entry={entry} onSave={save} onCancel={() => setEditing(null)} />}
          </li>
        })}
      </ul>}
      {list.entries.length === 0 && editing !== "" && <p className="text-sm text-lw-text-2">{form.empty}</p>}
      {notice && <p role="status" className="mt-1 text-xs text-lw-text-2">{notice}</p>}
      {error && <p role="alert" className="mt-1 text-xs text-lw-error">{error}</p>}
      {editing === "" ? <EntryForm list={list} lists={lists} meters={meters} form={form} onSave={save} onCancel={() => setEditing(null)} />
        : <button type="button" className={`${quietButton} mt-2 min-h-9`} onClick={() => { setNotice(""); setEditing("") }}>{add}</button>}
    </SettingsCard>
  )
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
