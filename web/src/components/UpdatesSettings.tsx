import { useState } from "react"
import { Download } from "lucide-react"
import { SettingsCard, primaryButton, quietButton } from "./settings/SettingsUI"
import { applyToolUpdate, applyUpdate, checkForUpdates, setUpdateMode, type ToolStatus, type UpdateMode, type UpdateStatus } from "../lib/updates"
import { cn } from "../lib/utils"

const modes: { mode: UpdateMode; label: string; description: string }[] = [
  { mode: "auto", label: "Automatic", description: "Installs a new release once no agent is in the middle of a turn, and restarts. Agents keep running." },
  { mode: "ask", label: "Ask", description: "Says a new release is out, and installs it when you ask." },
  { mode: "off", label: "Off", description: "Never looks for one." },
]

const states: Partial<Record<UpdateStatus["state"], string>> = {
  unchecked: "Not checked yet",
  up_to_date: "Up to date",
  available: "A newer release is available",
  waiting: "Waiting for the agents to finish their turns",
  installing: "Installing",
  restarting: "Restarting on the new release",
  installed: "Installed; restart Legato to use it",
  failed: "The last update failed",
}

const toolStates: Record<ToolStatus["state"], string> = {
  missing: "Not installed on this server",
  unmanaged: "Not a release build, so Legato leaves it alone",
  unchecked: "Not checked yet",
  up_to_date: "Up to date",
  available: "A newer release is available",
  installing: "Installing",
  failed: "The last update failed",
}

// UpdatesSettings is how this server takes new releases of Legato.
export function UpdatesSettings({ status, baseUrl, onChanged }: { status: UpdateStatus | null | undefined; baseUrl: string; onChanged?: () => void }) {
  const [busy, setBusy] = useState("")
  const [error, setError] = useState("")

  if (status === undefined) return null
  if (status === null) {
    return <SettingsCard icon={Download} title="Updates" description="This server does not update itself. It is older than the updater, or runs without it." />
  }
  const run = async (what: string, action: () => Promise<unknown>) => {
    setBusy(what)
    setError("")
    try {
      await action()
      onChanged?.()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy("")
    }
  }
  const tools = (status.tools ?? []).filter((tool) => tool.state !== "missing")
  const toolRows = tools.length > 0 && (
    <ToolRows tools={tools} busy={busy} onUpdate={(name) => void run(`tool:${name}`, () => applyToolUpdate(baseUrl, name))} />
  )

  if (!status.release) {
    return (
      <SettingsCard icon={Download} title="Updates"
        description={<>This Legato is built from source (<code>{status.version}</code>), so it does not update itself. Releases from GitHub do.</>}>
        {toolRows}
        {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
      </SettingsCard>
    )
  }

  return (
    <SettingsCard icon={Download} title="Updates" label="Updates"
      description="New releases of Legato from GitHub, stable ones only. The mode is saved on this server.">
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        <dt className="text-lw-text-2">This server</dt>
        <dd data-testid="current-version" className="font-mono text-lw-text">{status.version}</dd>
        <dt className="text-lw-text-2">Latest release</dt>
        <dd className="font-mono text-lw-text">
          {status.latest_url ? <a href={status.latest_url} target="_blank" rel="noreferrer" className="text-lw-link underline">{status.latest}</a> : (status.latest ?? "—")}
        </dd>
        <dt className="text-lw-text-2">State</dt>
        <dd className="text-lw-text">
          {states[status.state] ?? status.state}
          {status.checked_at && <span className="text-lw-faint"> · checked {new Date(status.checked_at).toLocaleString()}</span>}
        </dd>
      </dl>
      {status.error && <p className="mt-2 text-xs text-lw-error">{status.error}</p>}

      <fieldset className="mt-4 flex flex-col gap-2" disabled={busy !== ""}>
        <legend className="mb-1 text-sm text-lw-text">When a new release is out</legend>
        {modes.map((option) => (
          <label key={option.mode} className={cn("flex cursor-pointer gap-3 rounded-lg border px-3 py-2", status.mode === option.mode ? "border-lw-accent" : "border-lw-line")}>
            <input type="radio" name="update-mode" className="mt-1" checked={status.mode === option.mode}
              onChange={() => void run("mode", () => setUpdateMode(baseUrl, option.mode))} />
            <span>
              <span className="block text-sm text-lw-text">{option.label}</span>
              <span className="block text-xs text-lw-text-2">{option.description}</span>
            </span>
          </label>
        ))}
      </fieldset>

      <div className="mt-4 flex flex-wrap gap-2">
        <button className={quietButton} disabled={busy !== ""} onClick={() => void run("check", () => checkForUpdates(baseUrl))}>
          {busy === "check" ? "Checking…" : "Check now"}
        </button>
        {status.available && status.latest && (
          <button className={primaryButton} disabled={busy !== "" || status.state === "installing" || status.state === "restarting"}
            onClick={() => void run("apply", () => applyUpdate(baseUrl))}>
            Update to {status.latest}
          </button>
        )}
      </div>
      {toolRows}
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
    </SettingsCard>
  )
}

// ToolRows are the command-line tools Legato keeps current beside itself,
// under the same mode.
function ToolRows({ tools, busy, onUpdate }: { tools: ToolStatus[]; busy: string; onUpdate: (name: string) => void }) {
  return (
    <section className="mt-5 border-t border-lw-line pt-4" aria-label="Tools kept current">
      <h3 className="text-sm text-lw-text">Tools kept current with Legato</h3>
      {tools.map((tool) => (
        <div key={tool.name} className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
          <span className="font-mono text-lw-text">{tool.name}</span>
          {tool.version && <span className="font-mono text-lw-text-2">{tool.version}</span>}
          <span className="text-xs text-lw-text-2">{toolStates[tool.state] ?? tool.state}</span>
          {tool.managed && tool.available && tool.latest && (
            <button className={primaryButton} disabled={busy !== "" || tool.state === "installing"} onClick={() => onUpdate(tool.name)}>
              Update {tool.name} to {tool.latest}
            </button>
          )}
          {tool.error && tool.state === "failed" && <p className="w-full text-xs text-lw-error">{tool.error}</p>}
        </div>
      ))}
    </section>
  )
}
