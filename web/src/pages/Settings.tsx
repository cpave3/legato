import { useEffect, useRef, useState } from "react"
import { ShieldCheck, Download, Zap, ScanSearch, Keyboard, QrCode, Server, Trash2, Plus, RefreshCw, Smartphone, Sparkles } from "lucide-react"
import { setToken } from "../lib/auth"
import { useServer } from "../hooks/useServer"
import { QRScanner } from "../components/QRScanner"
import { PairDeviceDialog } from "../components/PairDeviceDialog"
import { ResourcesSettings } from "../components/ResourcesSettings"
import { CredentialsSettings } from "../components/CredentialsSettings"
import { DevicesSettings } from "../components/DevicesSettings"
import { SettingsCard, SettingsGroup, SettingsRow, SettingsRows, Toggle, BrowserTag, primaryButton, quietButton } from "../components/settings/SettingsUI"
import { useSettings } from "../hooks/useSettings"
import { useToast } from "../hooks/useToast"
import { apiFetch } from "../lib/api"
import { useSearchParams } from "react-router-dom"
import { animatedAgentActivityEnabled, setAnimatedAgentActivityEnabled } from "../lib/appearancePreferences"

type Group = { id: string; title: string }

export function SettingsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const { baseUrl, servers, addServer, removeServer, setActiveServer } = useServer()
  const settings = useSettings()
  const { addToast } = useToast()
  const [buildStarting, setBuildStarting] = useState(false)
  const [showPairDevice, setShowPairDevice] = useState(false)
  const [glitchEnabled, setGlitchEnabled] = useState(() => localStorage.getItem("legato:glitch-effect") !== "false")
  const [animatedActivityEnabled, setAnimatedActivityEnabled] = useState(animatedAgentActivityEnabled)
  const [promptDetectionEnabled, setPromptDetectionEnabled] = useState(() => localStorage.getItem("legato:prompt-detection") !== "false")
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

  const groups: Group[] = [
    { id: "agents", title: "Agents" },
    { id: "security", title: "Security" },
    { id: "connection", title: "Connection" },
    { id: "appearance", title: "Appearance" },
    ...(settings.data?.hot_reload_enabled ? [{ id: "development", title: "Development" }] : []),
  ]

  const groupIds = groups.map((g) => g.id).join(" ")
  const scroller = useRef<HTMLDivElement>(null)
  const [current, setCurrent] = useState("agents")
  // Mark the group in view in the section list.
  useEffect(() => {
    const root = scroller.current
    if (!root || typeof IntersectionObserver === "undefined") return
    const observer = new IntersectionObserver((entries) => {
      const shown = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0]
      if (shown) setCurrent(shown.target.id)
    }, { root, rootMargin: "0px 0px -65% 0px" })
    for (const id of groupIds.split(" ")) {
      const el = document.getElementById(id)
      if (el) observer.observe(el)
    }
    return () => observer.disconnect()
  }, [groupIds])

  const goTo = (id: string) => {
    document.getElementById(id)?.scrollIntoView?.({ behavior: "smooth", block: "start" })
    window.history.replaceState(window.history.state, "", `#${id}`)
    setCurrent(id)
  }

  const isActive = (url: string) => (url ? baseUrl === url : !baseUrl)

  return (
    <div ref={scroller} className="flex-1 overflow-y-auto bg-lw-ground font-lw text-lw-text">
      {/* On a phone: the groups as a row of chips that stays at the top. */}
      <nav aria-label="Settings sections" className="sticky top-0 z-10 flex gap-2 overflow-x-auto border-b border-lw-line bg-lw-ground/95 px-4 py-2.5 backdrop-blur lg:hidden">
        {groups.map((g) => (
          <button
            key={g.id}
            onClick={() => goTo(g.id)}
            aria-current={current === g.id ? "true" : undefined}
            className={`shrink-0 rounded-full border px-3 py-1.5 text-xs transition-colors ${current === g.id ? "border-lw-accent-ring bg-lw-accent-soft text-lw-accent-text" : "border-lw-line text-lw-text-2"}`}
          >
            {g.title}
          </button>
        ))}
      </nav>

      <div className="mx-auto grid w-full max-w-5xl gap-10 px-4 py-6 md:px-8 md:py-8 lg:grid-cols-[180px_minmax(0,1fr)]">
        <aside className="hidden lg:block">
          <div className="sticky top-8">
            <h1 className="mb-4 text-xl font-semibold">Settings</h1>
            <nav aria-label="Settings sections" className="flex flex-col gap-0.5">
              {groups.map((g) => (
                <button
                  key={g.id}
                  onClick={() => goTo(g.id)}
                  aria-current={current === g.id ? "true" : undefined}
                  className={`rounded-lg px-3 py-2 text-left text-sm transition-colors ${current === g.id ? "bg-lw-selected text-lw-text" : "text-lw-text-2 hover:bg-lw-surface hover:text-lw-text"}`}
                >
                  {g.title}
                </button>
              ))}
            </nav>
          </div>
        </aside>

        <main className="flex min-w-0 max-w-3xl flex-col gap-10">
          <h1 className="text-xl font-semibold lg:hidden">Settings</h1>

          <SettingsGroup id="agents" title="Agents" description="What agents are given, and how this browser works with them.">
            <ResourcesSettings baseUrl={baseUrl} />
            <SettingsRows>
              <SettingsRow
                icon={ScanSearch}
                title="Prompt detection"
                description="Find tool approval and plan prompts in a terminal agent's output and offer them as buttons."
                browser
                control={<Toggle label="Prompt detection" on={promptDetectionEnabled} onChange={togglePromptDetection} />}
              />
              <SettingsRow
                icon={Keyboard}
                title="Agent switch key"
                description="Hold this key and press a number to switch agents."
                browser
                control={(
                  <select
                    aria-label="Agent switch key"
                    value={switchModifier}
                    onChange={(e) => handleSwitchModifier(e.target.value)}
                    className="min-h-9 rounded-lg border border-lw-line-strong bg-lw-raised px-2 text-sm text-lw-text outline-none focus:border-lw-accent"
                  >
                    <option value="Control">Ctrl</option>
                    <option value="Alt">Alt / Option</option>
                    <option value="Meta">Meta / Cmd</option>
                  </select>
                )}
              />
            </SettingsRows>
          </SettingsGroup>

          <SettingsGroup id="security" title="Security" description="Who can allow approvals, and the secrets agents use without seeing them.">
            <DevicesSettings baseUrl={baseUrl} />
            <CredentialsSettings baseUrl={baseUrl} />
          </SettingsGroup>

          <SettingsGroup id="connection" title="Connection" description="The servers this browser talks to, pairing, and trusting this server's certificate.">
            <SettingsCard icon={Server} title="Servers" browser description="The Legato server this browser shows. Switch to another, or add one by its address.">
              <div className="overflow-hidden rounded-lg border border-lw-line divide-y divide-lw-line">
                {[{ name: "Local", url: "" }, ...servers].map((s) => (
                  <div key={s.url || "local"} className={`flex items-center gap-3 px-3 ${isActive(s.url) ? "bg-lw-selected" : ""}`}>
                    <button onClick={() => setActiveServer(s.url)} className="flex min-h-12 min-w-0 flex-1 items-center gap-3 text-left">
                      <span className={`h-2 w-2 shrink-0 rounded-full ${isActive(s.url) ? "bg-lw-live" : "bg-lw-line-strong"}`} />
                      <div className="min-w-0 flex-1">
                        <p className="text-sm text-lw-text">{s.name}</p>
                        <p className="truncate font-lw-mono text-xs text-lw-faint">{s.url || window.location.origin}</p>
                      </div>
                      {isActive(s.url) && <span className="font-lw-mono text-[10px] uppercase tracking-wide text-lw-live">in use</span>}
                    </button>
                    {s.url && (
                      <button
                        onClick={() => removeServer(s.url)}
                        className="flex h-10 w-10 items-center justify-center rounded-lg text-lw-faint transition-colors hover:bg-lw-raised hover:text-lw-danger"
                        title="Remove server"
                        aria-label={`Remove ${s.name}`}
                      >
                        <Trash2 size={14} />
                      </button>
                    )}
                  </div>
                ))}
              </div>
              <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-[8rem_minmax(0,1fr)_auto]">
                <input
                  type="text"
                  value={newServerName}
                  onChange={(e) => setNewServerName(e.target.value)}
                  placeholder="Name"
                  aria-label="Server name"
                  className="min-h-10 rounded-lg border border-lw-line-strong bg-lw-raised px-3 text-sm text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent"
                />
                <input
                  type="text"
                  value={newServerUrl}
                  onChange={(e) => setNewServerUrl(e.target.value)}
                  placeholder="https://hostname:3080"
                  aria-label="Server address"
                  className="min-h-10 rounded-lg border border-lw-line-strong bg-lw-raised px-3 font-lw-mono text-sm text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent"
                />
                <button onClick={handleAddServer} disabled={!newServerName.trim() || !newServerUrl.trim()} className={quietButton}>
                  <Plus size={14} />
                  Add
                </button>
              </div>
            </SettingsCard>

            <div className="grid gap-4 md:grid-cols-2">
              <SettingsCard icon={Smartphone} title="Pair a phone" description="Pair the Legato app with this server by typing four words, or scanning a code.">
                <button onClick={() => setShowPairDevice(true)} className={primaryButton}>
                  <Smartphone size={14} />
                  Pair a device
                </button>
              </SettingsCard>
              <SettingsCard
                icon={QrCode}
                title="Pair this browser"
                description={<>Scan the QR code another server shows with <code className="rounded bg-lw-raised px-1 font-lw-mono text-lw-text-2">legato pair</code>, and add it to this browser's servers.</>}
              >
                <button onClick={() => setShowScanner(true)} className={quietButton}>
                  <QrCode size={14} />
                  Scan QR code
                </button>
                {scanSuccess && <p role="status" className="mt-2 text-sm text-lw-live">{scanSuccess}</p>}
              </SettingsCard>
            </div>

            <SettingsCard icon={ShieldCheck} title="TLS certificate" description="Install this server's CA certificate on a device to trust its HTTPS address and install the web app.">
              <ul className="mb-3 flex flex-col gap-1 text-xs text-lw-text-2">
                <li><span className="text-lw-text">Android:</span> Settings → Security → Install a certificate → CA certificate.</li>
                <li><span className="text-lw-text">iOS:</span> install the profile, then turn on full trust in Settings → General → About → Certificate Trust Settings.</li>
              </ul>
              {settings.data?.ca_cert_available ? (
                <a href={baseUrl ? `${baseUrl}/api/ca-cert` : "/api/ca-cert"} download="legato-ca.pem" className={quietButton + " w-fit"}>
                  <Download size={14} />
                  Download CA certificate
                </a>
              ) : (
                <p className="text-xs text-lw-faint">This server uses a certificate of its own (web.tls.cert), so there is no CA to download.</p>
              )}
            </SettingsCard>
          </SettingsGroup>

          <SettingsGroup id="appearance" title="Appearance" description="Motion and effects. These are saved in this browser only.">
            <SettingsRows>
              <SettingsRow
                icon={Zap}
                title="Glitch effect"
                description="A short glitch animation when a terminal syncs or you switch agents."
                browser
                control={<Toggle label="Glitch effect" on={glitchEnabled} onChange={toggleGlitch} />}
              />
              <SettingsRow
                icon={Sparkles}
                title="Animated agent activity"
                description="A light that runs around a card while its agent works."
                browser
                control={<Toggle label="Animated agent activity" on={animatedActivityEnabled} onChange={toggleAnimatedActivity} />}
              />
            </SettingsRows>
          </SettingsGroup>

          {settings.data?.hot_reload_enabled && (
            <SettingsGroup id="development" title="Development" description="This Legato runs from a checkout under hot reload.">
              <SettingsCard icon={RefreshCw} title="Rebuild web application" description="Build and embed the latest web UI, then hot reload Legato. This connection drops for a moment.">
                <button onClick={handleFullBuild} disabled={buildStarting} className={primaryButton + " disabled:cursor-wait"}>
                  <RefreshCw size={14} className={buildStarting ? "animate-spin" : ""} />
                  {buildStarting ? "Starting rebuild…" : "Rebuild and hot reload"}
                </button>
              </SettingsCard>
            </SettingsGroup>
          )}

          <p className="pb-6 text-xs text-lw-faint">
            Settings marked <BrowserTag /> are kept in this browser. Everything else is on the server, in config.yaml or its database.
          </p>
        </main>
      </div>

      {showPairDevice && <PairDeviceDialog baseUrl={baseUrl} onClose={() => setShowPairDevice(false)} />}
      {showScanner && (
        <QRScanner onScan={handleQRScan} onClose={() => { setShowScanner(false); setSearchParams({}, { replace: true }) }} />
      )}
    </div>
  )
}
