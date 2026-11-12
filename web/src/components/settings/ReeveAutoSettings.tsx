import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Zap } from "lucide-react"
import { fetchReeveAuto, turnReeveAutoOff, type AutoRun } from "../../lib/reeve"
import { queryKeys } from "../../lib/query"
import { SettingsCard, quietButton } from "./SettingsUI"

const stateText: Record<string, string> = { queued: "running", running: "running", done: "done", failed: "failed", restarting: "restarting" }

function AutoRunRow({ run }: { run: AutoRun }) {
  return (
    <li className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 py-1.5 text-sm">
      <span className="min-w-0 text-lw-text">{run.reason}</span>
      <span className="text-xs text-lw-text-2">{stateText[run.state] ?? run.state}</span>
      <span className="text-xs text-lw-faint">{run.by.replace(/^auto: /, "")}</span>
      <span className="ml-auto text-xs text-lw-faint">{new Date(run.at).toLocaleString()}</span>
    </li>
  )
}

// ReeveAutoSettings says what the Reeve runs without asking, turns all of
// it off at once (no signature: it only makes the Reeve ask more), and
// lists the sets that ran without the person.
export function ReeveAutoSettings({ baseUrl }: { baseUrl: string }) {
  const queryClient = useQueryClient()
  const auto = useQuery({ queryKey: queryKeys.reeveAuto(baseUrl), queryFn: () => fetchReeveAuto(baseUrl), retry: false })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const data = auto.data
  if (!data) return null
  const turnOff = async () => {
    setBusy(true)
    setError("")
    try {
      await turnReeveAutoOff(baseUrl)
      await queryClient.invalidateQueries({ queryKey: queryKeys.reeveAuto(baseUrl) })
      await queryClient.invalidateQueries({ queryKey: queryKeys.configSettings(baseUrl) })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  const items: { key: string; text: string; warning?: string }[] = []
  if (data.change === "auto") items.push({ key: "change", text: "Changes (tasks, agents, their messages, ordinary settings)" })
  if (data.risky === "auto") items.push({ key: "risky", text: "Risky changes", warning: "Includes restarts, stopping agents and settings that choose what runs on this machine." })
  if (data.host_auto) items.push({ key: "host", text: "Host operations not as root", warning: "The Reeve can run any command as your user on this machine, and write any file you can." })
  return (
    <SettingsCard id="reeve-auto" icon={Zap} title="Runs without asking"
      description="Change sets at the auto level run at once. Deletions, the Reeve's own rules, security settings, pairing, secrets, config.yaml edits and anything as root still wait for you.">
      {data.on ? (
        <div className="flex flex-col gap-2">
          <ul className="flex flex-col gap-1.5 text-sm">
            {items.map((item) => (
              <li key={item.key}>
                <span className="text-lw-text">{item.text}</span>
                {item.warning && <p className="text-xs text-lw-needs">{item.warning}</p>}
              </li>
            ))}
            {data.rules.map((pattern) => (
              <li key={pattern} className="text-sm text-lw-text-2">Rule <code className="text-lw-text">{pattern}</code></li>
            ))}
          </ul>
          {data.pinned.map((key) => (
            <p key={key} className="text-xs text-lw-needs">config.yaml sets {key}: edit the file to change it.</p>
          ))}
          <div>
            <button type="button" className={quietButton} disabled={busy} onClick={() => void turnOff()}>Ask for everything</button>
          </div>
        </div>
      ) : (
        <p className="text-sm text-lw-text-2">Every change waits for your approval.</p>
      )}
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
      {data.recent.length > 0 && (
        <section className="mt-4 border-t border-lw-line pt-3" aria-label="Ran without asking">
          <h3 className="text-sm text-lw-text">Ran without asking</h3>
          <ul className="mt-1 flex flex-col divide-y divide-lw-line">
            {data.recent.map((run) => <AutoRunRow key={run.id} run={run} />)}
          </ul>
        </section>
      )}
    </SettingsCard>
  )
}
