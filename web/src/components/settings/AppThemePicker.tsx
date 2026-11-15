import { useState, useSyncExternalStore } from "react"
import { appTheme, appThemes, setAppTheme, subscribeTheme, type AppThemeId } from "../../lib/appTheme"
import { cn } from "../../lib/utils"
import { themePickerKeys } from "./themePickerKeys"

export function AppThemePicker() {
  const chosen = useSyncExternalStore(subscribeTheme, () => appTheme().id)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  return (
    <div>
    <div role="radiogroup" aria-label="App theme" onKeyDown={themePickerKeys} className="flex flex-wrap gap-1">
      {appThemes.map(option => (
        <button key={option.id} type="button" role="radio" aria-checked={chosen === option.id}
          tabIndex={chosen === option.id ? 0 : -1}
          disabled={busy}
          onClick={async () => {
            setError(""); setBusy(true)
            try { await setAppTheme(option.id as AppThemeId) }
            catch (e) { setError(e instanceof Error ? e.message : "Could not save theme") }
            finally { setBusy(false) }
          }}
          className={cn("rounded-md border px-3 py-2 text-sm", chosen === option.id ? "border-lw-accent bg-lw-selected text-lw-text" : "border-lw-line text-lw-text-2 hover:text-lw-text")}>
          {option.name}
        </button>
      ))}
    </div>
    {error && <p role="alert" className="mt-2 text-sm text-lw-error">{error}</p>}
    </div>
  )
}
