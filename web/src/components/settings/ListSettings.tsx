import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { List, Lock } from "lucide-react"
import { fetchConfigLists, removeConfigListEntry, saveConfigListEntry, type ConfigList, type ConfigListEntry } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"

function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

type EntryField = { key: string; label: string; kind?: "multiline" | "strings" | "bool" | "choice"; choices?: [string, string][] }
const commonFields: Record<string, EntryField[]> = {
  workspaces: [{ key: "color", label: "Color" }, { key: "path", label: "Path" }],
  adapters: [
    { key: "launch_args", label: "Launch arguments (one per line)", kind: "strings" },
    { key: "models", label: "Models (one per line)", kind: "strings" },
    { key: "native_bridge", label: "Native bridge", kind: "bool" },
  ],
  credentials: [{ key: "secret", label: "Secret reference" }, { key: "profile", label: "Profile" }, { key: "type", label: "Type" }],
  credential_sources: [{ key: "profile", label: "Profile" }],
  "agents.sandbox.mounts": [{ key: "access", label: "Access" }],
}
const fieldClass = "min-h-10 min-w-0 w-full rounded-lg border border-lw-line-strong bg-lw-ground px-2 py-1 text-sm text-lw-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-lw-accent-ring disabled:opacity-60"

// JSON is the single draft: typed edits replace only the field being edited.
function EntryEditor({ list, entry, onSave, onCancel }: { list: ConfigList; entry?: ConfigListEntry; onSave: (name: string, value: unknown) => Promise<void>; onCancel: () => void }) {
  const [name, setName] = useState(entry?.name ?? "")
  const current = pretty(entry ? entry.value : list.key === "agents.tmux_options" ? "" : {})
  const [text, setText] = useState(current)
  const [baseline, setBaseline] = useState(current)
  const [observed, setObserved] = useState(current)
  if (observed !== current) {
    setObserved(current)
    if (text === baseline || text === current) { setText(current); setBaseline(current) }
  }
  const conflict = text !== baseline && current !== baseline && text !== current
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)
  let value: unknown
  let parseError = ""
  try { value = JSON.parse(text) } catch { parseError = "The fields are not valid JSON." }
  const object = value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null
  const fields = commonFields[list.key] ?? []
  const update = (key: string, next: unknown) => {
    const changed = { ...object }
    if (next === undefined) delete changed[key]
    else changed[key] = next
    setText(pretty(changed))
    setError("")
  }
  const save = async () => {
    if (saving || parseError || conflict) return
    setError("")
    setSaving(true)
    try {
      await onSave(name.trim(), value)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSaving(false)
    }
  }
  return (
    <fieldset disabled={saving} className="mt-2 flex min-w-0 flex-col gap-2 border-t border-lw-line pt-3">
      <label className="flex flex-col gap-1 text-xs text-lw-text-2">
        {list.name_field === "vault" ? "Vault" : list.name_field === "path" ? "Path" : list.name_field === "pattern" ? "Pattern" : "Name"}
        <input aria-label={`${list.title} name`} value={name} disabled={Boolean(entry)} onChange={(e) => setName(e.target.value)}
          className={fieldClass} />
      </label>
      {fields.map((field) => <label key={field.key} className="flex min-w-0 flex-col gap-1 text-xs text-lw-text-2">
        {field.label}
        {field.kind === "choice"
          ? <select className={fieldClass} disabled={!object} value={typeof object?.[field.key] === "string" ? object[field.key] as string : ""} onChange={(e) => update(field.key, e.target.value === "" ? undefined : e.target.value)}>
            {field.choices?.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
          : field.kind === "bool"
          ? <select className={fieldClass} disabled={!object} value={typeof object?.[field.key] === "boolean" ? String(object[field.key]) : ""} onChange={(e) => update(field.key, e.target.value === "" ? undefined : e.target.value === "true")}>
            <option value="">Default (enabled)</option><option value="true">Enabled</option><option value="false">Disabled</option>
          </select>
          : field.kind === "multiline" || field.kind === "strings"
            ? <textarea className={fieldClass} rows={3} disabled={!object}
              value={field.kind === "strings" ? (Array.isArray(object?.[field.key]) ? (object[field.key] as string[]).join("\n") : "") : typeof object?.[field.key] === "string" ? object[field.key] as string : ""}
              onChange={(e) => update(field.key, field.kind === "strings" ? (e.target.value === "" ? [] : e.target.value.split("\n")) : e.target.value)} />
            : <input className={fieldClass} disabled={!object} value={typeof object?.[field.key] === "string" ? object[field.key] as string : ""} onChange={(e) => update(field.key, e.target.value)} />}
      </label>)}
      {list.key === "credentials" && <p className="text-xs text-lw-text-2">Enter a secret reference, such as legato://name or op://vault/item/field, not the secret value.</p>}
      {list.key === "agents.tmux_options" && <label className="flex flex-col gap-1 text-xs text-lw-text-2">
        Value
        <input className={fieldClass} disabled={typeof value !== "string"} value={typeof value === "string" ? value : ""} onChange={(e) => setText(pretty(e.target.value))} />
      </label>}
      <label className="flex flex-col gap-1 text-xs text-lw-text-2">
        Advanced JSON · fields as in config.yaml
        <textarea aria-label={`${list.title} fields`} value={text} onChange={(e) => { setText(e.target.value); setError("") }} rows={5}
          className={`${fieldClass} font-mono text-xs`} />
      </label>
      {conflict && <div className="text-sm text-lw-needs">
        <p role="status">This entry changed elsewhere.</p>
        <div className="mt-2 flex flex-wrap gap-2">
          <button type="button" className={quietButton} onClick={() => { setText(current); setBaseline(current) }}>Reload value</button>
          <button type="button" className={quietButton} onClick={() => setBaseline(current)}>Keep my edit</button>
        </div>
      </div>}
      {(error || parseError) && <p role="alert" className="text-xs text-lw-error">{error || parseError}</p>}
      <div className="flex gap-2">
        <button type="button" className={primaryButton} onClick={() => void save()} disabled={saving || !name.trim() || Boolean(parseError) || conflict}>{saving ? "Saving…" : "Save"}</button>
        <button type="button" className={quietButton} onClick={onCancel}>Cancel</button>
      </div>
    </fieldset>
  )
}

// ListCard is one list: the file's entries, locked, and the database's,
// which can be edited and removed; and Add.
export function ListCard({ list, baseUrl }: { list: ConfigList; baseUrl: string }) {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<string | null>(null)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const refresh = () => queryClient.invalidateQueries({ queryKey: queryKeys.configLists(baseUrl) })
  const save = async (name: string, value: unknown) => {
    setNotice("")
    const result = await saveConfigListEntry(baseUrl, list.key, name, value)
    if (result?.status === "needs_signature") {
      setNotice("Awaiting approval — approve this rule change on your phone or with a passkey.")
      return
    }
    setNotice(list.mode === "restart" ? "Saved — applies at restart." : list.mode === "new_agents" ? "Saved — applies to new agents." : "Saved.")
    setEditing(null)
    await refresh()
  }
  const remove = async (name: string) => {
    setError("")
    try {
      const result = await removeConfigListEntry(baseUrl, list.key, name)
      setNotice(result?.status === "needs_signature" ? "Awaiting approval — approve this removal on your phone or with a passkey." : "Removed.")
      await refresh()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }
  return (
    <SettingsCard id={`list-${list.key}`} icon={List} title={list.title} description={<><code>{list.key}</code>{list.mode === "restart" ? " · applies at restart" : list.mode === "new_agents" ? " · reaches agents started after a change" : ""}</>}>
      <ul className="flex flex-col divide-y divide-lw-line" aria-label={list.title}>
        {list.entries.map((entry) => (
          <li key={entry.name} className="py-2" data-testid={`entry-${list.key}-${entry.name}`}>
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm text-lw-text">{entry.name}</span>
              {entry.pinned
                ? <span className="inline-flex items-center gap-1 rounded bg-lw-raised px-1.5 py-0.5 text-[11px] text-lw-text-2"><Lock size={10} aria-hidden /> config.yaml</span>
                : entry.source !== "default" && <span className="rounded bg-lw-accent-soft px-1.5 py-0.5 text-[11px] text-lw-accent-text">set here</span>}
              {!entry.pinned && (
                <span className="ml-auto flex gap-1">
                  <button type="button" className={quietButton} onClick={() => { setNotice(""); setEditing(entry.name) }}>Edit</button>
                  <button type="button" className={quietButton} onClick={() => void remove(entry.name)} aria-label={`Remove ${entry.name}`}>Remove</button>
                </span>
              )}
            </div>
            {editing === entry.name && <EntryEditor list={list} entry={entry} onSave={save} onCancel={() => setEditing(null)} />}
          </li>
        ))}
      </ul>
      {list.entries.length === 0 && <p className="text-xs text-lw-faint">None.</p>}
      {notice && <p role="status" className="mt-1 text-xs text-lw-text-2">{notice}</p>}
      {error && <p role="alert" className="mt-1 text-xs text-lw-error">{error}</p>}
      {editing === "" ? <EntryEditor list={list} onSave={save} onCancel={() => setEditing(null)} />
        : <button type="button" className={`${quietButton} mt-2`} onClick={() => { setNotice(""); setEditing("") }}>Add to {list.title.toLowerCase()}</button>}
    </SettingsCard>
  )
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
