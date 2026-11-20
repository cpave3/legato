import { DirectorySettings } from "../components/settings/DirectorySettings"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { useSettingsCatalog, useSettingsLists } from "../components/settings/useSettingsCatalog"
import { ReeveAutoSettings } from "../components/settings/ReeveAutoSettings"
import { SkillsSettings } from "../components/settings/SkillsSettings"
import { MCPSettings } from "../components/settings/MCPSettings"
import { useEffect, useRef, useState } from "react"
import { ShieldCheck, Download, Zap, ScanSearch, Keyboard, QrCode, Server, Trash2, Plus, RefreshCw, Smartphone, Sparkles, FolderGit2, Palette, SquareTerminal } from "lucide-react"
import { TintPicker } from "../components/TintPicker"
import { AppThemePicker } from "../components/settings/AppThemePicker"
import { ServerThemeSettings } from "../components/settings/ServerThemeSettings"
import { TerminalThemePicker } from "../components/settings/TerminalThemePicker"
import { setToken } from "../lib/auth"
import { useServer } from "../hooks/useServer"
import { useServerVersion } from "../hooks/useServerTint"
import { QRScanner } from "../components/QRScanner"
import { PairDeviceDialog } from "../components/PairDeviceDialog"
import { ResourcesSettings } from "../components/ResourcesSettings"
import { RepositoriesSettings } from "../components/RepositoriesSettings"
import { RepositoryGuidanceSettings } from "../components/RepositoryGuidanceSettings"
import { EngramBindingsSettings } from "../components/EngramBindingsSettings"
import { CredentialsSettings } from "../components/CredentialsSettings"
import { DevicesSettings } from "../components/DevicesSettings"
import { IntegrationsSettings } from "../components/IntegrationsSettings"
import { MQTTSettings } from "../components/settings/MQTTSettings"
import { MainInstancesSettings, WorkersSettings } from "../components/InstancePairingSettings"
import { ApprovalNotificationSettings } from "../components/ApprovalNotificationSettings"
import { UpdatesSettings } from "../components/UpdatesSettings"
import { RestartLegato } from "../components/RestartLegato"
import { SettingsCard, SettingsGroup, SettingsRow, SettingsRows, Toggle, BrowserTag, primaryButton, quietButton } from "../components/settings/SettingsUI"
import { useSettings } from "../hooks/useSettings"
import { CategorySettings, SettingsLoadState, SettingsSearch } from "../components/settings/SettingsCatalog"
import { SettingSource } from "../components/settings/RegistrySettings"
import { MoveSettings } from "../components/settings/MoveSettings"
import { ExportSettings } from "../components/settings/ExportSettings"
import { ReevePairings, ReeveSessions } from "../components/settings/ReeveSettings"
import { ReeveEngramSettings } from "../components/settings/ReeveEngramSettings"
import { useToast } from "../hooks/useToast"
import { apiFetch } from "../lib/api"
import { Link, useLocation, useSearchParams } from "react-router-dom"
import { animatedAgentActivityEnabled, setAnimatedAgentActivityEnabled } from "../lib/appearancePreferences"

import { settingsCategories, settingsDestination } from "../lib/settingsCatalog"

