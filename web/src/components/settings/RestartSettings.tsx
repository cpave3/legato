import { useState } from "react"
import { Link } from "react-router-dom"
import { useQueryClient } from "@tanstack/react-query"
import { Power, RotateCcw } from "lucide-react"
import { restartLegato } from "../../lib/restart"
import { resetConfigSetting, setConfigSetting } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"
import { formatValue } from "../../lib/settingValues"
import { undoIsReset, type WaitingChange } from "../../lib/pendingRestart"
import { SettingsCard, quietButton } from "./SettingsUI"

// useRestart restarts Legato after the person confirms.
function useRestart(baseUrl: string) {
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
  return { state, error, ask: () => setState("confirm"), cancel: () => setState("idle"), restart }
}

const amberButton = "inline-flex min-h-8 items-center justify-center rounded-lg bg-lw-needs px-3 text-[13px] font-medium text-lw-ground hover:opacity-90 disabled:opacity-60"

// RestartBar sits on every Settings page while changes wait for a
// restart: it names up to three, counts the rest, and restarts Legato.
export function RestartBar({ changes, baseUrl }: { changes: WaitingChange[]; baseUrl: string }) {
  const restart = useRestart(baseUrl)
  if (changes.length === 0) return null
  const named = changes.slice(0, 3).map((c) => c.name).join(", ")
  const more = changes.length > 3 ? ` and ${changes.length - 3} more` : ""
  const lead = changes.length === 1 ? "1 change applies when Legato restarts:" : `${changes.length} changes apply when Legato restarts:`
  return <div role="region" aria-label="Changes waiting for a restart" className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-lw-line bg-lw-needs-soft px-4 py-2.5 text-[13.5px] text-lw-needs md:px-8">
    <RotateCcw size={15} aria-hidden />
    <p className="min-w-0 flex-1"><span className="font-medium">{lead}</span> {named}{more}.</p>
    <span className="flex items-center gap-2">
      {restart.state === "idle" && <>
        <Link to="#system/restart" className="px-2 text-[13px] text-lw-needs hover:underline">Show</Link>
        <button type="button" className={amberButton} onClick={restart.ask}>Restart Legato</button>
      </>}
      {restart.state === "confirm" && <>
        <span>Restart now?</span>
        <button type="button" className={amberButton} onClick={() => void restart.restart()}>Restart</button>
        <button type="button" className="px-2 text-[13px] hover:underline" onClick={restart.cancel}>Cancel</button>
      </>}
      {restart.state === "restarting" && <span role="status">Restarting…</span>}
    </span>
    {restart.error && <p role="alert" className="w-full text-xs text-lw-error">{restart.error}</p>}
  </div>
}

// RestartCard is System › Restart: every change that waits, with its value
// then and now and Undo, and Restart Legato.
export function RestartCard({ changes, baseUrl }: { changes: WaitingChange[]; baseUrl: string }) {
  const restart = useRestart(baseUrl)
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState("")
  const [error, setError] = useState("")
  const undo = async (change: WaitingChange) => {
    const s = change.setting
    if (!s) return
    setBusy(change.id)
    setError("")
    try {
      if (undoIsReset(s)) await resetConfigSetting(baseUrl, s.key)
      else await setConfigSetting(baseUrl, s.key, s.started_value)
      await queryClient.invalidateQueries({ queryKey: queryKeys.configSettings(baseUrl) })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy("")
    }
  }
  return <SettingsCard id="restart" icon={Power} title="Restart Legato"
    description="Every Legato process of this server shuts down and starts again. Agents keep running, and this page reconnects.">
    {changes.length === 0
      ? <p className="mb-3 text-sm text-lw-text-2">Nothing waits for a restart.</p>
      : <ul aria-label="Changes waiting for a restart" className="mb-3 divide-y divide-lw-line overflow-hidden rounded-lg border border-lw-line">
        {changes.map((change) => <li key={change.id} className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm">
          <span className="min-w-0 flex-1">{change.name}</span>
          {change.setting
            ? <>
              <span className="font-lw-mono text-xs text-lw-text-2">{formatValue(change.setting, change.setting.started_value)} → {formatValue(change.setting, change.setting.value)}</span>
              <button type="button" className={quietButton + " min-h-8 px-2.5 text-xs"} disabled={busy !== ""} onClick={() => void undo(change)} aria-label={`Undo ${change.name}`}>Undo</button>
            </>
            : <Link to={`#${change.id.split(":")[0]}`} className="text-xs text-lw-link hover:underline">Reset it on its row</Link>}
        </li>)}
      </ul>}
    {error && <p role="alert" className="mb-2 text-xs text-lw-error">{error}</p>}
    <div className="flex flex-wrap items-center gap-2">
      {restart.state === "idle" && <button className={quietButton} onClick={restart.ask}>Restart Legato</button>}
      {restart.state === "confirm" && <>
        <span className="text-sm text-lw-text">Restart now?</span>
        <button className={quietButton} onClick={() => void restart.restart()}>Restart</button>
        <button className={quietButton} onClick={restart.cancel}>Cancel</button>
      </>}
      {restart.state === "restarting" && <span role="status" className="text-sm text-lw-text-2">Restarting…</span>}
    </div>
    {restart.error && <p role="alert" className="mt-2 text-xs text-lw-error">{restart.error}</p>}
  </SettingsCard>
}
