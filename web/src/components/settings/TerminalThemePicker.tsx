import { useState, useSyncExternalStore } from "react"
import { cn } from "../../lib/utils"
import { subscribeTheme } from "../../lib/appTheme"
import { setTerminalTheme, terminalThemePreference, terminalThemes } from "../../lib/terminalTheme"
import { themePickerKeys } from "./themePickerKeys"

export function TerminalThemePicker() {
  const chosen = useSyncExternalStore(subscribeTheme, terminalThemePreference)
  const [error, setError] = useState("")
  const options = [{ id: "app" as const, name: "Follow app", theme: { background: undefined as string | undefined } }, ...terminalThemes]
  return (
    <div>
    <div role="radiogroup" aria-label="Terminal theme" onKeyDown={themePickerKeys} className="flex flex-wrap gap-0.5 rounded-md border border-lw-line bg-lw-ground p-0.5">
      {options.map((option) => (
        <button
          key={option.id}
          type="button"
          role="radio"
          aria-checked={chosen === option.id}
          tabIndex={chosen === option.id ? 0 : -1}
          onClick={() => {
            setError("")
            try { setTerminalTheme(option.id) }
            catch (e) { setError(e instanceof Error ? e.message : "Could not save terminal theme") }
          }}
          className={cn(
            "flex h-8 items-center gap-1.5 rounded px-2.5 text-xs transition-colors",
            chosen === option.id ? "bg-lw-selected text-lw-text" : "text-lw-text-2 hover:text-lw-text",
          )}
        >
          <span aria-hidden="true" className="size-2.5 rounded-full border border-lw-line-strong" style={{ background: option.theme.background }} />
          {option.name}
        </button>
      ))}
    </div>
    {error && <p role="alert" className="mt-2 text-sm text-lw-error">{error}</p>}
    </div>
  )
}
