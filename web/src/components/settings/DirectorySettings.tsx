import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Folder } from "lucide-react"
import { apiFetch } from "../../lib/api"
import { saveDirectoryPreset, type DirectoryPresets } from "../../lib/spawnDirectories"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"

export function DirectorySettings({ baseUrl }: { baseUrl: string }) {
  const directories = useQuery({ queryKey: [baseUrl, "settings-directory-presets"], queryFn: async () => {
    const response = await apiFetch(baseUrl, "/api/directories/presets")
    if (!response.ok) throw new Error("Could not load directory presets.")
    return response.json() as Promise<DirectoryPresets>
  }, retry: false })
  const [path, setPath] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [saved, setSaved] = useState(false)
  const save = async () => {
    setBusy(true)
    setError("")
    setSaved(false)
    try {
      await saveDirectoryPreset(baseUrl, path.trim())
      setPath("")
      setSaved(true)
      await directories.refetch()
    } catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)) }
    finally { setBusy(false) }
  }
  return <SettingsCard id="directory-presets" icon={Folder} title="Directory presets" description="Saved folders offered when starting agents. Configured repository folders are pinned by config.yaml.">
    {directories.isPending && <p role="status">Loading directory presets…</p>}
    {directories.error && <div role="alert"><p>{directories.error.message}{directories.data ? " Showing last loaded folders." : ""}</p><button className={quietButton} onClick={() => void directories.refetch()}>Retry directories</button></div>}
    <ul className="divide-y divide-lw-line">{directories.data?.presets.map((preset) => <li key={preset.path} className="py-2 text-sm"><span>{preset.name}</span><code className="block break-all text-xs text-lw-text-2">{preset.display}</code><span className="text-xs text-lw-text-2">{preset.saved ? "set here" : "config.yaml · pinned"}</span></li>)}</ul>
    <label className="mt-3 flex flex-col gap-1 text-sm">Directory path<input value={path} onChange={(event) => { setPath(event.target.value); setSaved(false) }} disabled={busy} className="min-h-11 rounded-lg border border-lw-line-strong bg-lw-ground px-3" /></label>
    <div className="mt-2 flex gap-2"><button className={primaryButton} disabled={busy || !path.trim()} onClick={() => void save()}>{busy ? "Saving…" : "Save directory"}</button><button className={quietButton} disabled={busy || !path} onClick={() => setPath("")}>Cancel</button></div>
    {error && <p role="alert" className="mt-2 text-sm text-lw-error">{error}</p>}
    {saved && <p role="status" className="mt-2 text-sm text-lw-text-2">Directory saved.</p>}
  </SettingsCard>
}
