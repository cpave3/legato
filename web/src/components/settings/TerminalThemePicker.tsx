import { useState } from "react"
import { cn } from "../../lib/utils"
import { setTerminalTheme, terminalTheme, terminalThemes } from "../../lib/terminalTheme"

// Chooses the colours of agents' terminals in this browser. A terminal takes
// the choice the next time it opens.
export function TerminalThemePicker() {
  const [chosen, setChosen] = useState(() => terminalTheme().id)
  return (
    <div role="radiogroup" aria-label="Terminal theme" className="flex flex-wrap gap-0.5 rounded-md border border-lw-line bg-lw-ground p-0.5">
      {terminalThemes.map((option) => (
        <button
          key={option.id}
          type="button"
          role="radio"
          aria-checked={chosen === option.id}
          onClick={() => {
            setChosen(option.id)
            setTerminalTheme(option.id)
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
  )
}
