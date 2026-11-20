import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom"
import { ChevronLeft, ChevronRight, ExternalLink } from "lucide-react"
import { DirectorySettings } from "../components/settings/DirectorySettings"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { useSettingsCatalog, useSettingsLists } from "../components/settings/useSettingsCatalog"
import { RailSettings } from "../components/settings/RailSettings"
import { ReeveAutoSettings } from "../components/settings/ReeveAutoSettings"
import { SkillsSettings } from "../components/settings/SkillsSettings"
import { MCPSettings } from "../components/settings/MCPSettings"
import { ServerThemeSettings } from "../components/settings/ServerThemeSettings"
import { setToken } from "../lib/auth"
import { useServer } from "../hooks/useServer"
import { useServerVersion } from "../hooks/useServerTint"
import { useDevices } from "../hooks/useDevices"
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
import { RestartBar, RestartCard } from "../components/settings/RestartSettings"
import { waitingChanges } from "../lib/pendingRestart"
import { PickerContext, pickerData } from "../lib/settingPickers"
import { listSkills } from "../lib/skills"
import { fetchAdapters } from "../lib/adapters"
import { queryKeys } from "../lib/query"
import { useQuery } from "@tanstack/react-query"
import { SettingsCard, quietButton } from "../components/settings/SettingsUI"
import { Globe, Palette } from "lucide-react"
import { useSettings } from "../hooks/useSettings"
import { SettingsLoadState, SettingsSearch } from "../components/settings/SettingsCatalog"
import { search as searchSettings, searchEntries } from "../lib/settingsSearch"
import { MoveSettings } from "../components/settings/MoveSettings"
import { ExportSettings } from "../components/settings/ExportSettings"
import { ReevePairings, ReeveSessions } from "../components/settings/ReeveSettings"
import { ReeveEngramSettings } from "../components/settings/ReeveEngramSettings"
import { SettingsSectionView } from "../components/settings/SettingsSection"
import { ShowKeysContext, storeShowKeys, storedShowKeys } from "../components/settings/showKeys"
import { Toggle } from "../components/settings/SettingsUI"
import { AppAppearanceCard, BoardColumnsCard, DevelopmentCard, KeyboardCard, ManagedClonesCard, PairBrowserCard, PairPhoneCard, ServersCard, TLSCard, TintCard } from "../components/settings/SettingsCards"
import type { ConfigSetting } from "../lib/configSettings"
import { findSection, groupElementId, groupHasContent, leadGroup, navGroups, sectionGroups, settingsDestination, settingsSections, slug, type SettingsSection } from "../lib/settingsCatalog"

// The state a section shows beside its name in the nav.
interface SectionState { label?: string; on?: boolean; dot?: string; count?: number }

// Features with a switch of their own show On or Off.
const featureSwitches: Record<string, string> = {
  sandbox: "agents.sandbox.enabled",
  swarms: "swarm.enabled",
  "review-passes": "review_passes.enabled",
  learn: "learning.enabled",
}

function sectionStates(registry: ConfigSetting[], noDevice: boolean, waiting: number): Record<string, SectionState> {
  const states: Record<string, SectionState> = {}
  for (const [section, key] of Object.entries(featureSwitches)) {
    const setting = registry.find((s) => s.key === key)
    if (setting) states[section] = { on: setting.value === true, label: setting.value === true ? "On" : "Off" }
  }
  if (noDevice) states.security = { dot: "No approval device" }
  if (waiting > 0) states.system = { count: waiting, label: `${waiting} waiting` }
  return states
}

function StateMark({ state, phone }: { state?: SectionState; phone?: boolean }) {
  if (!state) return null
  if (state.count) return <span className="ml-auto inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-full bg-lw-needs px-1.5 font-lw-mono text-[11px] text-lw-ground" title={`${state.count} changes wait for a restart`} aria-label={`${state.count} changes wait for a restart`}>{state.count}</span>
  if (state.dot) return phone
    ? <span className="ml-auto text-xs text-lw-needs">{state.dot}</span>
    : <span className="ml-auto h-[7px] w-[7px] rounded-full bg-lw-needs" title={state.dot} aria-label={state.dot} role="img" />
  return <span className={`ml-auto text-xs ${state.on ? "text-lw-live" : "text-lw-text-2"}`}>{state.label}</span>
}

