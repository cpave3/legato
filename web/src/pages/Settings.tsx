import { useState } from "react"
import { ShieldCheck, Download, Zap, ScanSearch, Keyboard, QrCode, Server, Trash2, Plus, RefreshCw, Smartphone } from "lucide-react"
import { setToken } from "../lib/auth"
import { useServer } from "../hooks/useServer"
import { QRScanner } from "../components/QRScanner"
import { PairDeviceDialog } from "../components/PairDeviceDialog"
import { ResourcesSettings } from "../components/ResourcesSettings"
import { CredentialsSettings } from "../components/CredentialsSettings"
import { DevicesSettings } from "../components/DevicesSettings"
import { useSettings } from "../hooks/useSettings"
import { useToast } from "../hooks/useToast"
import { apiFetch } from "../lib/api"
import { useSearchParams } from "react-router-dom"
import { animatedAgentActivityEnabled, setAnimatedAgentActivityEnabled } from "../lib/appearancePreferences"

export function SettingsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const { baseUrl, servers, addServer, removeServer, setActiveServer } = useServer()
  const settings = useSettings()
  const { addToast } = useToast()
  const [buildStarting, setBuildStarting] = useState(false)
  const [showPairDevice, setShowPairDevice] = useState(false)
  const [glitchEnabled, setGlitchEnabled] = useState(() => {
    return localStorage.getItem("legato:glitch-effect") !== "false"
  })
  const [animatedActivityEnabled, setAnimatedActivityEnabled] = useState(animatedAgentActivityEnabled)
  const [promptDetectionEnabled, setPromptDetectionEnabled] = useState(() => {
    return localStorage.getItem("legato:prompt-detection") !== "false"
  })
  const [switchModifier, setSwitchModifier] = useState(() => {
    const stored = localStorage.getItem("legato:switch-modifier")
    if (stored) return stored
    return /Mac|iPhone|iPad/.test(navigator.platform) ? "Alt" : "Control"
  })


  const toggleGlitch = () => {
    const next = !glitchEnabled
    setGlitchEnabled(next)
    localStorage.setItem("legato:glitch-effect", next ? "true" : "false")
  }

  const toggleAnimatedActivity = () => {
    const next = !animatedActivityEnabled
    setAnimatedActivityEnabled(next)
    setAnimatedAgentActivityEnabled(next)
  }

  const togglePromptDetection = () => {
    const next = !promptDetectionEnabled
    setPromptDetectionEnabled(next)
    localStorage.setItem("legato:prompt-detection", next ? "true" : "false")
  }

  const handleSwitchModifier = (value: string) => {
    setSwitchModifier(value)
    localStorage.setItem("legato:switch-modifier", value)
  }

  const handleFullBuild = async () => {
    if (buildStarting) return
    setBuildStarting(true)
    sessionStorage.setItem("legato:post-rebuild-redirect", "/agents")
    try {
      const response = await apiFetch(baseUrl, "/api/hot-reload/build", { method: "POST" })
      if (!response.ok) {
        throw new Error(await response.text().catch(() => "Failed to start full rebuild"))
      }
      addToast("Full rebuild started", "success")
      setBuildStarting(false)
    } catch (error) {
      addToast(error instanceof Error ? error.message : "Failed to start full rebuild", "error")
      setBuildStarting(false)
    }
  }

  const [showScanner, setShowScanner] = useState(() => searchParams.get("pair") === "qr")
  const [scanSuccess, setScanSuccess] = useState("")

  const [newServerName, setNewServerName] = useState("")
  const [newServerUrl, setNewServerUrl] = useState("")

  const handleAddServer = () => {
    const name = newServerName.trim()
    const url = newServerUrl.trim().replace(/\/$/, "")
    if (!name || !url) return
    addServer(name, url)
    setNewServerName("")
    setNewServerUrl("")
  }

  const handleQRScan = (data: { url: string; token: string }) => {
    // Validate URL before storing anything.
    let hostname: string
    try {
      const parsed = new URL(data.url)
      hostname = parsed.hostname
    } catch {
      setScanSuccess("")
      window.alert("Invalid server URL in QR code: " + data.url)
      return
    }

    // Store token for the scanned server.
    const isOrigin = new URL(data.url).host === window.location.host
    setToken(data.token, data.url)
    if (isOrigin) {
      setToken(data.token)
    }
    // Add to server registry if it's a remote server.
    if (!isOrigin) {
      addServer(hostname, data.url)
    }
    setShowScanner(false)
    setSearchParams({}, { replace: true })
    setScanSuccess(`Paired with ${data.url}`)
    setTimeout(() => setScanSuccess(""), 3000)
  }

  return (
    <div className="flex-1 overflow-y-auto p-4 md:p-6">
      <h1 className="text-lg font-semibold text-zinc-100 mb-6">Settings</h1>

      <section className="max-w-lg mb-8">
        <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">
          Appearance
        </h2>
        <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Zap size={20} className="text-indigo-400 shrink-0" />
              <div>
                <p className="text-sm text-zinc-300">Glitch effect</p>
                <p className="text-xs text-zinc-500">
                  Sci-fi glitch animation on terminal sync events
                </p>
              </div>
            </div>
            <button
              onClick={toggleGlitch}
              className={`relative h-6 w-11 rounded-full transition-colors ${glitchEnabled ? "bg-indigo-600" : "bg-zinc-700"}`}
            >
              <span
                className={`absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white transition-transform ${glitchEnabled ? "translate-x-5" : "translate-x-0"}`}
              />
            </button>
          </div>
        </div>
        <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4 mt-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Zap size={20} className="text-purple-400 shrink-0" />
              <div>
                <p className="text-sm text-zinc-300">Animated agent activity</p>
                <p className="text-xs text-zinc-500">
                  Moving card highlight that responds to agent activity
                </p>
              </div>
            </div>
            <button
              type="button"
              aria-label="Animated agent activity"
              aria-pressed={animatedActivityEnabled}
              onClick={toggleAnimatedActivity}
              className={`relative h-6 w-11 rounded-full transition-colors ${animatedActivityEnabled ? "bg-indigo-600" : "bg-zinc-700"}`}
            >
              <span
                className={`absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white transition-transform ${animatedActivityEnabled ? "translate-x-5" : "translate-x-0"}`}
              />
            </button>
          </div>
        </div>
        <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4 mt-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <ScanSearch size={20} className="text-indigo-400 shrink-0" />
              <div>
                <p className="text-sm text-zinc-300">Prompt detection</p>
                <p className="text-xs text-zinc-500">
                  Auto-detect tool approval and plan prompts in agent output
                </p>
              </div>
            </div>
            <button
              onClick={togglePromptDetection}
              className={`relative h-6 w-11 rounded-full transition-colors ${promptDetectionEnabled ? "bg-indigo-600" : "bg-zinc-700"}`}
            >
              <span
                className={`absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white transition-transform ${promptDetectionEnabled ? "translate-x-5" : "translate-x-0"}`}
              />
            </button>
          </div>
        </div>
        <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4 mt-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Keyboard size={20} className="text-indigo-400 shrink-0" />
              <div>
                <p className="text-sm text-zinc-300">Agent switch key</p>
                <p className="text-xs text-zinc-500">
                  Hold this key + number to switch agents
                </p>
              </div>
            </div>
            <select
              value={switchModifier}
              onChange={(e) => handleSwitchModifier(e.target.value)}
              className="rounded bg-zinc-800 border border-zinc-700 px-2 py-1 text-sm text-zinc-200 outline-none"
            >
              <option value="Control">Ctrl</option>
              <option value="Alt">Alt / Option</option>
              <option value="Meta">Meta / Cmd</option>
            </select>
          </div>
        </div>
      </section>

      {settings.data?.hot_reload_enabled && (
        <section className="max-w-lg mb-8">
          <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">
            Development
          </h2>
          <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
            <div className="flex items-start gap-3">
              <RefreshCw size={20} className="text-indigo-400 mt-0.5 shrink-0" />
              <div className="flex flex-1 flex-col gap-2">
                <p className="text-sm text-zinc-300">Rebuild web application</p>
                <p className="text-xs text-zinc-500">
                  Build and embed the latest web UI, then hot reload Legato. This connection will briefly disconnect and reconnect.
                </p>
                <button
                  onClick={handleFullBuild}
                  disabled={buildStarting}
                  className="mt-1 inline-flex min-h-11 w-full items-center justify-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-900 disabled:cursor-wait disabled:opacity-60 sm:w-fit"
                >
                  <RefreshCw size={14} className={buildStarting ? "animate-spin" : ""} />
                  {buildStarting ? "Starting rebuild…" : "Rebuild and hot reload"}
                </button>
              </div>
            </div>
          </div>
        </section>
      )}

      <ResourcesSettings baseUrl={baseUrl} />
      <CredentialsSettings baseUrl={baseUrl} />
      <DevicesSettings baseUrl={baseUrl} />

      <section className="max-w-lg mb-8">
        <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">
          Pairing
        </h2>
        <div className="mb-3 rounded-lg border border-zinc-800 bg-zinc-900 p-4">
          <div className="flex items-start gap-3">
            <Smartphone size={20} className="text-indigo-400 mt-0.5 shrink-0" />
            <div className="flex flex-col gap-2">
              <p className="text-sm text-zinc-300">
                Pair a phone with this server by typing four words in the Legato app.
              </p>
              <button
                onClick={() => setShowPairDevice(true)}
                className="mt-1 inline-flex w-fit items-center gap-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500"
              >
                <Smartphone size={14} />
                Pair a device
              </button>
            </div>
          </div>
        </div>
        <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
          <div className="flex items-start gap-3">
            <QrCode size={20} className="text-indigo-400 mt-0.5 shrink-0" />
            <div className="flex flex-col gap-2">
              <p className="text-sm text-zinc-300">
                Scan a QR code from another Legato instance to pair with it.
              </p>
              <p className="text-xs text-zinc-500">
                Run <code className="text-zinc-400 bg-zinc-800 px-1 rounded">legato pair</code> on the server to display the QR code.
              </p>
              <button
                onClick={() => setShowScanner(true)}
                className="mt-1 inline-flex w-fit items-center gap-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500"
              >
                <QrCode size={14} />
                Scan QR Code
              </button>
              {scanSuccess && (
                <p className="text-sm text-emerald-400">{scanSuccess}</p>
              )}
            </div>
          </div>
        </div>
      </section>

      {showPairDevice && <PairDeviceDialog baseUrl={baseUrl} onClose={() => setShowPairDevice(false)} />}

      {showScanner && (
        <QRScanner onScan={handleQRScan} onClose={() => { setShowScanner(false); setSearchParams({}, { replace: true }) }} />
      )}

      <section className="max-w-lg mb-8">
          <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">
            Servers
          </h2>
          <div className="rounded-lg border border-zinc-800 bg-zinc-900 divide-y divide-zinc-800">
            <button
              onClick={() => setActiveServer("")}
              className={`flex min-h-11 w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-zinc-800 ${!baseUrl ? "bg-zinc-800/50" : ""}`}
            >
              <Server size={16} className={!baseUrl ? "text-indigo-400" : "text-zinc-600"} />
              <div className="flex-1 min-w-0">
                <p className="text-sm text-zinc-200">Local</p>
                <p className="text-xs text-zinc-500 truncate">{window.location.origin}</p>
              </div>
              {!baseUrl && <span className="text-[10px] text-indigo-400 font-medium">ACTIVE</span>}
            </button>
            {servers.map((s) => (
              <div key={s.url} className={`flex items-center gap-3 px-4 py-3 transition-colors hover:bg-zinc-800 ${baseUrl === s.url ? "bg-zinc-800/50" : ""}`}>
                <button
                  onClick={() => setActiveServer(s.url)}
                  className="flex min-h-11 flex-1 items-center gap-3 text-left min-w-0"
                >
                  <Server size={16} className={baseUrl === s.url ? "text-indigo-400" : "text-zinc-600"} />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm text-zinc-200">{s.name}</p>
                    <p className="text-xs text-zinc-500 truncate">{s.url}</p>
                  </div>
                  {baseUrl === s.url && <span className="text-[10px] text-indigo-400 font-medium">ACTIVE</span>}
                </button>
                <button
                  onClick={() => removeServer(s.url)}
                  className="flex h-11 w-11 items-center justify-center rounded text-zinc-600 hover:text-red-400 hover:bg-zinc-800 transition-colors"
                  title="Remove server"
                >
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
          </div>
          <div className="mt-3 grid grid-cols-1 gap-2 sm:flex">
            <input
              type="text"
              value={newServerName}
              onChange={(e) => setNewServerName(e.target.value)}
              placeholder="Name"
              className="min-h-11 w-full rounded bg-zinc-800 border border-zinc-700 px-3 text-sm text-zinc-200 placeholder:text-zinc-600 outline-none focus:border-indigo-500 sm:w-28"
            />
            <input
              type="text"
              value={newServerUrl}
              onChange={(e) => setNewServerUrl(e.target.value)}
              placeholder="https://hostname:3080"
              className="min-h-11 flex-1 rounded bg-zinc-800 border border-zinc-700 px-3 text-sm text-zinc-200 placeholder:text-zinc-600 outline-none focus:border-indigo-500"
            />
            <button
              onClick={handleAddServer}
              disabled={!newServerName.trim() || !newServerUrl.trim()}
              className="flex min-h-11 items-center justify-center rounded bg-indigo-600 px-3 text-sm text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
            >
              <Plus size={14} />
            </button>
          </div>
        </section>

      <section className="max-w-lg">
        <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">
          TLS Certificate
        </h2>
        <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4">
          <div className="flex items-start gap-3">
            <ShieldCheck size={20} className="text-indigo-400 mt-0.5 shrink-0" />
            <div className="flex flex-col gap-2">
              <p className="text-sm text-zinc-300">
                Install the CA certificate on your device to trust this server
                and enable PWA installation.
              </p>
              <p className="text-xs text-zinc-500">
                On iOS: install the profile, then enable full trust in Settings
                &rarr; General &rarr; About &rarr; Certificate Trust Settings.
              </p>
              <p className="text-xs text-zinc-500">
                On Android: Settings &rarr; Security &rarr; Install a
                certificate &rarr; CA certificate.
              </p>
              {settings.data?.ca_cert_available ? (
                <a
                  href={baseUrl ? `${baseUrl}/api/ca-cert` : "/api/ca-cert"}
                  download="legato-ca.pem"
                  className="mt-2 inline-flex w-fit items-center gap-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-indigo-500"
                >
                  <Download size={14} />
                  Download CA Certificate
                </a>
              ) : (
                <p className="mt-1 text-xs text-zinc-600">
                  No auto-generated CA certificate available. Using custom TLS
                  config.
                </p>
              )}
            </div>
          </div>
        </div>
      </section>
    </div>
  )
}
