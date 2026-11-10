import { useState } from "react"
import { FileDown } from "lucide-react"
import { exportSettings, importSettings, type ImportOutcome } from "../../lib/configSettings"
import { SettingsCard, quietButton } from "./SettingsUI"

// saveFile hands the browser a file to save.
function saveFile(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "application/yaml" }))
  const link = document.createElement("a")
  link.href = url
  link.download = name
  link.click()
  URL.revokeObjectURL(url)
}

// ExportSettings downloads the settings set on this server as a file
// shaped like config.yaml, secrets as names.
export function ExportSettings({ baseUrl, save = saveFile }: { baseUrl: string; save?: (name: string, text: string) => void }) {
  const [pinned, setPinned] = useState(false)
  const [error, setError] = useState("")
  const [imported, setImported] = useState<ImportOutcome | null>(null)
  const runImport = async (file: File) => {
    setError("")
    setImported(null)
    try {
      setImported(await importSettings(baseUrl, await file.text()))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }
  const run = async () => {
    setError("")
    try {
      save("legato-settings.yaml", await exportSettings(baseUrl, pinned))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }
  return (
    <SettingsCard icon={FileDown} title="Export and import settings"
      description="Export writes the settings set on this server as a file shaped like config.yaml, secrets as their names. Import turns such a file into one change set for you to approve.">
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm text-lw-text">
          <input type="checkbox" checked={pinned} onChange={() => setPinned(!pinned)} />
          Include settings from config.yaml
        </label>
        <button type="button" className={quietButton} onClick={() => void run()}>Export settings</button>
        <label className={`${quietButton} relative cursor-pointer`}>
          Import settings…
          <input type="file" accept=".yaml,.yml" aria-label="Import settings" className="sr-only"
            onChange={(e) => { const file = e.target.files?.[0]; if (file) void runImport(file) }} />
        </label>
      </div>
      {imported && (
        <p role="status" className="mt-2 text-xs text-lw-text-2">
          {imported.calls} changes wait for your approval in the approval tray.
          {imported.skipped && imported.skipped.length > 0 && <> Left out, since they live only in config.yaml: {imported.skipped.join(", ")}.</>}
        </p>
      )}
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
    </SettingsCard>
  )
}
