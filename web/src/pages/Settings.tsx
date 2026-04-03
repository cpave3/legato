import { useEffect, useState } from "react"
import { ShieldCheck, Download, Zap, ScanSearch, Keyboard, QrCode, Server, Trash2, Plus } from "lucide-react"
import { setToken } from "../lib/auth"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"
import { QRScanner } from "../components/QRScanner"

interface SettingsData {
  ca_cert_available: boolean
}

export function SettingsPage() {
  const { baseUrl, servers, addServer, removeServer, setActiveServer } = useServer()
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [glitchEnabled, setGlitchEnabled] = useState(() => {
    return localStorage.getItem("legato:glitch-effect") !== "false"
  })
  const [promptDetectionEnabled, setPromptDetectionEnabled] = useState(() => {
    return localStorage.getItem("legato:prompt-detection") !== "false"
  })
  const [switchModifier, setSwitchModifier] = useState(() => {
    const stored = localStorage.getItem("legato:switch-modifier")
    if (stored) return stored
    return /Mac|iPhone|iPad/.test(navigator.platform) ? "Alt" : "Control"
  })

  useEffect(() => {
    apiFetch(baseUrl, "/api/settings")
      .then((r) => r.json())
      .then(setSettings)
      .catch(() => setSettings(null))
  }, [baseUrl])

  const toggleGlitch = () => {
    const next = !glitchEnabled
    setGlitchEnabled(next)
    localStorage.setItem("legato:glitch-effect", next ? "true" : "false")
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

  const [showScanner, setShowScanner] = useState(false)
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
    setScanSuccess(`Paired with ${data.url}`)
    setTimeout(() => setScanSuccess(""), 3000)
  }

  return (
    <div className="flex-1 overflow-y-auto p-6">
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

      <section className="max-w-lg mb-8">
        <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">
          Pairing
        </h2>
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

      {showScanner && (
        <QRScanner onScan={handleQRScan} onClose={() => setShowScanner(false)} />
      )}

      {servers.length > 0 && (
        <section className="max-w-lg mb-8">
          <h2 className="text-sm font-medium uppercase tracking-wider text-zinc-500 mb-3">
            Servers
          </h2>
          <div className="rounded-lg border border-zinc-800 bg-zinc-900 divide-y divide-zinc-800">
            <button
              onClick={() => setActiveServer("")}
              className={`flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-zinc-800 ${!baseUrl ? "bg-zinc-800/50" : ""}`}
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
                  className="flex flex-1 items-center gap-3 text-left min-w-0"
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
                  className="rounded p-1 text-zinc-600 hover:text-red-400 hover:bg-zinc-800 transition-colors"
                  title="Remove server"
                >
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
          </div>
          <div className="mt-3 flex gap-2">
            <input
              type="text"
              value={newServerName}
              onChange={(e) => setNewServerName(e.target.value)}
              placeholder="Name"
              className="w-28 rounded bg-zinc-800 border border-zinc-700 px-2 py-1.5 text-sm text-zinc-200 placeholder:text-zinc-600 outline-none focus:border-indigo-500"
            />
            <input
              type="text"
              value={newServerUrl}
              onChange={(e) => setNewServerUrl(e.target.value)}
              placeholder="https://hostname:3080"
              className="flex-1 rounded bg-zinc-800 border border-zinc-700 px-2 py-1.5 text-sm text-zinc-200 placeholder:text-zinc-600 outline-none focus:border-indigo-500"
            />
            <button
              onClick={handleAddServer}
              disabled={!newServerName.trim() || !newServerUrl.trim()}
              className="rounded bg-indigo-600 px-3 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
            >
              <Plus size={14} />
            </button>
          </div>
        </section>
      )}

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
              {settings?.ca_cert_available ? (
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
