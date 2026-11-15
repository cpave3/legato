import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { DatabaseZap } from "lucide-react"
import { fetchMovable, moveSettings, type SettingsMove } from "../../lib/configSettings"
import { useConfirm } from "../../hooks/useConfirm"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"

const entryKey = (e: { list: string; name: string }) => `${e.list}/${e.name}`

// MoveSettings offers to move every setting config.yaml pins into the
// database, each ticked; untick any to keep pinned. Legato restarts after.
export function MoveSettings({ baseUrl }: { baseUrl: string }) {
  const { confirm, dialog } = useConfirm()
  const movable = useQuery({ queryKey: [baseUrl, "config-move"], queryFn: () => fetchMovable(baseUrl), retry: false })
  const [unticked, setUnticked] = useState<Set<string>>(new Set())
  const [state, setState] = useState<"idle" | "confirming" | "moving" | "restarting">("idle")
  const [error, setError] = useState("")
  const data = movable.data ?? { settings: [], entries: [] }
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
    setState("confirming")
    if (!await confirm({ title: "Move settings and restart?", message: `Move ${chosen.settings.length + chosen.entries.length} items out of config.yaml into the database? Legato will restart and connected clients will disconnect. A backup of config.yaml is kept.`, confirmLabel: "Move and restart", destructive: true })) {
      setState("idle")
      return
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
  const count = all.filter((key) => !unticked.has(key)).length
  return (
    <SettingsCard id="move" icon={DatabaseZap} title="Move settings into the database"
      description="These are set in config.yaml. Moved, they can be changed here and by the Reeve; config.yaml is rewritten without them, keeping its comments, with a backup beside it. Legato restarts, and goes back to the old file if it does not start.">
      {dialog}
      {movable.isPending && <p role="status" className="text-sm text-lw-text-2">Loading movable settings…</p>}
      {movable.isError && <div role="alert" className="text-sm text-lw-error">
        Could not load movable settings: {movable.error.message}{movable.data ? " Showing last checked items. Retry before moving." : ""}
        <button type="button" className={quietButton} onClick={() => void movable.refetch()}>Retry</button>
      </div>}
      {movable.isSuccess && all.length === 0 && <p className="text-sm text-lw-text-2">No settings can move from config.yaml.</p>}
      <ul className="flex flex-col gap-1" aria-label="Settings to move">
        {all.map((key) => (
          <li key={key}>
            <label className="flex items-center gap-2 text-sm text-lw-text">
              <input type="checkbox" disabled={state !== "idle"} checked={!unticked.has(key)} onChange={() => toggle(key)} />
              <code className="text-xs">{key}</code>
            </label>
          </li>
        ))}
      </ul>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button type="button" className={primaryButton} disabled={count === 0 || state !== "idle" || movable.isError || movable.isFetching} onClick={() => void move()}>
          Move {count} to the database and restart
        </button>
        {state === "moving" && <span role="status" className="text-sm text-lw-text-2">Moving…</span>}
        {state === "restarting" && <span role="status" className="text-sm text-lw-text-2">Restarting…</span>}
      </div>
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
    </SettingsCard>
  )
}
