import { useState } from "react"
import { Check } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"
import { tints } from "../lib/tints"
import { cn } from "../lib/utils"

// TintPicker chooses this server's tint, which is saved on the server: every
// browser and phone that uses it shows the colour.
export function TintPicker({ tint, onChanged }: { tint?: string; onChanged?: () => void }) {
  const { baseUrl } = useServer()
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const current = tint ?? ""

  const choose = async (name: string) => {
    if (saving || name === current) return
    setSaving(true)
    setError("")
    try {
      const response = await apiFetch(baseUrl, "/api/settings/tint", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tint: name }),
      })
      if (!response.ok) throw new Error(await response.text())
      onChanged?.()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSaving(false)
    }
  }

  const swatch = "flex size-7 items-center justify-center rounded-full border transition-transform hover:scale-110 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-lw-accent"
  return (
    <div className="flex flex-col items-end gap-1">
      <div role="radiogroup" aria-label="Tint" className="flex flex-wrap justify-end gap-1.5">
        <button type="button" role="radio" aria-checked={current === ""} aria-label="None" title="None" disabled={saving}
          onClick={() => void choose("")}
          className={cn(swatch, "border-lw-line-strong bg-lw-ground text-lw-text-2", current === "" && "ring-2 ring-lw-text")}>
          {current === "" ? <Check size={13} /> : <span className="h-px w-3.5 rotate-45 bg-lw-faint" />}
        </button>
        {tints.map((option) => (
          <button key={option.name} type="button" role="radio" aria-checked={current === option.name} aria-label={option.label} title={option.label}
            disabled={saving} onClick={() => void choose(option.name)}
            className={cn(swatch, "border-transparent text-black/70", current === option.name && "ring-2 ring-lw-text")}
            style={{ backgroundColor: option.color }}>
            {current === option.name && <Check size={13} />}
          </button>
        ))}
      </div>
      {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
    </div>
  )
}
