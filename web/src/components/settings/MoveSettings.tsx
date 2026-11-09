import { useEffect, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { DatabaseZap } from "lucide-react"
import { fetchMovable, moveSettings, type SettingsMove } from "../../lib/configSettings"
import { SettingsCard, primaryButton } from "./SettingsUI"

const entryKey = (e: { list: string; name: string }) => `${e.list}/${e.name}`

// MoveSettings offers to move every setting config.yaml pins into the
// database, each ticked; untick any to keep pinned. Legato restarts after.
export function MoveSettings({ baseUrl }: { baseUrl: string }) {
  const movable = useQuery({ queryKey: [baseUrl, "config-move"], queryFn: () => fetchMovable(baseUrl), retry: false })
  const [unticked, setUnticked] = useState<Set<string>>(new Set())
  const [state, setState] = useState<"idle" | "moving" | "restarting">("idle")
  const [error, setError] = useState("")
  useEffect(() => setUnticked(new Set()), [movable.data])
  const data = movable.data
  if (!data || (data.settings.length === 0 && data.entries.length === 0)) return null
  const all = [...data.settings, ...data.entries.map(entryKey)]
  const toggle = (key: string) => setUnticked((u) => {
    const next = new Set(u)
    if (next.has(key)) next.delete(key)
    else next.add(key)
    return next
  })
  const move = async () => {
    const chosen: SettingsMove = {
      settings: data.settings.filter((k) => !unticked.has(k)),
      entries: data.entries.filter((e) => !unticked.has(entryKey(e))),
    }
    setState("moving")
    setError("")
    try {
      await moveSettings(baseUrl, chosen)
      setState("restarting")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setState("idle")
    }
  }
  const count = all.length - unticked.size
  return (
    <SettingsCard id="move" icon={DatabaseZap} title="Move settings into the database"
      description="These are set in config.yaml. Moved, they can be changed here and by the Reeve; config.yaml is rewritten without them, keeping its comments, with a backup beside it. Legato restarts, and goes back to the old file if it does not start.">
      <ul className="flex flex-col gap-1" aria-label="Settings to move">
        {all.map((key) => (
          <li key={key}>
            <label className="flex items-center gap-2 text-sm text-lw-text">
              <input type="checkbox" checked={!unticked.has(key)} onChange={() => toggle(key)} />
              <code className="text-xs">{key}</code>
            </label>
          </li>
        ))}
      </ul>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button type="button" className={primaryButton} disabled={count === 0 || state !== "idle"} onClick={() => void move()}>
          Move {count} to the database and restart
        </button>
        {state === "restarting" && <span role="status" className="text-sm text-lw-text-2">Restarting…</span>}
      </div>
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
    </SettingsCard>
  )
}
