import { useCallback, useEffect, useState } from "react"
import { Brain } from "lucide-react"
import { SettingsCard } from "./settings/SettingsUI"
import { apiFetch } from "../lib/api"

interface EngramBound {
  name: string
  default?: boolean
  missing?: boolean
}

interface EngramBindingPlace {
  key: string
  label: string
  kind: "repo" | "folder"
  engrams: EngramBound[]
}

interface EngramBindingsView {
  status: { enabled: boolean; found?: boolean; binary?: string; version?: string; store?: string; error?: string; mode?: string }
  places: EngramBindingPlace[]
}

const smallButton = "rounded px-2 py-1 text-xs text-lw-text-2 transition-colors hover:bg-lw-raised hover:text-lw-text"

// EngramBindingsSettings lists the engrams bound to each repository and
// folder, which the spawn dialog offers first there. Binding happens
// through the dialog's Remember; here an engram is unbound, made the
// default, or the default cleared.
export function EngramBindingsSettings({ baseUrl }: { baseUrl: string }) {
  const [view, setView] = useState<EngramBindingsView | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const response = await apiFetch(baseUrl, "/api/engram-bindings")
      if (!response.ok) throw new Error(await response.text())
      setView(await response.json())
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }, [baseUrl])

  useEffect(() => { void load() }, [load])

  const act = async (path: string, init: RequestInit) => {
    setError(null)
    try {
      const response = await apiFetch(baseUrl, path, init)
      if (!response.ok) throw new Error(await response.text())
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }
  const setDefault = (place: string, engram: string | null) =>
    act("/api/engram-bindings/default", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ place, engram }) })
  const unbind = (place: string, engram: string) =>
    act(`/api/engram-bindings?${new URLSearchParams({ place, engram })}`, { method: "DELETE" })

  const status = view?.status
  return (
    <SettingsCard id="engram-bindings" icon={Brain} title="Engram bindings"
      description="The engrams the spawn dialog offers first in each repository or folder, and the one it picks there. Tick Remember in the spawn dialog to bind one.">
      <div className="flex flex-col gap-3">
        {error && <p role="alert" className="rounded border border-lw-danger-line bg-lw-error-soft px-2 py-1.5 text-xs text-lw-danger">{error}</p>}
        {status && (
          <p className="text-xs text-lw-text-2">
            {status.found
              ? <>engram {status.version} at <span className="font-lw-mono">{status.binary}</span>, store <span className="font-lw-mono">{status.store}</span></>
              : status.mode === "off"
                ? "Engrams are off (engram.enabled)."
                : status.error || "engram was not found."}
          </p>
        )}
        {view && view.places.length === 0 && <p className="text-xs text-lw-faint">No engram is bound yet.</p>}
        {view && view.places.length > 0 && (
          <ul className="divide-y divide-lw-line rounded-lg border border-lw-line">
            {view.places.map((place) => {
              const hasDefault = place.engrams.some((engram) => engram.default)
              return (
                <li key={place.key} aria-label={place.label} className="flex flex-col gap-1.5 px-3 py-2">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-lw-mono text-sm text-lw-text">{place.label}</span>
                    <span className="text-[11px] text-lw-faint">{place.kind}</span>
                    {hasDefault && <button type="button" className={`${smallButton} ml-auto`} onClick={() => void setDefault(place.key, null)}>Clear the default</button>}
                  </div>
                  <ul className="flex flex-col gap-1">
                    {place.engrams.map((engram) => (
                      <li key={engram.name} className="flex items-center gap-2 text-sm">
                        <span className={engram.missing ? "text-lw-faint line-through" : "text-lw-text"}>{engram.name}</span>
                        {engram.default && <span className="rounded bg-lw-selected px-1.5 py-px text-[11px] text-lw-text">default</span>}
                        {engram.missing && <span className="rounded border border-lw-needs/50 px-1.5 py-px text-[11px] text-lw-needs" title="engram no longer lists this engram">missing</span>}
                        <span className="ml-auto flex items-center gap-1">
                          {!engram.default && (
                            <button type="button" aria-label={`Make ${engram.name} the default`} className={smallButton} onClick={() => void setDefault(place.key, engram.name)}>Make default</button>
                          )}
                          <button type="button" aria-label={`Unbind ${engram.name}`} className={smallButton} onClick={() => void unbind(place.key, engram.name)}>Unbind</button>
                        </span>
                      </li>
                    ))}
                  </ul>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </SettingsCard>
  )
}
