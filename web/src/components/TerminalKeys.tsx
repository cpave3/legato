import { useEffect, useState } from "react"
import { cn } from "../lib/utils"

interface TerminalKeysProps {
  // Sends tmux key names to the agent's session, as send_keys takes them.
  onSendKeys: (keys: string) => void
}

const keys = [
  { label: "Esc", name: "Escape", keys: "Escape" },
  { label: "⇧Tab", name: "Shift+Tab", keys: "BTab" },
  { label: "↑", name: "Up", keys: "Up" },
  { label: "↓", name: "Down", keys: "Down" },
]

const key = "h-11 min-w-0 flex-1 rounded-lg border border-lw-line-strong bg-lw-raised font-lw-mono text-xs text-lw-text-2 transition-colors active:bg-lw-selected"

// The keys a phone's keyboard lacks, for an agent in its terminal. Ctrl
// waits for the next character typed, in the message box or the terminal,
// and sends it as Ctrl and that character instead.
export function TerminalKeys({ onSendKeys }: TerminalKeysProps) {
  const [ctrl, setCtrl] = useState(false)

  useEffect(() => {
    if (!ctrl) return
    const onBeforeInput = (event: InputEvent) => {
      const typed = event.data
      if (!typed || typed.length !== 1) return
      event.preventDefault()
      setCtrl(false)
      onSendKeys(`C-${typed.toLowerCase()}`)
    }
    document.addEventListener("beforeinput", onBeforeInput, true)
    return () => document.removeEventListener("beforeinput", onBeforeInput, true)
  }, [ctrl, onSendKeys])

  return (
    <div role="group" aria-label="Terminal keys" className="flex shrink-0 gap-1.5 px-3 pt-2">
      {keys.map((k) => (
        <button key={k.keys} type="button" aria-label={k.name} onClick={() => onSendKeys(k.keys)} className={key}>
          {k.label}
        </button>
      ))}
      <button
        type="button"
        aria-pressed={ctrl}
        title="Sends the next character you type with Ctrl"
        onClick={() => setCtrl((on) => !on)}
        className={cn(key, ctrl && "border-lw-accent-ring bg-lw-accent-soft text-lw-accent-text")}
      >
        Ctrl
      </button>
    </div>
  )
}
