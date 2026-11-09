import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { List, Lock } from "lucide-react"
import { fetchConfigLists, removeConfigListEntry, saveConfigListEntry, type ConfigList, type ConfigListEntry } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"

function pretty(value: unknown): string {
  const shown = { ...(value as Record<string, unknown>) }
  return JSON.stringify(shown, null, 2)
}

// EntryEditor edits one entry's fields as JSON, in config.yaml's shape.
function EntryEditor({ list, entry, onSave, onCancel }: { list: ConfigList; entry?: ConfigListEntry; onSave: (name: string, value: unknown) => Promise<void>; onCancel: () => void }) {
  const [name, setName] = useState(entry?.name ?? "")
  const [text, setText] = useState(entry ? pretty(entry.value) : "{\n}")
  const [error, setError] = useState("")
  const save = async () => {
    let value: unknown
    try {
      value = JSON.parse(text)
    } catch {
      setError("The fields are not valid JSON.")
      return
    }
    try {
      await onSave(name.trim(), value)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }
  return (
    <div className="mt-2 flex flex-col gap-2 rounded-lg border border-lw-line p-3">
      <label className="flex flex-col gap-1 text-xs text-lw-text-2">
        Name
        <input aria-label={`${list.title} name`} value={name} disabled={Boolean(entry)} onChange={(e) => setName(e.target.value)}
          className="min-h-9 rounded-lg border border-lw-line bg-lw-ground px-2 text-sm text-lw-text disabled:opacity-60" />
      </label>
      <label className="flex flex-col gap-1 text-xs text-lw-text-2">
        Fields, as in config.yaml (JSON)
        <textarea aria-label={`${list.title} fields`} value={text} onChange={(e) => setText(e.target.value)} rows={5}
          className="rounded-lg border border-lw-line bg-lw-ground px-2 py-1 font-mono text-xs text-lw-text" />
      </label>
      {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
      <div className="flex gap-2">
        <button type="button" className={primaryButton} onClick={() => void save()} disabled={!name.trim()}>Save</button>
        <button type="button" className={quietButton} onClick={onCancel}>Cancel</button>
      </div>
    </div>
  )
}

// ListCard is one list: the file's entries, locked, and the database's,
// which can be edited and removed; and Add.
function ListCard({ list, baseUrl }: { list: ConfigList; baseUrl: string }) {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<string | null>(null)
  const [error, setError] = useState("")
  const refresh = () => queryClient.invalidateQueries({ queryKey: queryKeys.configLists(baseUrl) })
  const save = async (name: string, value: unknown) => {
    await saveConfigListEntry(baseUrl, list.key, name, value)
    setEditing(null)
    await refresh()
  }
  const remove = async (name: string) => {
    setError("")
    try {
      await removeConfigListEntry(baseUrl, list.key, name)
      await refresh()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }
  return (
    <SettingsCard icon={List} title={list.title} description={<><code>{list.key}</code>{list.mode === "restart" ? " · applies at restart" : list.mode === "new_agents" ? " · reaches agents started after a change" : ""}</>}>
      <ul className="flex flex-col divide-y divide-lw-line" aria-label={list.title}>
        {list.entries.map((entry) => (
          <li key={entry.name} className="py-2" data-testid={`entry-${list.key}-${entry.name}`}>
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm text-lw-text">{entry.name}</span>
              {entry.pinned
                ? <span className="inline-flex items-center gap-1 rounded bg-lw-raised px-1.5 py-0.5 text-[11px] text-lw-text-2"><Lock size={10} aria-hidden /> config.yaml</span>
                : <span className="rounded bg-lw-accent-soft px-1.5 py-0.5 text-[11px] text-lw-accent-text">set here</span>}
              {!entry.pinned && (
                <span className="ml-auto flex gap-1">
                  <button type="button" className={quietButton} onClick={() => setEditing(entry.name)}>Edit</button>
                  <button type="button" className={quietButton} onClick={() => void remove(entry.name)} aria-label={`Remove ${entry.name}`}>Remove</button>
                </span>
              )}
            </div>
            {editing === entry.name && <EntryEditor list={list} entry={entry} onSave={save} onCancel={() => setEditing(null)} />}
          </li>
        ))}
      </ul>
      {list.entries.length === 0 && <p className="text-xs text-lw-faint">None.</p>}
      {error && <p role="alert" className="mt-1 text-xs text-lw-error">{error}</p>}
      {editing === "" ? <EntryEditor list={list} onSave={save} onCancel={() => setEditing(null)} />
        : <button type="button" className={`${quietButton} mt-2`} onClick={() => setEditing("")}>Add to {list.title.toLowerCase()}</button>}
    </SettingsCard>
  )
}

// ListSettings is every list of named entries the server keeps.
export function ListSettings({ baseUrl }: { baseUrl: string }) {
  const lists = useQuery({ queryKey: queryKeys.configLists(baseUrl), queryFn: () => fetchConfigLists(baseUrl), retry: false })
  if (!lists.data) return null
  return <>{lists.data.map((list) => <ListCard key={list.key} list={list} baseUrl={baseUrl} />)}</>
}