function SectionLink({ section, current, state, phone, onClick, count }: { section: SettingsSection; current: boolean; state?: SectionState; phone?: boolean; onClick: () => void; count?: number }) {
  const className = phone
    ? "flex min-h-12 items-center gap-2 px-4 text-sm text-lw-text"
    : `flex min-h-9 items-center gap-2 rounded-lg px-3 text-sm ${current ? "bg-lw-selected font-medium text-lw-text" : "text-lw-text-2 hover:bg-lw-surface hover:text-lw-text"}`
  const dim = count === 0 ? " opacity-45" : ""
  if (section.href) return <Link to={section.href} className={className + dim}>{section.title}<ExternalLink size={12} aria-hidden className="ml-auto text-lw-faint" /></Link>
  return <Link to={`#${section.id}`} onClick={onClick} aria-current={current ? "page" : undefined} className={className + dim}>
    {section.title}
    {count !== undefined
      ? <span className="ml-auto font-lw-mono text-[11px] text-lw-text-2" aria-label={`${count} matches`}>{count}</span>
      : <StateMark state={state} phone={phone} />}
    {phone && !state && <ChevronRight size={14} aria-hidden className="ml-auto text-lw-faint" />}
  </Link>
}

export function SettingsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const { baseUrl, servers, addServer } = useServer()
  const version = useServerVersion(baseUrl)
  const settings = useSettings()
  const configSettings = useSettingsCatalog(baseUrl)
  const configLists = useSettingsLists(baseUrl)
  const devices = useDevices({ baseUrl })
  const registry = useMemo(() => configSettings.data?.settings ?? [], [configSettings.data])
  const fileOnly = configSettings.data?.file_only ?? []
  const fileOnlyValues = configSettings.data?.file_only_values ?? {}
  const [showKeys, setShowKeys] = useState(storedShowKeys)
  // What the pickers choose from: the agent tools and the skills, beside
  // the lists (tiers, models, columns, workspaces).
  const adapters = useQuery({ queryKey: queryKeys.adapters(baseUrl), queryFn: () => fetchAdapters(baseUrl), retry: false })
  const skills = useQuery({ queryKey: [baseUrl, "settings-skills"], queryFn: () => listSkills(baseUrl), retry: false })
  const lists = configLists.data ?? []
  const registered = (key: string) => registry.find((s) => s.key === key)
  const [showPairDevice, setShowPairDevice] = useState(false)
  const [showScanner, setShowScanner] = useState(() => searchParams.get("pair") === "qr")
  const [scanSuccess, setScanSuccess] = useState("")

  const handleQRScan = (data: { url: string; token: string }) => {
    let hostname: string
    try {
      hostname = new URL(data.url).hostname
    } catch {
      setScanSuccess("")
      window.alert("Invalid server URL in QR code: " + data.url)
      return
    }
    const isOrigin = new URL(data.url).host === window.location.host
    setToken(data.token, data.url)
    if (isOrigin) setToken(data.token)
    // A remote server joins this browser's servers.
    if (!isOrigin) addServer(hostname, data.url)
    setShowScanner(false)
    setSearchParams({}, { replace: true })
    setScanSuccess(`Paired with ${data.url}`)
    setTimeout(() => setScanSuccess(""), 3000)
  }

  const refetchSettings = () => { void settings.refetch?.(); void configSettings.refetch() }

  // Every card the page can draw, by its catalog ID.
  const cards: Record<string, ReactNode> = {
    "app-appearance": <AppAppearanceCard />,
    "tint": <div id="tint"><TintCard tint={settings.data?.tint} setting={registered("web.tint")} onChanged={refetchSettings} /></div>,
    "server-theme": <SettingsCard id="server-theme" icon={Palette} title="Theme for devices that follow this Legato" description="Devices that choose Follow server use this theme."><ServerThemeSettings /></SettingsCard>,
    "needs-you-rail": <RailSettings baseUrl={baseUrl} settings={registry} />,
    "browser-notifications": <div id="browser-notifications"><ApprovalNotificationSettings /></div>,
    "keyboard": <KeyboardCard />,
    "repositories": <div id="list-repositories"><div id="list-workflows.repositories"><RepositoriesSettings baseUrl={baseUrl} onChanged={() => void settings.refetch?.()} /></div></div>,
    "directory-presets": <DirectorySettings baseUrl={baseUrl} />,
    "repository-guidance": <div id="list-agents.guidance"><RepositoryGuidanceSettings baseUrl={baseUrl} /></div>,
    "job-clones": <ManagedClonesCard />,
    "skills": <SkillsSettings baseUrl={baseUrl} />,
    "resources": <div id="list-resources"><ResourcesSettings baseUrl={baseUrl} /></div>,
    "engram-bindings": <EngramBindingsSettings baseUrl={baseUrl} />,
    "reeve-auto": <ReeveAutoSettings baseUrl={baseUrl} />,
    "reeve-engram": <ReeveEngramSettings baseUrl={baseUrl} />,
    "reeve-pairings": <ReevePairings baseUrl={baseUrl} />,
    "reeve-sessions": <div id="reeve-sessions"><ReeveSessions baseUrl={baseUrl} /></div>,
    "list-board.columns": <BoardColumnsCard />,
    "burnlite": <IntegrationsSettings baseUrl={baseUrl} only="burnlite" />,
    "argus": <IntegrationsSettings baseUrl={baseUrl} only="argus" />,
    "mcp-connections": <MCPSettings baseUrl={baseUrl} />,
    "mqtt": <MQTTSettings settings={registry} baseUrl={baseUrl} loading={false} error={null} onRetry={() => void configSettings.refetch()} />,
    "devices": <DevicesSettings baseUrl={baseUrl} />,
    "credentials": <CredentialsSettings baseUrl={baseUrl} />,
    "integration-tokens": <IntegrationsSettings baseUrl={baseUrl} only="tokens" />,
    "servers": <ServersCard />,
    "pair-browser": <PairBrowserCard onScan={() => setShowScanner(true)} scanned={scanSuccess} />,
    "pair-phone": <PairPhoneCard onPair={() => setShowPairDevice(true)} />,
    "tls": <TLSCard baseUrl={baseUrl} available={settings.data?.ca_cert_available} />,
    "workers": <WorkersSettings baseUrl={baseUrl} />,
    "main-instances": <MainInstancesSettings baseUrl={baseUrl} />,
    "restart": <RestartCard changes={waitingChanges(registry, lists)} baseUrl={baseUrl} />,
    "web-elsewhere": <SettingsCard id="web-elsewhere" icon={Globe} title="The web server" description="Its port, TLS and public address are in config.yaml only, and shown in Connections."><Link to="#connections/network" className={quietButton}>Open Connections › Network</Link></SettingsCard>,
    "updates": <div id="updates"><div id="setting-updates.mode"><UpdatesSettings status={settings.data ? settings.data.updates ?? null : undefined} baseUrl={baseUrl} modeSetting={registered("updates.mode")} onChanged={refetchSettings} /></div></div>,
    "move": <MoveSettings baseUrl={baseUrl} />,
    "export": <div id="export"><ExportSettings baseUrl={baseUrl} /></div>,
  }
  if (settings.data?.hot_reload_enabled) cards.development = <DevelopmentCard baseUrl={baseUrl} />

  const scroller = useRef<HTMLDivElement>(null)
  const header = useRef<HTMLElement>(null)
  const { hash, key: destinationRevision } = useLocation()
  let target = hash.slice(1)
  try { target = decodeURIComponent(target) } catch { /* An invalid fragment stays visible in the unavailable-target notice. */ }
  const wide = useMediaQuery("(min-width: 1024px)")
  const destination = settingsDestination(target, registry, fileOnly)
  const currentId = destination?.section ?? (!target && wide ? "appearance" : "")
  const current = findSection(currentId)
  const [search, setSearch] = useState("")
  const [active, setActive] = useState(0)
  const searchBox = useRef<HTMLInputElement>(null)
  const navigate = useNavigate()
  const searching = search.trim() !== ""
  const found = useMemo(() => searchSettings(searchEntries(registry, fileOnly, fileOnlyValues, lists, Boolean(settings.data?.hot_reload_enabled)), search),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [registry, configSettings.data, lists, settings.data?.hot_reload_enabled, search])
  const flat = found.groups.flatMap((g) => g.results)
  const clearSearch = () => { setSearch(""); setActive(0) }
  // `/` focuses search from anywhere on the page but a field.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable)) return
      e.preventDefault()
      searchBox.current?.focus()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
  const onSearchKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" && flat.length) { e.preventDefault(); setActive((a) => Math.min(a + 1, flat.length - 1)) }
    else if (e.key === "ArrowUp" && flat.length) { e.preventDefault(); setActive((a) => Math.max(a - 1, 0)) }
    else if (e.key === "Enter" && flat[active]) { e.preventDefault(); const id = flat[active].id; clearSearch(); navigate(`#${id}`) }
    else if (e.key === "Escape") { e.preventDefault(); clearSearch() }
  }
  const [targetFound, setTargetFound] = useState(true)
  const changes = waitingChanges(registry, lists)
  const states = sectionStates(registry, (devices.data?.devices?.length ?? 1) === 0, changes.length)
  const loaded = !configSettings.isPending && !configLists.isPending

  // Open at what the fragment names: a row, a card, a group, or the section.
  const focusId = destination?.element ?? (destination?.group ? groupElementId(destination.section, destination.group) : destination ? `section-${destination.section}` : target)
  const wholeSection = focusId === `section-${destination?.section}`
  useEffect(() => {
    if (!target) return
    let focused = false
    const resolve = () => {
      const element = document.getElementById(focusId)
      setTargetFound(Boolean(element))
      if (element && !focused) {
        element.tabIndex = -1
        element.style.scrollMarginTop = `${(header.current?.getBoundingClientRect().height ?? 0) + 16}px`
        element.focus({ preventScroll: true })
        // A whole section opens at the top, with Back and its group links.
        if (wholeSection) scroller.current?.scrollTo?.({ top: 0 })
        else element.scrollIntoView?.({ block: "start" })
        focused = true
      }
    }
    resolve()
    const observer = new MutationObserver(resolve)
    if (scroller.current) observer.observe(scroller.current, { childList: true, subtree: true })
    return () => observer.disconnect()
  }, [target, focusId, wholeSection, baseUrl, destinationRevision])

  const serverName = servers.find((s) => s.url === baseUrl)?.name ?? (baseUrl ? baseUrl : "Local")
  const groups = current && !current.href ? sectionGroups(current, registry).filter((g) => g !== leadGroup
    && groupHasContent(current.id, g, { settings: registry, fileOnly, lists, cardIds: Object.keys(cards) })) : []
  const groupLinks = (className: string, linkClass: (g: string) => string) => groups.length > 0 && <nav aria-label={`${current!.title} groups`} className={className}>
    {groups.map((g) => <Link key={g} to={`#${current!.id}/${slug(g)}`} className={linkClass(g)} aria-current={destination?.group === g ? "location" : undefined}>{g}</Link>)}
  </nav>

  return (
    <div ref={scroller} className="flex-1 overflow-y-auto bg-lw-ground font-lw text-lw-text">
      <header ref={header} className="sticky top-0 z-10 border-b border-lw-line bg-lw-ground px-4 py-3 md:px-8">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-4 gap-y-2">
          <h1 className="text-lg font-semibold">Settings</h1>
          <Link to="#connections/this-browser" className="inline-flex min-h-8 max-w-[16rem] items-center gap-2 rounded-lg border border-lw-line bg-lw-surface px-2.5 text-sm text-lw-text-2 hover:text-lw-text" title={`Server: ${baseUrl || window.location.origin}`} aria-label={`Server: ${baseUrl || window.location.origin}`}>
            <span className="h-2 w-2 shrink-0 rounded-full bg-lw-live" aria-hidden />
            <span className="truncate">{serverName}</span>
          </Link>
          <span className="flex w-full items-center gap-2 sm:ml-auto sm:w-96">
            <input ref={searchBox} type="search" aria-label="Search settings" placeholder="Search settings, keys and values" value={search}
              onChange={(event) => { setSearch(event.target.value); setActive(0) }} onKeyDown={onSearchKey}
              className="min-h-9 w-full rounded-lg border border-lw-line-strong bg-lw-raised px-3 text-sm" />
            {!wide && searching && <button type="button" onClick={clearSearch} className="min-h-9 px-1 text-sm text-lw-link">Cancel</button>}
            {wide && <kbd className="rounded border border-lw-line px-1.5 font-lw-mono text-[11px] text-lw-text-2" aria-hidden>/</kbd>}
          </span>
        </div>
        {changes.length > 0 && <div className="-mx-4 -mb-3 mt-3 md:-mx-8"><RestartBar changes={changes} baseUrl={baseUrl} /></div>}
      </header>
      <div className="mx-auto grid w-full max-w-6xl gap-8 px-4 py-6 md:px-8 lg:grid-cols-[248px_minmax(0,1fr)]">
        <aside className={current || (searching && !wide) ? "hidden lg:block" : ""}>
          <nav aria-label="Settings sections" className={wide ? "flex flex-col gap-px" : "flex flex-col gap-4"}>
            {navGroups.map((nav) => <div key={nav} role="group" aria-label={nav}>
              <p className="px-3 pb-1.5 pt-3 font-lw-mono text-[10.5px] uppercase tracking-wider text-lw-text-2">{nav}</p>
              <div className={wide ? "flex flex-col gap-px" : "divide-y divide-lw-line overflow-hidden rounded-xl border border-lw-line bg-lw-surface"}>
                {settingsSections.filter((s) => s.nav === nav).map((section) => <div key={section.id}>
                  <SectionLink section={section} current={currentId === section.id && !searching} state={states[section.id]} phone={!wide} onClick={clearSearch} count={searching ? found.counts[section.id] ?? 0 : undefined} />
                  {wide && currentId === section.id && groupLinks("ml-3 mt-0.5 mb-1.5 flex flex-col border-l border-lw-line", (g) => `px-3 py-1 text-[13px] ${destination?.group === g ? "-ml-px border-l-2 border-lw-accent text-lw-text" : "text-lw-text-2 hover:text-lw-text"}`)}
                </div>)}
              </div>
            </div>)}
          </nav>
          <div className="mt-4 flex flex-col gap-3 border-t border-lw-line px-3 pt-3 text-[12.5px] text-lw-text-2">
            <div className="flex items-center justify-between gap-3">
              <span>Show config keys</span>
              <Toggle label="Show config keys" on={showKeys} onChange={() => { const next = !showKeys; setShowKeys(next); storeShowKeys(next) }} />
            </div>
            {version && <p data-testid="settings-version" className="font-lw-mono text-xs text-lw-faint">Legato {version}</p>}
          </div>
        </aside>
        <main key={baseUrl} className="flex min-w-0 max-w-3xl flex-col gap-6">
          <SettingsLoadState loading={configSettings.isPending} error={configSettings.error} stale={Boolean(configSettings.data)} onRetry={() => void configSettings.refetch()} />
          {configLists.isPending && <p role="status" className="text-sm text-lw-text-2">Loading configuration lists…</p>}
          {configLists.error && <div><p role="alert" className="text-sm text-lw-error">Could not load configuration lists: {configLists.error.message}{configLists.data ? " Showing last loaded entries." : ""}</p><button className={quietButton + " mt-2"} onClick={() => void configLists.refetch()}>Retry lists</button></div>}
          {target && loaded && (!destination || !targetFound) && <p role="status" className="text-sm text-lw-text-2">This settings destination is unavailable on this server: {target}. Choose a section or search for another setting.</p>}
          {searching && <SettingsSearch query={search} groups={found.groups} total={found.total} active={active} onSelect={clearSearch} />}
          {current && !wide && !searching && <div className="flex flex-col gap-3">
            <Link to="/settings" className={quietButton + " w-fit"}><ChevronLeft size={14} aria-hidden />Back to Settings</Link>
            {groupLinks("-mx-4 flex gap-2 overflow-x-auto px-4", () => "shrink-0 rounded-full border border-lw-line px-3 py-1 text-xs text-lw-text-2")}
          </div>}
          {current?.href && !search.trim() && <section id={`section-${current.id}`} aria-label={current.title} className="flex flex-col gap-3">
            <h2 className="text-xl font-semibold">{current.title}</h2>
            <p className="text-sm text-lw-text-2">{current.title} has a page of its own.</p>
            <Link to={current.href} className={quietButton + " w-fit"}>Open {current.title}</Link>
          </section>}
          {current && !current.href && !search.trim() && <ShowKeysContext.Provider value={showKeys}><PickerContext.Provider value={pickerData(lists, registry, { adapters: adapters.data?.adapters ?? [], default: adapters.data?.default ?? "" }, (skills.data?.skills ?? []).filter((sk) => !sk.error).map((sk) => sk.name))}>
            <SettingsSectionView section={current} content={{ settings: registry, fileOnly, fileOnlyValues, lists, baseUrl, cards }} />
          </PickerContext.Provider></ShowKeysContext.Provider>}
        </main>
      </div>

      {showPairDevice && <PairDeviceDialog baseUrl={baseUrl} onClose={() => setShowPairDevice(false)} />}
      {showScanner && <QRScanner onScan={handleQRScan} onClose={() => { setShowScanner(false); setSearchParams({}, { replace: true }) }} />}
    </div>
  )
}
