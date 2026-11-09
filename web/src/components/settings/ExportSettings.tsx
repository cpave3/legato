import { useState } from "react"
import { FileDown } from "lucide-react"
import { exportSettings } from "../../lib/configSettings"
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
  const run = async () => {
    setError("")
    try {
      save("legato-settings.yaml", await exportSettings(baseUrl, pinned))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }
  return (
    <SettingsCard icon={FileDown} title="Export settings"
      description="The settings set on this server, as a file shaped like config.yaml. Secrets are written as their names, never their values.">
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm text-lw-text">
          <input type="checkbox" checked={pinned} onChange={() => setPinned(!pinned)} />
          Include settings from config.yaml
        </label>
        <button type="button" className={quietButton} onClick={() => void run()}>Export settings</button>
      </div>
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
    </SettingsCard>
  )
}
