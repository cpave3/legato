import { useState, type ReactNode } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { List, Lock } from "lucide-react"
import { fetchConfigLists, removeConfigListEntry, saveConfigListEntry, type ConfigList, type ConfigListEntry } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"
import { cn } from "../../lib/utils"
import { entryYaml, formFor, parseEntryYaml, type FormField, type ListForm } from "../../lib/listForms"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"

const fieldClass = "min-h-9 min-w-0 w-full rounded-lg border border-lw-line-strong bg-lw-ground px-2.5 py-1 text-sm text-lw-text placeholder:text-lw-faint focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-60"

type Value = Record<string, unknown> | string

function asObject(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

// FieldInput edits one field of an entry by its kind.
function FieldInput({ field, value, onChange }: { field: FormField; value: unknown; onChange: (next: unknown) => void }) {
  switch (field.kind) {
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
        {field.optional && <option value="">{field.unsetLabel ?? "Default"}</option>}
        {(field.choices ?? []).map(([v, label]) => <option key={v} value={v}>{label}</option>)}
      </select>
    case "bool":
      return <select aria-label={field.label} className={fieldClass} value={typeof value === "boolean" ? String(value) : ""} onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value === "true")}>
        <option value="">{field.unsetLabel ?? "Default"}</option><option value="true">On</option><option value="false">Off</option>
      </select>
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
export function EntryForm({ list, form, entry, onSave, onCancel }: { list: ConfigList; form: ListForm; entry?: ConfigListEntry; onSave: (name: string, value: unknown) => Promise<void>; onCancel: () => void }) {
  const initial: Value = form.scalar ? (typeof entry?.value === "string" ? entry.value : "") : asObject(entry?.value)
  const [name, setName] = useState(entry?.name ?? "")
  const [draft, setDraft] = useState<Value>(initial)
  const [baseline, setBaseline] = useState(JSON.stringify(entry?.value ?? null))
  const [yaml, setYaml] = useState<string | null>(null)
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)
  // Changed elsewhere while edited: choose theirs or keep this draft.
  const theirs = JSON.stringify(entry?.value ?? null)
  const dirty = JSON.stringify(draft) !== baseline || yaml !== null
  // Untouched, the form follows a change made elsewhere.
  if (!dirty && theirs !== baseline) {
    setBaseline(theirs)
    setDraft(form.scalar ? (typeof entry?.value === "string" ? entry.value : "") : asObject(entry?.value))
  }
  const conflict = Boolean(entry) && theirs !== baseline && dirty
  const setField = (key: string, next: unknown) => {
    const object = { ...(draft as Record<string, unknown>) }
    if (next === undefined) delete object[key]
    else object[key] = next
    setDraft(object)
    setError("")
  }
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
    setDraft(form.scalar ? String(parsed.value ?? "") : asObject(parsed.value))
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
        {form.fields.map((field) => <Labelled key={field.key} field={field}>
          <FieldInput field={field} value={(draft as Record<string, unknown>)[field.key]} onChange={(v) => setField(field.key, v)} />
        </Labelled>)}
      </> : <Labelled field={{ label: "As in config.yaml", help: "The whole entry, fields the form shows and those it does not." }}>
        <textarea aria-label={`${list.title} entry as YAML`} rows={8} value={yaml} onChange={(e) => { setYaml(e.target.value); setError("") }} className={cn(fieldClass, "font-lw-mono text-xs")} spellCheck={false} />
      </Labelled>}
      {conflict && <div className="flex flex-wrap items-center gap-2 text-xs text-lw-needs">
        <p role="status">This entry changed elsewhere while you edited it.</p>
        <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={() => { setDraft(form.scalar ? String(entry?.value ?? "") : asObject(entry?.value)); setBaseline(theirs); setYaml(null) }}>Use theirs</button>
        <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} onClick={() => setBaseline(theirs)}>Keep mine</button>
      </div>}
      {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className={primaryButton + " min-h-9"} onClick={() => void save()} disabled={saving || (!name.trim() && yaml === null) || conflict}>{saving ? "Saving…" : "Save"}</button>
        <button type="button" className={quietButton + " min-h-9"} onClick={onCancel}>Cancel</button>
        <button type="button" className="ml-auto text-xs text-lw-link hover:underline" onClick={yaml === null ? toYaml : backToForm}>{yaml === null ? "Edit as YAML" : "Back to the form"}</button>
      </div>
    </fieldset>
  )
}

