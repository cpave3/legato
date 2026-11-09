import { useState } from "react"
import { Power } from "lucide-react"
import { SettingsCard, quietButton } from "./settings/SettingsUI"
import { restartLegato } from "../lib/restart"

// RestartLegato restarts this server's Legato, after the person confirms.
export function RestartLegato({ baseUrl }: { baseUrl: string }) {
  const [state, setState] = useState<"idle" | "confirm" | "restarting">("idle")
  const [error, setError] = useState("")
  const restart = async () => {
    setState("restarting")
    setError("")
    try {
      await restartLegato(baseUrl)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      setState("idle")
    }
  }
  return (
    <SettingsCard icon={Power} title="Restart Legato"
      description="Every Legato process of this server shuts down and starts again. Agents keep running, and this page reconnects.">
      <div className="flex flex-wrap items-center gap-2">
        {state === "idle" && <button className={quietButton} onClick={() => setState("confirm")}>Restart Legato</button>}
        {state === "confirm" && (
          <>
            <span className="text-sm text-lw-text">Restart now?</span>
            <button className={quietButton} onClick={() => void restart()}>Restart</button>
            <button className={quietButton} onClick={() => setState("idle")}>Cancel</button>
          </>
        )}
        {state === "restarting" && <span role="status" className="text-sm text-lw-text-2">Restarting…</span>}
      </div>
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
    </SettingsCard>
  )
}