export function SettingsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const { baseUrl, servers, addServer, removeServer, setActiveServer } = useServer()
  const version = useServerVersion(baseUrl)
  const settings = useSettings()
  const configSettings = useSettingsCatalog(baseUrl)
  const configLists = useSettingsLists(baseUrl)
  const registry = configSettings.data?.settings ?? []
  const fileOnly = configSettings.data?.file_only ?? []
  const registered = (key: string) => registry.find((s) => s.key === key)
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

  const scroller = useRef<HTMLDivElement>(null)
  const header = useRef<HTMLElement>(null)
  const { hash, key: destinationRevision } = useLocation()
  let target = hash.slice(1)
  try { target = decodeURIComponent(target) } catch { /* An invalid fragment stays visible in the unavailable-target notice. */ }
  const wide = useMediaQuery("(min-width: 1024px)")
  const current = settingsDestination(target) ?? (!target && wide ? "preferences" : "")
  const [search, setSearch] = useState("")
  const [targetFound, setTargetFound] = useState(true)

  useEffect(() => {
    if (!target) return
    let focused = false
    const resolve = () => {
      const alias = ["handoff", "connection", "lists"].includes(target) ? current : target
      const element = document.getElementById(alias)
      setTargetFound(Boolean(element))
      if (element && !focused) {
        element.tabIndex = -1
        element.style.scrollMarginTop = `${(header.current?.getBoundingClientRect().height ?? 0) + 16}px`
        element.focus({ preventScroll: true })
        element.scrollIntoView?.({ block: "start" })
        focused = true
      }
    }
    resolve()
    const observer = new MutationObserver(resolve)
    if (scroller.current) observer.observe(scroller.current, { childList: true, subtree: true })
    return () => observer.disconnect()
  }, [target, current, baseUrl, destinationRevision])

  const isActive = (url: string) => (url ? baseUrl === url : !baseUrl)

  return (
    <div ref={scroller} className="flex-1 overflow-y-auto bg-lw-ground font-lw text-lw-text">
      <header ref={header} className="sticky top-0 z-10 border-b border-lw-line bg-lw-ground px-4 py-4 md:px-8">
        <div className="mx-auto max-w-5xl">
          <h1 className="text-xl font-semibold">Settings</h1>
          <p className="mt-1 break-all text-sm text-lw-text-2">Server: {baseUrl || window.location.origin}</p>
          <input type="search" aria-label="Search settings" placeholder="Search settings" value={search} onChange={(event) => setSearch(event.target.value)} className="mt-3 min-h-11 w-full rounded-lg border border-lw-line-strong bg-lw-raised px-3 text-sm" />
        </div>
      </header>
      <div className="mx-auto grid w-full max-w-6xl gap-8 px-4 py-6 md:px-8 lg:grid-cols-[220px_minmax(0,1fr)]">
        <aside className={current ? "hidden lg:block" : ""}>
          <nav aria-label="Settings sections" className="flex flex-col gap-1">
            {settingsCategories.map((group) => <Link key={group.id} to={`#${group.id}`} onClick={() => setSearch("")} aria-current={current === group.id ? "page" : undefined} className={`flex min-h-11 items-center rounded-lg px-3 text-sm ${current === group.id ? "bg-lw-selected text-lw-text" : "text-lw-text-2 hover:bg-lw-surface"}`}>{group.title}</Link>)}
          </nav>
          {version && <p data-testid="settings-version" className="mt-4 px-3 font-lw-mono text-xs text-lw-faint">Legato {version}</p>}
        </aside>
        <main key={baseUrl} className="flex min-w-0 flex-col gap-6">
          <SettingsLoadState loading={configSettings.isPending} error={configSettings.error} stale={Boolean(configSettings.data)} onRetry={() => void configSettings.refetch()} />
          {configLists.isPending && <p role="status" className="text-sm text-lw-text-2">Loading configuration lists…</p>}
          {configLists.error && <div><p role="alert" className="text-sm text-lw-error">Could not load configuration lists: {configLists.error.message}{configLists.data ? " Showing last loaded entries." : ""}</p><button className={quietButton + " mt-2"} onClick={() => void configLists.refetch()}>Retry lists</button></div>}
          {target && !targetFound && !configSettings.isPending && !configLists.isPending && <p role="status" className="text-sm text-lw-text-2">This settings destination is unavailable on this server: {target}. Choose a category or search for another setting.</p>}
          {search.trim() && <SettingsSearch query={search} settings={registry} fileOnly={fileOnly} lists={configLists.data ?? []} development={Boolean(settings.data?.hot_reload_enabled)} onSelect={() => setSearch("")} />}
          {current && <Link to="/settings" className={quietButton + " w-fit lg:hidden"}>Back to Settings</Link>}
          {current === "agents" && <SettingsGroup id="agents" title="Agents and workspaces">
            {/* The spawn dialog and the workflow form read the repositories from the settings. */}
            <div id="list-repositories"><div id="list-workflows.repositories"><RepositoriesSettings baseUrl={baseUrl} onChanged={() => void settings.refetch()} /></div></div>
            <DirectorySettings baseUrl={baseUrl} />
            <div id="list-agents.guidance"><RepositoryGuidanceSettings baseUrl={baseUrl} /></div>
            <SettingsCard id="job-clones" icon={FolderGit2} title="Managed clones" description="View and remove clones Legato made for agents on the Remote page.">
              <Link to="/remote" className={quietButton}>Open Remote</Link>
            </SettingsCard>
            <SettingsCard id="list-macros" icon={Zap} title="Macros">
              <Link to="/macros" className={quietButton}>Open Macros</Link>
            </SettingsCard>
            <div id="list-resources"><ResourcesSettings baseUrl={baseUrl} /></div>
            <SkillsSettings baseUrl={baseUrl} />
            <EngramBindingsSettings baseUrl={baseUrl} />

          <CategorySettings category="agents" settings={registry} fileOnly={fileOnly} baseUrl={baseUrl} listKeys={(configLists.data ?? []).map((list) => list.key)} />
          </SettingsGroup>}

          {current === "reeve" && <SettingsGroup id="reeve" title="Reeve" description="The agent of this Legato itself.">
            <div id="reeve-sessions"><ReeveSessions baseUrl={baseUrl} /></div>
            <ReeveAutoSettings baseUrl={baseUrl} />
            <ReeveEngramSettings baseUrl={baseUrl} />
            <ReevePairings baseUrl={baseUrl} />
          <CategorySettings category="reeve" settings={registry} fileOnly={fileOnly} baseUrl={baseUrl} listKeys={(configLists.data ?? []).map((list) => list.key)} />
          </SettingsGroup>}

          {current === "security" && <SettingsGroup id="security" title="Security">

            <DevicesSettings baseUrl={baseUrl} />
            <CredentialsSettings baseUrl={baseUrl} />
            <SettingsCard id="integration-tokens" icon={ShieldCheck} title="Integration tokens" description="Scoped tokens for external services that call this Legato.">
              <p className="mb-3 text-sm text-lw-text-2">Create or revoke tokens on this server with <code>legato integration token create</code> or <code>legato integration token revoke</code>.</p>
              <Link to="#integration-token-overview" className={quietButton}>View token scopes</Link>
            </SettingsCard>
          <CategorySettings category="security" settings={registry} fileOnly={fileOnly} baseUrl={baseUrl} listKeys={(configLists.data ?? []).map((list) => list.key)} />
          </SettingsGroup>}

          {current === "integrations" && <SettingsGroup id="integrations" title="Integrations" description="External services, Home Assistant publishing, and integration tokens.">
            <MQTTSettings settings={registry} baseUrl={baseUrl} loading={false} error={null} onRetry={() => void configSettings.refetch()} />
            <div id="integration-token-overview"><IntegrationsSettings baseUrl={baseUrl} /></div>
            <MCPSettings baseUrl={baseUrl} />
          <CategorySettings category="integrations" settings={registry} fileOnly={fileOnly} baseUrl={baseUrl} listKeys={(configLists.data ?? []).map((list) => list.key)} />
          </SettingsGroup>}



          {current === "connections" && <SettingsGroup id="connections" title="Connections and handoff">
            <WorkersSettings baseUrl={baseUrl} />
            <MainInstancesSettings baseUrl={baseUrl} />

            <SettingsCard id="servers" icon={Server} title="Servers" browser description="The Legato server this browser shows. Switch to another, or add one by its address.">
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
              <SettingsCard id="pair-phone" icon={Smartphone} title="Pair a phone" description="Pair the Legato app with this server by typing four words, or scanning a code.">
                <button onClick={() => setShowPairDevice(true)} className={primaryButton}>
                  <Smartphone size={14} />
                  Pair a device
                </button>
              </SettingsCard>
              <SettingsCard
                id="pair-browser" icon={QrCode}
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

            <SettingsCard id="tls" icon={ShieldCheck} title="TLS certificate" description="Install this server's CA certificate on a device to trust its HTTPS address and install the web app.">
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
          <CategorySettings category="connections" settings={registry} fileOnly={fileOnly} baseUrl={baseUrl} listKeys={(configLists.data ?? []).map((list) => list.key)} />
          </SettingsGroup>}

          {current === "preferences" && <SettingsGroup id="preferences" title="Preferences">
            <div id="appearance" />
            <div id="keyboard">            <SettingsRows>
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
            </SettingsRows></div>
            <div id="browser-notifications"><ApprovalNotificationSettings /></div>
            <SettingsRows>
              <SettingsRow icon={Palette} title="App theme" description="Colours for this device. Terminals follow unless you choose a separate theme." browser={((window as { legatoShell?: { theme?: number } }).legatoShell?.theme ?? 0) < 1} control={<AppThemePicker />} />
              <SettingsRow
                icon={SquareTerminal}
                title="Terminal theme"
                description="Follow the app theme, or choose separate colours for agents' terminals."
                browser
                control={<TerminalThemePicker />}
              />
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
          <CategorySettings category="preferences" settings={registry} fileOnly={fileOnly} baseUrl={baseUrl} listKeys={(configLists.data ?? []).map((list) => list.key)} />
          </SettingsGroup>}

          {current === "server" && <SettingsGroup id="server" title="Server and maintenance">
            <MoveSettings baseUrl={baseUrl} />

            <div id="export"><ExportSettings baseUrl={baseUrl} /></div>
            <SettingsRows><div id="setting-web.tint">              <SettingsRow
                icon={Palette}
                title="Tint"
                description="A colour for this server, along the top of the window and down the rail. Every browser and phone that uses this server shows it."
                control={
                  <div className="flex flex-col items-end gap-1">
                    <TintPicker tint={settings.data?.tint} pinned={registered("web.tint")?.pinned} onChanged={() => { void settings.refetch(); void configSettings.refetch() }} />
                    {registered("web.tint") && <SettingSource setting={registered("web.tint")!} />}
                  </div>
                }
              />
</div></SettingsRows>
            <SettingsCard id="server-theme" icon={Palette} title="Server appearance" description="Server preferences apply to devices that choose Follow server.">
              <ServerThemeSettings />
            </SettingsCard>
            <div id="updates">
            <div id="setting-updates.mode"><UpdatesSettings status={settings.data ? settings.data.updates ?? null : undefined} baseUrl={baseUrl} modeSetting={registered("updates.mode")}
              onChanged={() => { void settings.refetch(); void configSettings.refetch() }} /></div>
            <RestartLegato baseUrl={baseUrl} /></div>

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
            <SettingsCard id="list-board.columns" icon={Server} title="Board columns" description="Column names and ticket status mappings come from board.columns in config.yaml.">
              <p className="mb-3 text-sm text-lw-text-2">Edit the columns in this server’s config.yaml, then run <code>legato columns sync</code> on that server.</p>
              <Link to="/board" className={quietButton}>Open board</Link>
            </SettingsCard>
          <CategorySettings category="server" settings={registry} fileOnly={fileOnly} baseUrl={baseUrl} listKeys={(configLists.data ?? []).map((list) => list.key)} />
          </SettingsGroup>}

          {current && <p className="pb-6 text-xs text-lw-faint">
            Settings marked <BrowserTag /> are kept in this browser. Everything else is on the server, in config.yaml or its database.
          </p>}
        </main>
      </div>

      {showPairDevice && <PairDeviceDialog baseUrl={baseUrl} onClose={() => setShowPairDevice(false)} />}
      {showScanner && (
        <QRScanner onScan={handleQRScan} onClose={() => { setShowScanner(false); setSearchParams({}, { replace: true }) }} />
      )}
    </div>
  )
}