// summary says in a few words what an entry holds.
function summary(form: ListForm, entry: ConfigListEntry): string {
  if (form.scalar) return typeof entry.value === "string" ? entry.value : ""
  const object = asObject(entry.value)
  return form.fields.map((f) => {
    const v = object[f.key]
    if (v === undefined || v === "") return ""
    if (f.kind === "choice") return f.choices?.find(([c]) => c === v)?.[1] ?? String(v)
    if (f.kind === "swatches") return ""
    return Array.isArray(v) ? v.join(" ") : String(v)
  }).filter(Boolean).join(" · ")
}

// ListCard is one list: config.yaml's entries, read-only with a lock, and
// those kept here, which can be edited and removed; and its Add.
export function ListCard({ list, baseUrl }: { list: ConfigList; baseUrl: string }) {
  const queryClient = useQueryClient()
  const form = formFor(list.key, list.name_field)
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
      {list.entries.length > 0 && <ul className="flex flex-col divide-y divide-lw-line" aria-label={list.title}>
        {list.entries.map((entry, index) => {
          const color = list.key === "workspaces" ? asObject(entry.value).color : undefined
          return <li key={entry.name} className="py-2" data-testid={`entry-${list.key}-${entry.name}`}>
            <div className="flex flex-wrap items-center gap-2">
              {form.ordered && <span className="w-5 font-lw-mono text-[11px] text-lw-text-2">{index + 1}</span>}
              {typeof color === "string" && <span className="h-2.5 w-2.5 rounded-full" style={{ background: color }} aria-hidden />}
              <span className="text-sm text-lw-text">{entry.name}</span>
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
            {editing === entry.name && <EntryForm list={list} form={form} entry={entry} onSave={save} onCancel={() => setEditing(null)} />}
          </li>
        })}
      </ul>}
      {list.entries.length === 0 && editing !== "" && <p className="text-sm text-lw-text-2">{form.empty}</p>}
      {notice && <p role="status" className="mt-1 text-xs text-lw-text-2">{notice}</p>}
      {error && <p role="alert" className="mt-1 text-xs text-lw-error">{error}</p>}
      {editing === "" ? <EntryForm list={list} form={form} onSave={save} onCancel={() => setEditing(null)} />
        : <button type="button" className={`${quietButton} mt-2 min-h-9`} onClick={() => { setNotice(""); setEditing("") }}>{add}</button>}
    </SettingsCard>
  )
}

function listFormsAdd(form: ListForm, list: ConfigList): string {
  return form.add === "Add" ? `Add to ${list.title.toLowerCase()}` : form.add
}

export function ListSettings({ baseUrl, lists }: { baseUrl: string; lists?: string[] }) {
  const query = useQuery({ queryKey: queryKeys.configLists(baseUrl), queryFn: () => fetchConfigLists(baseUrl), retry: false, enabled: lists?.length !== 0 })
  if (lists?.length === 0) return null
  return <>
    {query.isPending && <p role="status" className="text-sm text-lw-text-2">Loading lists…</p>}
    {query.isFetching && query.data && <p role="status" className="text-sm text-lw-text-2">Refreshing lists…</p>}
    {query.isError && <div role="alert" className="text-sm text-lw-error">
      <p>{query.error.message}{query.data ? " Showing previously loaded lists; they may be out of date." : ""}</p>
      <button type="button" className={quietButton} disabled={query.isFetching} onClick={() => void query.refetch()}>Retry</button>
    </div>}
    {query.data?.filter((list) => lists === undefined || lists.includes(list.key)).map((list) => <ListCard key={`${baseUrl}:${list.key}`} list={list} baseUrl={baseUrl} />)}
  </>
}
