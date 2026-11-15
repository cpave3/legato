import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useServer } from "../../hooks/useServer"
import { appThemes } from "../../lib/appTheme"
import { fetchThemePreference, saveThemePreference } from "../../lib/serverTheme"
import { queryKeys } from "../../lib/query"
import { quietButton } from "./SettingsUI"

export function ServerThemeSettings() {
  const { baseUrl } = useServer()
  return <PreferenceEditor key={baseUrl} baseUrl={baseUrl} />
}

function PreferenceEditor({ baseUrl }: { baseUrl: string }) {
  const client = useQueryClient()
  const preference = useQuery({ queryKey: queryKeys.themePreference(baseUrl), queryFn: () => fetchThemePreference(baseUrl) })
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  async function save(id: string) {
    if (!preference.data) return
    setError(""); setBusy(true)
    try {
      const saved = await saveThemePreference(baseUrl, id, preference.data.revision)
      client.setQueryData(queryKeys.themePreference(baseUrl), saved)
      await client.invalidateQueries({ queryKey: queryKeys.meta(baseUrl) })
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not save the server theme preference.")
    } finally { setBusy(false) }
  }
  return <div className="space-y-2">
    <label htmlFor="server-preferred-theme" className="block text-sm text-lw-text">Preferred theme</label>
    <p id="server-theme-description" className="text-xs text-lw-text-2">Used by devices that choose Follow server.</p>
    {preference.isPending ? <p role="status" className="text-sm text-lw-text-2">Loading preference…</p> : preference.data &&
      <select id="server-preferred-theme" aria-describedby="server-theme-description" value={preference.data.id} disabled={busy}
        onChange={event => void save(event.target.value)}
        className="min-h-10 max-w-full rounded-lg border border-lw-line-strong bg-lw-raised px-3 text-sm text-lw-text disabled:opacity-50">
        <option value="">No preference</option>
        {appThemes.map(theme => <option key={theme.id} value={theme.id}>{theme.name}</option>)}
      </select>}
    {(error || preference.error) && <div>
      <p role="alert" className="text-sm text-lw-error">{error || preference.error?.message}</p>
      <button type="button" className={`${quietButton} mt-2`} disabled={busy || preference.isFetching} onClick={() => { setError(""); void preference.refetch() }}>Reload preference</button>
    </div>}
  </div>
}
